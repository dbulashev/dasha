package mcpserver

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dbulashev/dasha/gen/apiclient"
)

func cmpSide(calls int64, exec, rows, io float64) *apiclient.QueryReportMetrics {
	mean := 0.0
	if calls > 0 {
		mean = exec / float64(calls)
	}

	r := int64(rows)

	return &apiclient.QueryReportMetrics{ //nolint:exhaustruct
		Calls: &calls, ExecTimeMs: &exec, MeanExecTimeMs: &mean, Rows: &r, IoTimeMs: &io,
	}
}

func cmpItem(id string, left, right *apiclient.QueryReportMetrics) apiclient.QueryCompareItem {
	db := "orders"

	return apiclient.QueryCompareItem{QueryID: id, Datname: &db, Query: "select " + id, Left: left, Right: right}
}

func cmpFixture() []apiclient.QueryCompareItem {
	return []apiclient.QueryCompareItem{
		cmpItem("more", cmpSide(1000, 1000, 10, 5), cmpSide(2000, 2000, 20, 10)),
		cmpItem("slower", cmpSide(1000, 1000, 10, 5), cmpSide(1000, 4000, 10, 400)),
		cmpItem("added", nil, cmpSide(10, 300, 5000, 1)),
		cmpItem("removed", cmpSide(10, 50, 1, 3), nil),
		cmpItem("stable", cmpSide(1000, 1000, 10, 5), cmpSide(1050, 1060, 10, 5)),
	}
}

func entryByID(r *compareResult, id string) *compareEntry {
	for i := range r.ranked {
		if r.ranked[i].QueryID == id {
			return &r.ranked[i]
		}
	}

	return nil
}

func TestQueryCompare_Verdicts(t *testing.T) {
	t.Parallel()

	r := buildCompare(cmpFixture(), compareSortExecTime, compareDefaultLimit)

	for id, want := range map[string]string{
		"more": verdictMoreCalls, "slower": verdictSlower, "added": verdictAdded,
		"removed": verdictRemoved, "stable": verdictStable,
	} {
		if e := entryByID(r, id); e == nil || e.Verdict != want {
			t.Errorf("%s: verdict = %+v, want %s", id, e, want)
		}
	}

	s := entryByID(r, "slower")
	if s.Delta.ExecTimeMs != 3000 || s.Delta.MeanExecTimeMs != 3 || s.ChangePct == nil ||
		*s.ChangePct.MeanExecTimeMs != 300 || *s.ChangePct.Calls != 0 {
		t.Errorf("slower delta = %+v change = %+v", s.Delta, s.ChangePct)
	}

	if a := entryByID(r, "added"); a.ChangePct != nil || a.Delta.Calls != 10 {
		t.Errorf("added must carry B as its delta and no change_pct: %+v", a)
	}

	if d := entryByID(r, "removed"); d.Delta.Calls != -10 || d.Delta.ExecTimeMs != -50 {
		t.Errorf("removed must carry minus A as its delta: %+v", d.Delta)
	}

	if r.Summary.Compared != 5 || r.Summary.Added != 1 || r.Summary.Removed != 1 || r.Summary.Returned != 5 {
		t.Errorf("summary = %+v", r.Summary)
	}
}

func TestQueryCompare_SlowerAndMoreCalls(t *testing.T) {
	t.Parallel()

	r := buildCompare([]apiclient.QueryCompareItem{
		cmpItem("both", cmpSide(100, 100, 1, 0), cmpSide(200, 800, 1, 0)),
		cmpItem("less", cmpSide(100, 100, 1, 0), cmpSide(95, 90, 1, 0)),
	}, compareSortExecTime, compareDefaultLimit)

	if v := entryByID(r, "both").Verdict; v != verdictSlowerMore {
		t.Errorf("both = %s", v)
	}

	if v := entryByID(r, "less").Verdict; v != verdictLessLoad {
		t.Errorf("less = %s", v)
	}
}

