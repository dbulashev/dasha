package insights

import (
	"slices"
	"testing"

	"github.com/dbulashev/dasha/internal/health"
)

// row is one group of a window: a statement running with one plan shape.
func row(queryID int64, hash string, p50, p95 float64, indexes ...string) GroupRow {
	return GroupRow{ //nolint:exhaustruct
		QueryID:    queryID,
		HasQueryID: queryID != 0,
		Hash:       hash,
		Count:      minSlowerPlans,
		Durations:  DurationStats{Min: p50, P50: p50, P95: p95, Max: p95, Sum: p50 * minSlowerPlans},
		QueryText:  "SELECT count(*) FROM orders WHERE status = $1",
		Indexes:    indexes,
	}
}

func TestCompareListsAStatementThatLostItsIndex(t *testing.T) {
	t.Parallel()

	base := []GroupRow{row(42, "indexed", 10, 12, "orders_status_idx")}
	cur := []GroupRow{row(42, "seqscan", 200, 300)}

	got := Compare(cur, base, true)
	if len(got) != 1 {
		t.Fatalf("regressions = %d, want the one statement", len(got))
	}

	r := got[0]
	if !slices.Equal(r.Reasons, []string{ReasonNewShape, ReasonLostIndex, ReasonSlower}) {
		t.Errorf("reasons = %v, want the shape, the index and the time", r.Reasons)
	}

	if !slices.Equal(r.AddedHashes, []string{"seqscan"}) || !slices.Equal(r.RemovedHashes, []string{"indexed"}) {
		t.Errorf("shapes = +%v -%v, want +seqscan -indexed", r.AddedHashes, r.RemovedHashes)
	}

	if !slices.Equal(r.LostIndexes, []string{"orders_status_idx"}) || len(r.AddedIndexes) != 0 {
		t.Errorf("indexes = +%v -%v, want the lost one alone", r.AddedIndexes, r.LostIndexes)
	}

	if r.Severity != health.SeverityHigh {
		t.Errorf("severity = %s, want %s", r.Severity, health.SeverityHigh)
	}

	if r.P50Ratio != 20 || r.P95Ratio != 25 {
		t.Errorf("ratios = %v / %v, want 20 and 25", r.P50Ratio, r.P95Ratio)
	}
}

func withCount(r GroupRow, count int) GroupRow {
	r.Count = count

	return r
}

func TestCompareIgnoresASlowdownMeasuredOnTooFewPlans(t *testing.T) {
	t.Parallel()

	base := []GroupRow{withCount(row(42, "hot", 10, 20, "orders_status_idx"), 2)}
	cur := []GroupRow{withCount(row(42, "hot", 30, 70, "orders_status_idx"), 2)}

	if got := Compare(cur, base, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: the p95 of two plans is their slowest run", got)
	}

	shifted := []GroupRow{withCount(row(42, "cold", 100, 2000, "orders_status_idx"), 2)}

	got := Compare(shifted, base, true)
	if len(got) != 1 {
		t.Fatalf("regressions = %d, want the new shape", len(got))
	}

	if !slices.Equal(got[0].Reasons, []string{ReasonNewShape}) || got[0].Severity != health.SeverityLow {
		t.Errorf("regression = %v at %s, want the shape alone at %s",
			got[0].Reasons, got[0].Severity, health.SeverityLow)
	}
}

func TestCompareReportsALostIndexHoweverFewThePlans(t *testing.T) {
	t.Parallel()

	base := []GroupRow{withCount(row(42, "indexed", 10, 12, "orders_status_idx"), 1)}
	cur := []GroupRow{withCount(row(42, "seqscan", 200, 300), 1)}

	got := Compare(cur, base, true)
	if len(got) != 1 {
		t.Fatalf("regressions = %d, want the one statement", len(got))
	}

	if !slices.Equal(got[0].Reasons, []string{ReasonNewShape, ReasonLostIndex}) {
		t.Errorf("reasons = %v, want the shape and the index without the time", got[0].Reasons)
	}

	if got[0].Severity != health.SeverityHigh {
		t.Errorf("severity = %s, want %s", got[0].Severity, health.SeverityHigh)
	}
}

func TestCompareCarriesThePlanCountsOfBothSides(t *testing.T) {
	t.Parallel()

	base := []GroupRow{withCount(row(42, "indexed", 10, 12, "orders_status_idx"), 40)}
	cur := []GroupRow{withCount(row(42, "seqscan", 200, 300), 25)}

	got := Compare(cur, base, true)
	if len(got) != 1 {
		t.Fatalf("regressions = %d, want one", len(got))
	}

	if got[0].CurrentCount != 25 || got[0].BaselineCount != 40 {
		t.Errorf("counts = %d / %d, want 25 and 40", got[0].CurrentCount, got[0].BaselineCount)
	}
}

