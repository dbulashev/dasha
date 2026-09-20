package insights

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/internal/explain"
)

func corpusPlans(t *testing.T, name string, limits PlanLimits) *Plans {
	t.Helper()

	plans := NewPlans(limits)

	for _, r := range loadCorpus(t, name) {
		if pr, ok := Detect(r.Text, r.QueryID); ok {
			plans.Add(pr, r.Timestamp)
		}
	}

	return plans
}

func groupsOf(s PlansSummary, queryID int64) []PlanGroup {
	var out []PlanGroup

	for _, g := range s.Groups {
		if g.HasQueryID && g.QueryID == queryID {
			out = append(out, g)
		}
	}

	return out
}

// Query ids of the probe cases in pg17.jsonl.
const (
	qidCountK42 = -4452854032459450605 // text_verbose_off, json_verbose_off, analyze_off
	qidParam    = -9088110242053087898 // param_1, param_2
	qidNested   = 8193680034694553172  // probe_f() and the statement inside it
)

func TestPlansCorpusSummary(t *testing.T) {
	t.Parallel()

	s := corpusPlans(t, "pg17.jsonl", PlanLimits{}).Summary(explain.Context{})

	if s.Records != 14 {
		t.Errorf("records = %d, want 14", s.Records)
	}

	notParsed := 0
	for _, np := range s.NotParsed {
		notParsed += np.Count
	}

	if s.Parsed+notParsed != s.Records {
		t.Errorf("parsed %d + not parsed %d != records %d", s.Parsed, notParsed, s.Records)
	}

	want := []NotParsed{{Code: explain.CodeUnsupportedFormat, Count: 2}}
	if !slices.Equal(s.NotParsed, want) {
		t.Errorf("not parsed = %+v, want xml and yaml as %+v", s.NotParsed, want)
	}

	if s.WithoutQueryID != 0 {
		t.Errorf("without query id = %d, want 0", s.WithoutQueryID)
	}

	if s.TotalGroups != len(s.Groups) {
		t.Errorf("total groups = %d, listed %d; the top of 100 must list all", s.TotalGroups, len(s.Groups))
	}

	if s.MaxDurationMs != 667.705 {
		t.Errorf("max duration = %v, want the CREATE TABLE AS at 667.705", s.MaxDurationMs)
	}

	for i := 1; i < len(s.Groups); i++ {
		if s.Groups[i-1].Durations.Sum < s.Groups[i].Durations.Sum {
			t.Errorf("groups not ordered by total time at %d", i)
		}
	}
}

func TestPlansFoldLiteralsAndFormats(t *testing.T) {
	t.Parallel()

	s := corpusPlans(t, "pg17.jsonl", PlanLimits{}).Summary(explain.Context{})

	param := groupsOf(s, qidParam)
	if len(param) != 1 || param[0].Count != 2 {
		t.Fatalf("param_1/param_2 = %d groups, want one of 2 plans: %+v", len(param), param)
	}

	g := param[0]
	if g.Durations.Min != 14.752 || g.Durations.Max != 16.749 || math.Abs(g.Durations.Sum-31.501) > 1e-9 {
		t.Errorf("durations = %+v", g.Durations)
	}

	if *g.Sample.Duration != 16.749 || !strings.Contains(g.Sample.QueryText, "id = 2") {
		t.Errorf("sample is %v ms %q, want the slowest run", *g.Sample.Duration, g.Sample.QueryText)
	}

	folded, plans := false, 0

	for _, g := range groupsOf(s, qidCountK42) {
		plans += g.Count
		folded = folded || g.Count >= 2
	}

	if plans != 3 || !folded {
		t.Errorf("text and json of one statement did not fold: %d plans, folded %v", plans, folded)
	}
}

// TestPlansKeepNestedStatementsApart: a statement run inside a function shares
// the query id of the call.
func TestPlansKeepNestedStatementsApart(t *testing.T) {
	t.Parallel()

	s := corpusPlans(t, "pg17.jsonl", PlanLimits{}).Summary(explain.Context{})

	nested := groupsOf(s, qidNested)
	if len(nested) != 2 {
		t.Fatalf("nested = %d groups, want 2", len(nested))
	}

	texts := []string{nested[0].Sample.QueryText, nested[1].Sample.QueryText}
	if !slices.ContainsFunc(texts, func(q string) bool { return strings.HasPrefix(q, "SELECT probe_f()") }) {
		t.Errorf("query texts = %q, want the call among them", texts)
	}
}

func TestPlansWithoutQueryIDStayApartByText(t *testing.T) {
	t.Parallel()

	body := func(filter string) string {
		return "Query Text: SELECT * FROM t WHERE " + filter + "\n" +
			"Seq Scan on t  (cost=0.00..10.00 rows=1 width=4) (actual time=0.010..0.020 rows=1 loops=1)\n" +
			"  Filter: (" + filter + ")\n"
	}

	plans := NewPlans(PlanLimits{})
	plans.Add(PlanRecord{DurationMs: 1, Body: body("a = 1")}, at(0))
	plans.Add(PlanRecord{DurationMs: 2, Body: body("a = 2")}, at(1))
	plans.Add(PlanRecord{DurationMs: 3, Body: body("b = 1")}, at(2))

	s := plans.Summary(explain.Context{})

	if s.WithoutQueryID != 3 {
		t.Errorf("without query id = %d, want 3", s.WithoutQueryID)
	}

	if s.TotalGroups != 2 {
		t.Errorf("groups = %d, want a = ? and b = ? apart", s.TotalGroups)
	}
}

