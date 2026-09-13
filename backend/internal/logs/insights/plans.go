package insights

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/logs/pattern"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
)

// Reasons a plan record is not parsed, next to the parser's own codes.
const (
	CodePlanTooLarge        = "plan_too_large"
	CodePlanBudgetExhausted = "plan_budget_exhausted"
)

// PlanLimits bound the parsing of one window; a zero limit is no limit.
type PlanLimits struct {
	MaxPlanBytes int
	MaxPlans     int
}

// DurationStats are exact over the durations of a group, in milliseconds.
type DurationStats struct {
	Min, P50, P95, Max, Sum float64
}

// PlanGroup is the plans of one statement with one shape. Sample is the
// slowest of them; the findings and dormant rules are evaluated on it.
type PlanGroup struct {
	QueryID     int64
	HasQueryID  bool
	Hash        string
	Count       int
	Durations   DurationStats
	First, Last time.Time
	Sample      explain.Plan
	Findings    []explain.Finding
	Dormant     []explain.Dormant
}

type NotParsed struct {
	Code  string
	Count int
}

// DormantRule is a rule that could not run on Groups of the groups.
type DormantRule struct {
	Code    string
	Missing []string
	Groups  int
}

// PlansSummary is what the plans of one window add up to. Records counts the
// auto_explain records seen, parsed or not.
type PlansSummary struct {
	Records         int
	Parsed          int
	NotParsed       []NotParsed
	WithoutQueryID  int
	TotalGroups     int
	TotalDurationMs float64
	MaxDurationMs   float64
	First, Last     time.Time
	Groups          []PlanGroup
	Dormant         []DormantRule
	BudgetExhausted bool
}

// A nested statement shares the query id of its caller, so the normalized query
// text is part of the key.
type planKey struct {
	queryID    int64
	hasQueryID bool
	hash       string
	text       string
}

type planAcc struct {
	group     PlanGroup
	durations []float64
}

// Plans accumulates the auto_explain records of one window.
type Plans struct {
	limits    PlanLimits
	records   int
	attempted int
	parsed    int
	noQueryID int
	notParsed map[string]int
	groups    map[planKey]*planAcc
	exhausted bool
}

func NewPlans(limits PlanLimits) *Plans {
	return &Plans{
		limits:    limits,
		notParsed: map[string]int{},
		groups:    map[planKey]*planAcc{},
	}
}

func (a *Plans) Add(rec PlanRecord, ts time.Time) {
	a.records++

	if a.limits.MaxPlanBytes > 0 && len(rec.Body) > a.limits.MaxPlanBytes {
		a.notParsed[CodePlanTooLarge]++

		return
	}

	if a.limits.MaxPlans > 0 && a.attempted >= a.limits.MaxPlans {
		a.notParsed[CodePlanBudgetExhausted]++
		a.exhausted = true

		return
	}

	a.attempted++

	p, err := explain.Parse(rec.Body, explain.SourceLog)
	if err != nil {
		a.notParsed[parseCode(err)]++

		return
	}

	a.parsed++

	if !rec.HasQueryID {
		a.noQueryID++
	}

	p.QueryID, p.HasQueryID = rec.QueryID, rec.HasQueryID
	p.Duration = &rec.DurationMs
	p.QueryText = sanitize.SQL(p.QueryText)

	key := planKey{
		queryID:    rec.QueryID,
		hasQueryID: rec.HasQueryID,
		hash:       explain.Hash(p.Root),
		text:       pattern.Key(p.QueryText),
	}

	acc, ok := a.groups[key]
	if !ok {
		acc = &planAcc{group: PlanGroup{
			QueryID:    rec.QueryID,
			HasQueryID: rec.HasQueryID,
			Hash:       key.hash,
			First:      ts,
			Last:       ts,
			Sample:     p,
		}}
		a.groups[key] = acc
	}

	acc.group.Count++
	acc.group.First, acc.group.Last = widen(acc.group.First, acc.group.Last, ts)
	acc.durations = append(acc.durations, rec.DurationMs)

	if rec.DurationMs > *acc.group.Sample.Duration {
		acc.group.Sample = p
	}
}

// Exhausted reports whether a plan record has already been turned away for
// lack of budget.
func (a *Plans) Exhausted() bool {
	return a.exhausted
}