func TestCompareIgnoresAStatementThatStayedTheSame(t *testing.T) {
	t.Parallel()

	base := []GroupRow{row(42, "indexed", 10, 12, "orders_status_idx")}
	cur := []GroupRow{row(42, "indexed", 11, 13, "orders_status_idx")}

	if got := Compare(cur, base, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: one shape, the same time", got)
	}
}

func TestCompareIgnoresAStatementOnlyOneWindowHolds(t *testing.T) {
	t.Parallel()

	base := []GroupRow{row(42, "indexed", 10, 12)}
	cur := []GroupRow{row(42, "indexed", 10, 12), row(77, "fresh", 500, 900)}

	if got := Compare(cur, base, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: a first appearance is no regression", got)
	}

	if got := Compare(base, cur, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: a statement that stopped running is no regression", got)
	}
}

// The percentiles come from the shape the statement spent its time in, not from
// the one that merely ran slowest once.
func TestCompareReadsTheDominantShape(t *testing.T) {
	t.Parallel()

	slow := row(42, "rare", 1000, 2000)
	slow.Durations.Sum = 200

	base := []GroupRow{row(42, "hot", 10, 20)}
	cur := []GroupRow{row(42, "hot", 30, 60), slow}

	got := Compare(cur, base, true)
	if len(got) != 1 {
		t.Fatalf("regressions = %d, want one", len(got))
	}

	if got[0].P95Ratio != 3 {
		t.Errorf("p95 ratio = %v, want 3 from the shape holding most of the time", got[0].P95Ratio)
	}
}

func TestCompareTiesPlansWithoutAQueryIDByText(t *testing.T) {
	t.Parallel()

	base := []GroupRow{row(0, "indexed", 10, 12, "orders_status_idx")}
	cur := []GroupRow{row(0, "seqscan", 10, 12)}

	got := Compare(cur, base, true)
	if len(got) != 1 || got[0].HasQueryID {
		t.Fatalf("regressions = %+v, want the statement matched by its text", got)
	}

	other := row(0, "seqscan", 10, 12)
	other.QueryText = "SELECT count(*) FROM payments WHERE status = $1"

	if got := Compare([]GroupRow{other}, base, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: another statement", got)
	}
}

func TestCompareRanksTheWorstFirst(t *testing.T) {
	t.Parallel()

	base := []GroupRow{
		row(1, "a", 10, 10, "idx_one"),
		row(2, "b", 10, 10),
		row(3, "c", 10, 10),
	}

	lostIndex := row(1, "a", 10, 10)
	twentyfold := row(2, "b", 10, 200)
	newShapeSameTime := row(3, "c2", 10, 10)

	cur := []GroupRow{lostIndex, twentyfold, newShapeSameTime}

	got := Compare(cur, base, true)
	if len(got) != 3 {
		t.Fatalf("regressions = %d, want three", len(got))
	}

	wantSeverity := []health.Severity{health.SeverityHigh, health.SeverityHigh, health.SeverityLow}
	for i, r := range got {
		if r.Severity != wantSeverity[i] {
			t.Errorf("regression %d severity = %s, want %s", i, r.Severity, wantSeverity[i])
		}
	}

	if got[0].QueryID != 2 || got[1].QueryID != 1 || got[2].QueryID != 3 {
		t.Errorf("order = %d, %d, %d; want 2, 1, 3", got[0].QueryID, got[1].QueryID, got[2].QueryID)
	}
}

func TestCompareIgnoresAGrowthFromAZeroBaseline(t *testing.T) {
	t.Parallel()

	base := []GroupRow{row(42, "hot", 0, 0, "orders_status_idx")}
	cur := []GroupRow{row(42, "hot", 50, 90, "orders_status_idx")}

	if got := Compare(cur, base, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: a growth from zero is not a multiple", got)
	}
}

func TestCompareLeavesIndexesOutWhenTheWindowRecordedNone(t *testing.T) {
	t.Parallel()

	base := []GroupRow{row(42, "indexed", 10, 12, "orders_status_idx")}
	cur := []GroupRow{row(42, "indexed", 11, 13)}

	if got := Compare(cur, base, false); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: a window that recorded no index lost none", got)
	}
}

func TestCompareSkipsGroupsWithNeitherAQueryIDNorText(t *testing.T) {
	t.Parallel()

	nameless := func(hash string, p50, p95 float64) GroupRow {
		r := row(0, hash, p50, p95)
		r.QueryText = ""

		return r
	}

	base := []GroupRow{nameless("a", 10, 10), nameless("b", 10, 10)}
	cur := []GroupRow{nameless("c", 200, 300), nameless("d", 200, 300)}

	if got := Compare(cur, base, true); len(got) != 0 {
		t.Errorf("regressions = %+v, want none: unrelated groups must not merge into one statement", got)
	}
}