func TestQueryCompare_Sort(t *testing.T) {
	t.Parallel()

	for sort, first := range map[string]string{
		"exec_time": "slower", "calls": "more", "rows": "added", "io_time": "slower",
	} {
		r := buildCompare(cmpFixture(), sort, compareDefaultLimit)
		if got := r.Queries[0].QueryID; got != first {
			t.Errorf("sort=%s: first = %s, want %s", sort, got, first)
		}

		if last := r.Queries[len(r.Queries)-1].QueryID; last != "removed" {
			t.Errorf("sort=%s: last = %s, want removed", sort, last)
		}
	}
}

func TestQueryCompare_LimitAndNote(t *testing.T) {
	t.Parallel()

	r := buildCompare(cmpFixture(), compareSortExecTime, 2)

	if r.Summary.Returned != 2 || len(r.Queries) != 2 || r.Queries[0].QueryID != "slower" {
		t.Fatalf("limit=2: %+v", r.Queries)
	}

	n := r.note()
	if n.Total != 5 || !strings.HasPrefix(n.Full, "limit=5") || !strings.Contains(n.Folded, "ranked tail") {
		t.Errorf("note = %+v", n)
	}

	if full := buildCompare(cmpFixture(), compareSortExecTime, 20).note(); strings.Contains(full.Folded, "tail") {
		t.Errorf("a complete list must not claim a cut tail: %+v", full)
	}
}

func TestQueryCompare_TruncQuery(t *testing.T) {
	t.Parallel()

	long := "select  *\n\tfrom t where " + strings.Repeat("ж", 300)
	it := apiclient.QueryCompareItem{QueryID: "1", Query: long} //nolint:exhaustruct
	e := compareItem(it)

	if len(e.QueryTrunc) > compareQueryBytes || !strings.HasPrefix(e.QueryTrunc, "select * from t where ") {
		t.Errorf("query_trunc = %q (%d bytes)", e.QueryTrunc, len(e.QueryTrunc))
	}

	if !utf8.ValidString(e.QueryTrunc) || e.QueryLen != len(long) {
		t.Errorf("query_len = %d, want %d", e.QueryLen, len(long))
	}
}

func TestQueryCompare_ShrinkLowersLimit(t *testing.T) {
	t.Parallel()

	items := make([]apiclient.QueryCompareItem, 0, 100)
	for i := range 100 {
		items = append(items, cmpItem(strconv.Itoa(i), cmpSide(10, 10, 1, 0), cmpSide(20, float64(20+i), 1, 0)))
	}

	r := buildCompare(items, compareSortExecTime, 20)

	next, step, ok := r.shrink()
	if !ok || step != "limit=10" {
		t.Fatalf("shrink = %q %v", step, ok)
	}

	s, _ := next.(*compareResult)
	if s.Queries[0].QueryID != "99" || s.Summary.Returned != 10 || len(r.Queries) != 20 {
		t.Errorf("shrink must keep the head and leave the receiver: %s, %d, %d",
			s.Queries[0].QueryID, s.Summary.Returned, len(r.Queries))
	}

	next, _, ok = s.shrink()
	if !ok || len(next.(*compareResult).Queries) != compareFloorLimit {
		t.Fatalf("second shrink must reach the floor")
	}

	if _, hint, ok := next.shrink(); ok || !strings.Contains(hint, "limit") {
		t.Errorf("shrink below the floor: ok=%v hint=%q", ok, hint)
	}
}

func TestQueryCompare_BudgetShrinksThroughRender(t *testing.T) {
	t.Parallel()

	items := make([]apiclient.QueryCompareItem, 0, 100)
	for i := range 100 {
		it := cmpItem(strconv.Itoa(i), cmpSide(10, 10, 1, 0), cmpSide(20, float64(20+i), 1, 0))
		it.Query = strings.Repeat("x", 500)
		items = append(items, it)
	}

	ctx, rec := budgetCtx(8 << 10)

	res, _, _ := renderResult(ctx, buildCompare(items, compareSortExecTime, 100), nil)
	if res.IsError {
		t.Fatalf("refused: %s", contentText(res.Content[0]))
	}

	if len(rec.steps) == 0 || !strings.Contains(contentText(res.Content[0]), `"reason":"budget"`) {
		t.Errorf("steps = %v, note = %s", rec.steps, contentText(res.Content[0]))
	}
}