// Summary evaluates the rules once per group and keeps the groups among the
// top by total time or among the top by slowest run, ordered by total time.
func (a *Plans) Summary(top int) PlansSummary {
	s := PlansSummary{
		Records:         a.records,
		Parsed:          a.parsed,
		WithoutQueryID:  a.noQueryID,
		TotalGroups:     len(a.groups),
		BudgetExhausted: a.exhausted,
	}

	for code, n := range a.notParsed {
		s.NotParsed = append(s.NotParsed, NotParsed{Code: code, Count: n})
	}

	slices.SortFunc(s.NotParsed, func(x, y NotParsed) int {
		return cmp.Or(cmp.Compare(y.Count, x.Count), cmp.Compare(x.Code, y.Code))
	})

	groups := make([]PlanGroup, 0, len(a.groups))
	dormant := map[string]*DormantRule{}

	for _, acc := range a.groups {
		g := acc.group
		g.Durations = durationStats(acc.durations)

		maskPlan(&g.Sample)
		g.Findings, g.Dormant = explain.Evaluate(&g.Sample, explain.Context{})

		for _, d := range g.Dormant {
			rule, ok := dormant[d.Code]
			if !ok {
				rule = &DormantRule{Code: d.Code}
				dormant[d.Code] = rule
			}

			rule.Groups++

			for _, m := range d.Missing {
				if !slices.Contains(rule.Missing, m) {
					rule.Missing = append(rule.Missing, m)
				}
			}
		}

		s.TotalDurationMs += g.Durations.Sum
		s.MaxDurationMs = max(s.MaxDurationMs, g.Durations.Max)

		if len(groups) == 0 {
			s.First, s.Last = g.First, g.Last
		} else {
			s.First, s.Last = widen(s.First, s.Last, g.First)
			s.First, s.Last = widen(s.First, s.Last, g.Last)
		}

		groups = append(groups, g)
	}

	for _, rule := range dormant {
		s.Dormant = append(s.Dormant, *rule)
	}

	slices.SortFunc(s.Dormant, func(x, y DormantRule) int { return cmp.Compare(x.Code, y.Code) })

	s.Groups = topGroups(groups, top)

	return s
}

func topGroups(groups []PlanGroup, top int) []PlanGroup {
	bySum := func(x, y PlanGroup) int {
		return cmp.Or(
			cmp.Compare(y.Durations.Sum, x.Durations.Sum),
			cmp.Compare(y.Durations.Max, x.Durations.Max),
			cmp.Compare(y.Count, x.Count),
			cmp.Compare(x.Hash, y.Hash),
			cmp.Compare(x.QueryID, y.QueryID),
			cmp.Compare(x.Sample.QueryText, y.Sample.QueryText),
		)
	}

	slices.SortFunc(groups, bySum)

	if len(groups) <= top {
		return groups
	}

	byMax := make([]int, len(groups))
	for i := range byMax {
		byMax[i] = i
	}

	slices.SortStableFunc(byMax, func(x, y int) int {
		return cmp.Compare(groups[y].Durations.Max, groups[x].Durations.Max)
	})

	keep := make([]bool, len(groups))
	for i := range top {
		keep[i] = true
		keep[byMax[i]] = true
	}

	out := make([]PlanGroup, 0, 2*top)

	for i, g := range groups {
		if keep[i] {
			out = append(out, g)
		}
	}

	return out
}

// durationStats uses nearest-rank percentiles over the sorted durations.
func durationStats(ds []float64) DurationStats {
	if len(ds) == 0 {
		return DurationStats{}
	}

	sorted := slices.Clone(ds)
	slices.Sort(sorted)

	rank := func(q float64) float64 {
		i := int(math.Ceil(q*float64(len(sorted)))) - 1

		return sorted[max(i, 0)]
	}

	var sum float64
	for _, d := range sorted {
		sum += d
	}

	return DurationStats{
		Min: sorted[0],
		P50: rank(0.5),
		P95: rank(0.95),
		Max: sorted[len(sorted)-1],
		Sum: sum,
	}
}

func parseCode(err error) string {
	var pe *explain.Error
	if errors.As(err, &pe) {
		return pe.Code
	}

	return explain.CodeParseError
}

// maskPlan applies the masking the log search applies to the message field;
// the body is parsed first because masking can break a JSON document.
func maskPlan(p *explain.Plan) {
	p.QueryText = sanitize.SQL(p.QueryText)
	p.QueryParams = sanitize.SQL(p.QueryParams)

	p.Walk(func(_ []int, n *explain.Node) bool {
		n.Filter = sanitize.SQL(n.Filter)
		n.IndexCond = sanitize.SQL(n.IndexCond)
		n.RecheckCond = sanitize.SQL(n.RecheckCond)
		n.JoinCond = sanitize.SQL(n.JoinCond)

		return true
	})
}
