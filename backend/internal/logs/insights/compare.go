package insights

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/logs/pattern"
)

// Why a statement is listed as a regression.
const (
	ReasonNewShape  = "new_shape"
	ReasonLostIndex = "lost_index"
	ReasonSlower    = "slower"
)

// Growth of p95 that makes a statement a regression, and the growth that makes
// it a severe one.
const (
	slowdownFactor = 2.0
	severeFactor   = 10.0
)

// Plans a shape needs in both windows before their p95 is compared: percentiles
// go by nearest rank, so below this the p95 is the slowest run and an ordinary
// spread between two executions reads as a slowdown.
const minSlowerPlans = 20

// Regression is one statement whose plans differ between a baseline window and
// the current one. Current and Baseline are the durations of the shape that
// took the most time in each window: percentiles of two shapes cannot be merged
// after the fact, and that shape is where the statement spent its time.
type Regression struct {
	QueryID       int64
	HasQueryID    bool
	QueryText     string
	Reasons       []string
	AddedHashes   []string
	RemovedHashes []string
	AddedIndexes  []string
	LostIndexes   []string
	Current       DurationStats
	Baseline      DurationStats
	// CurrentCount and BaselineCount are the plans behind the dominant shape of
	// each window, which is what the ratios are measured over.
	CurrentCount  int
	BaselineCount int
	P50Ratio      float64
	P95Ratio      float64
	Severity      health.Severity
}

// Compare lists the statements whose plans changed for the worse between the
// two windows, worst first. A statement only one window holds is left out: a
// first appearance is not a regression, and neither is a disappearance.
// indexesKnown false leaves indexes out of the comparison: the groups of a
// window that recorded none cannot tell a dropped index from an unrecorded one.
func Compare(current, baseline []GroupRow, indexesKnown bool) []Regression {
	cur, base := statements(current), statements(baseline)

	out := make([]Regression, 0, len(cur))

	for key, c := range cur {
		b, ok := base[key]
		if !ok {
			continue
		}

		if r, ok := compareStatement(c, b, indexesKnown); ok {
			out = append(out, r)
		}
	}

	slices.SortFunc(out, func(x, y Regression) int {
		return cmp.Or(
			cmp.Compare(severityRank(x.Severity), severityRank(y.Severity)),
			cmp.Compare(y.P95Ratio, x.P95Ratio),
			cmp.Compare(y.Current.Sum, x.Current.Sum),
			cmp.Compare(x.QueryID, y.QueryID),
			cmp.Compare(x.QueryText, y.QueryText),
		)
	})

	return out
}

// statement is what one statement did in one window: every shape it ran with,
// every index those shapes read, and the group that took the most time.
type statement struct {
	dominant GroupRow
	hashes   []string
	indexes  []string
}

func statements(rows []GroupRow) map[string]*statement {
	out := make(map[string]*statement, len(rows))

	for _, r := range rows {
		key, ok := statementKey(r)
		if !ok {
			continue
		}

		s, ok := out[key]
		if !ok {
			s = &statement{dominant: r, hashes: nil, indexes: nil}
			out[key] = s
		}

		if r.Durations.Sum > s.dominant.Durations.Sum {
			s.dominant = r
		}

		s.hashes = appendNew(s.hashes, r.Hash)
		s.indexes = appendNew(s.indexes, r.Indexes...)
	}

	return out
}

// statementKey ties the groups of one statement together across the two
// windows: the query id when the record carries one, the normalized text when
// it does not. A group carrying neither is left out, else every such group of a
// window would merge into one statement.
func statementKey(r GroupRow) (string, bool) {
	if r.HasQueryID {
		return "q:" + strconv.FormatInt(r.QueryID, 10), true
	}

	if key := pattern.Key(r.QueryText); key != "" {
		return "t:" + key, true
	}

	return "", false
}

func compareStatement(cur, base *statement, indexesKnown bool) (Regression, bool) {
	r := Regression{
		QueryID:       cur.dominant.QueryID,
		HasQueryID:    cur.dominant.HasQueryID,
		QueryText:     cur.dominant.QueryText,
		Reasons:       nil,
		AddedHashes:   missingFrom(cur.hashes, base.hashes),
		RemovedHashes: missingFrom(base.hashes, cur.hashes),
		AddedIndexes:  nil,
		LostIndexes:   nil,
		Current:       cur.dominant.Durations,
		Baseline:      base.dominant.Durations,
		CurrentCount:  cur.dominant.Count,
		BaselineCount: base.dominant.Count,
		P50Ratio:      ratio(cur.dominant.Durations.P50, base.dominant.Durations.P50),
		P95Ratio:      ratio(cur.dominant.Durations.P95, base.dominant.Durations.P95),
		Severity:      "",
	}

	if indexesKnown {
		r.AddedIndexes = missingFrom(cur.indexes, base.indexes)
		r.LostIndexes = missingFrom(base.indexes, cur.indexes)
	}

	if len(r.AddedHashes) > 0 {
		r.Reasons = append(r.Reasons, ReasonNewShape)
	}

	if len(r.LostIndexes) > 0 {
		r.Reasons = append(r.Reasons, ReasonLostIndex)
	}

	measured := r.CurrentCount >= minSlowerPlans && r.BaselineCount >= minSlowerPlans

	if measured && r.P95Ratio >= slowdownFactor {
		r.Reasons = append(r.Reasons, ReasonSlower)
	}

	if len(r.Reasons) == 0 {
		return Regression{}, false
	}

	r.Severity = regressionSeverity(r)

	return r, true
}

// regressionSeverity ranks what the change costs: an index the current window
// stopped using, or a tenfold p95, outweighs a shape that appeared without
// costing time. A ratio that did not earn ReasonSlower carries no weight here
// either.
func regressionSeverity(r Regression) health.Severity {
	slower := slices.Contains(r.Reasons, ReasonSlower)

	switch {
	case len(r.LostIndexes) > 0, slower && r.P95Ratio >= severeFactor:
		return health.SeverityHigh
	case slower:
		return health.SeverityMedium
	default:
		return health.SeverityLow
	}
}

// ratio is zero when the baseline measured nothing: a growth from zero is not a
// multiple.
func ratio(cur, base float64) float64 {
	if base <= 0 {
		return 0
	}

	return cur / base
}

func missingFrom(values, other []string) []string {
	var out []string

	for _, v := range values {
		if !slices.Contains(other, v) {
			out = append(out, v)
		}
	}

	slices.Sort(out)

	return out
}

func appendNew(values []string, add ...string) []string {
	for _, v := range add {
		if v != "" && !slices.Contains(values, v) {
			values = append(values, v)
		}
	}

	return values
}

func severityRank(s health.Severity) int {
	switch s {
	case health.SeverityHigh:
		return 0
	case health.SeverityMedium:
		return 1
	default:
		return 2
	}
}