func TestPlansDormantRulesOnAPlanWithoutAnalyze(t *testing.T) {
	t.Parallel()

	r := probeCase(t, loadCorpus(t, "pg17.jsonl"), "analyze_off")[0]
	pr, _ := Detect(r.Text, r.QueryID)

	plans := NewPlans(PlanLimits{})
	plans.Add(pr, r.Timestamp)

	s := plans.Summary(explain.Context{})

	i := slices.IndexFunc(s.Dormant, func(d DormantRule) bool { return d.Code == explain.RuleRowMisestimate })
	if i < 0 {
		t.Fatalf("dormant = %+v, want %s among them", s.Dormant, explain.RuleRowMisestimate)
	}

	if d := s.Dormant[i]; d.Groups != 1 || !slices.Contains(d.Missing, explain.MissingActual) {
		t.Errorf("dormant %+v, want one group missing %s", d, explain.MissingActual)
	}
}

func TestPlansLimits(t *testing.T) {
	t.Parallel()

	s := corpusPlans(t, "pg17.jsonl", PlanLimits{MaxPlans: 3}).Summary(explain.Context{})

	if !s.BudgetExhausted || s.Parsed > 3 {
		t.Errorf("parsed = %d, exhausted = %v; want at most 3 and exhausted", s.Parsed, s.BudgetExhausted)
	}

	if !slices.ContainsFunc(s.NotParsed, func(np NotParsed) bool { return np.Code == CodePlanBudgetExhausted }) {
		t.Errorf("not parsed = %+v, want %s", s.NotParsed, CodePlanBudgetExhausted)
	}

	s = corpusPlans(t, "pg17.jsonl", PlanLimits{MaxPlanBytes: 700}).Summary(explain.Context{})

	large := slices.IndexFunc(s.NotParsed, func(np NotParsed) bool { return np.Code == CodePlanTooLarge })
	if large < 0 || s.BudgetExhausted {
		t.Errorf("not parsed = %+v, exhausted = %v; want %s without exhausting the budget",
			s.NotParsed, s.BudgetExhausted, CodePlanTooLarge)
	}
}

func TestPlansMaskCredentials(t *testing.T) {
	t.Parallel()

	body := "Query Text: SELECT * FROM pg_subscription WHERE subconninfo = 'host=x password=hunter2'\n" +
		"Seq Scan on pg_subscription  (cost=0.00..1.01 rows=1 width=100) (actual time=0.010..0.011 rows=0 loops=1)\n" +
		"  Filter: (subconninfo = 'host=x password=hunter2'::text)\n" +
		"  Rows Removed by Filter: 1\n"

	plans := NewPlans(PlanLimits{})
	plans.Add(PlanRecord{DurationMs: 1, Body: body, QueryID: 7, HasQueryID: true}, at(0))

	g := plans.Summary(explain.Context{}).Groups[0]

	if strings.Contains(g.Sample.QueryText, "hunter2") || strings.Contains(g.Sample.Root.Filter, "hunter2") {
		t.Errorf("password left in the plan: %q / %q", g.Sample.QueryText, g.Sample.Root.Filter)
	}
}

func TestTopGroupsKeepsTheSlowestRunToo(t *testing.T) {
	t.Parallel()

	groups := []PlanGroup{
		{Hash: "wide", Durations: DurationStats{Sum: 100, Max: 10}},
		{Hash: "spike", Durations: DurationStats{Sum: 50, Max: 50}},
		{Hash: "small", Durations: DurationStats{Sum: 20, Max: 5}},
	}

	got := TopGroups(groups, 2)

	hashes := make([]string, 0, len(got))
	for _, g := range got {
		hashes = append(hashes, g.Hash)
	}

	if !slices.Equal(hashes, []string{"wide", "spike"}) {
		t.Errorf("top = %v, want wide (total) and spike (slowest run)", hashes)
	}
}

func TestTopGroupsAnswersWithNoMoreThanTheCap(t *testing.T) {
	t.Parallel()

	groups := make([]PlanGroup, 20)
	for i := range groups {
		groups[i] = PlanGroup{ //nolint:exhaustruct
			Hash:      strconv.Itoa(i),
			Durations: DurationStats{Sum: float64(20 - i), Max: float64(i)}, //nolint:exhaustruct
		}
	}

	if got := TopGroups(groups, 5); len(got) != 5 {
		t.Errorf("top = %d groups, want the 5 asked for", len(got))
	}
}

func TestDurationStats(t *testing.T) {
	t.Parallel()

	got := durationStats([]float64{5, 1, 3, 2, 4})
	want := DurationStats{Min: 1, P50: 3, P95: 5, Max: 5, Sum: 15}

	if got != want {
		t.Errorf("durationStats = %+v, want %+v", got, want)
	}

	if one := durationStats([]float64{7}); one.P50 != 7 || one.P95 != 7 {
		t.Errorf("single run = %+v", one)
	}
}

func TestRowTellsAnEmptyIndexListFromAnAbsentOne(t *testing.T) {
	t.Parallel()

	var g PlanGroup
	g.Sample.Root.Type = "Seq Scan"

	if idx := g.Row().Indexes; idx == nil || len(idx) != 0 {
		t.Errorf("indexes = %#v, want an empty list: the plan read none", idx)
	}
}
