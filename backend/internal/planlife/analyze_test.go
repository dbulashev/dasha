package planlife

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/internal/pathprobe"
)

// Real and hand-written traces: pathprobe/testdata/README.md.
func analyze(t *testing.T, name string) Analysis {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "pathprobe", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return analyzeBody(t, body)
}

func analyzeBody(t *testing.T, body []byte) Analysis {
	t.Helper()

	tr, err := pathprobe.Parse(body)
	if err != nil {
		t.Fatal(err)
	}

	return Analyze(tr)
}

func path(t *testing.T, a Analysis, id int) PathView {
	t.Helper()

	pv, ok := a.Path(id)
	if !ok {
		t.Fatalf("no path %d", id)
	}

	return *pv
}

func hasCaveat(a Analysis, code string, match func(Caveat) bool) bool {
	for _, c := range a.Caveats {
		if c.Code == code && (match == nil || match(c)) {
			return true
		}
	}

	return false
}

func cause(p PathView) CauseCode {
	if p.Cause == nil {
		return ""
	}

	return p.Cause.Code
}

// Acceptance 1: the final path of the join demo is the Hash Join (path 24)
// over Seq Scan o and Index Only Scan c.
func TestDemoJoin_Final(t *testing.T) {
	a := analyze(t, "demo-join.json")

	if a.Final.Status != FinalExact || a.Final.PathID != 24 {
		t.Fatalf("final %+v", a.Final)
	}

	root := path(t, a, 24)
	outer, inner := path(t, a, root.OuterID), path(t, a, root.InnerID)

	if root.PathType != "HashJoin" || outer.PathType != "SeqScan" || outer.Rel != "{2}" ||
		inner.PathType != "IndexOnlyScan" || inner.Rel != "{1}" {
		t.Errorf("final tree: %s(%s %s, %s %s)", root.PathType, outer.PathType, outer.Rel, inner.PathType, inner.Rel)
	}

	for _, id := range a.Final.Tree {
		if f := path(t, a, id).Fate; f != FateChosen {
			t.Errorf("path %d of the final tree: %s", id, f)
		}
	}

	if p := path(t, a, 1); p.KeptReason == nil || *p.KeptReason != "startup" {
		t.Errorf("path 1 kept_reason %v", p.KeptReason)
	}

	for _, c := range a.Caveats {
		if c.Scope == ScopeTrace && c.Code != CaveatIndexNotExamined && c.Code != CaveatPrecheckNoInputs {
			t.Errorf("unexpected trace caveat %+v", c)
		}
	}
}

// Acceptance 2 and 4: the chain of pp_orders_customer_idx, and why its nested
// loops lost.
func TestDemoJoin_IndexChain(t *testing.T) {
	a := analyze(t, "demo-join.json")

	var ch *IndexChain

	for i := range a.Indexes {
		if a.Indexes[i].Index == "pp_orders_customer_idx" {
			ch = &a.Indexes[i]
		}
	}

	if ch == nil {
		t.Fatal("no chain for pp_orders_customer_idx")
	}

	if ch.Outcome != IndexLost || ch.Truncated || ch.OID != 16403 {
		t.Errorf("outcome %s truncated %t oid %d", ch.Outcome, ch.Truncated, ch.OID)
	}

	var leaves []int
	for _, l := range ch.Leaves {
		leaves = append(leaves, l.PathID)
	}

	if !slices.Equal(leaves, []int{7, 8}) {
		t.Fatalf("leaves %v, want [7 8]", leaves)
	}

	// path 8 (parameterized, 100 rows a loop) is the inner of nested loops 11
	// and 15, and of the memoize paths under 12 and 16
	var up []int
	for _, n := range ch.Leaves[1].Up {
		up = append(up, n.PathID)
	}

	for _, want := range []int{11, 15} {
		if !slices.Contains(up, want) {
			t.Errorf("path 8 up %v, missing %d", up, want)
		}
	}

	nl11, nl15 := path(t, a, 11), path(t, a, 15)

	if nl11.Fate != FateDisplaced || nl11.By != 15 || nl11.BySeq != 25 || cause(nl11) != CauseLostOnStartup ||
		*nl11.Cause.Gap >= 0 || *nl11.Cause.Startup != 0.71 || *nl11.Cause.WinnerStartup != 0.42 {
		t.Errorf("path 11: fate %s by %d/#%d cause %+v", nl11.Fate, nl11.By, nl11.BySeq, nl11.Cause)
	}

	if nl15.Fate != FateDisplaced || nl15.By != 18 || cause(nl15) != CauseCostlierTotal {
		t.Errorf("path 15: fate %s by %d cause %+v", nl15.Fate, nl15.By, nl15.Cause)
	}

	// path 7 (unparameterized) went into merge join 14, rejected by nested loop 11 (#18)
	mj := path(t, a, 14)
	if mj.InnerID != 7 || mj.Fate != FateRejected || mj.By != 11 || mj.BySeq != 18 || cause(mj) != CauseCostlierTotal {
		t.Errorf("path 14: inner %d fate %s by %d/#%d cause %+v", mj.InnerID, mj.Fate, mj.By, mj.BySeq, mj.Cause)
	}

	if !hasCaveat(a, CaveatIndexNotExamined, nil) {
		t.Error("no index_not_examined caveat next to the index list")
	}
}

// Acceptance 3: the join relation lists its paths by method and the precheck
// skips as groups.
func TestDemoJoin_Relation(t *testing.T) {
	a := analyze(t, "demo-join.json")

	var r *Relation

	for i := range a.Relations {
		if a.Relations[i].Key == "{1,2}" {
			r = &a.Relations[i]
		}
	}

	if r == nil {
		t.Fatal("no relation {1,2}")
	}

	if r.Label != "c ⋈ o" {
		t.Errorf("label %q", r.Label)
	}

	byType := map[string][]int{}
	for _, id := range r.PathIDs {
		pt := path(t, a, id).PathType
		byType[pt] = append(byType[pt], id)
	}

	for typ, want := range map[string][]int{
		"HashJoin":  {18, 19, 24},
		"NestLoop":  {11, 12, 15, 16, 20, 22},
		"MergeJoin": {10, 14},
	} {
		got := slices.Clone(byType[typ])
		slices.Sort(got)

		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", typ, got, want)
		}
	}

	skipped := 0
	for _, g := range r.Precheck {
		skipped += g.Count

		if g.MinCost > g.MaxCost || g.MinCost <= 0 || g.Reason == "" {
			t.Errorf("precheck group %+v", g)
		}
	}

	if skipped != 13 {
		t.Errorf("%d precheck skips, want 13", skipped)
	}

	if !hasCaveat(a, CaveatPrecheckNoInputs, func(c Caveat) bool { return c.Rel == "{1,2}" }) {
		t.Error("no precheck_no_inputs caveat on {1,2}")
	}
}

func TestDemoIndex_Final(t *testing.T) {
	a := analyze(t, "demo-index.json")

	if a.Final.Status != FinalExact || a.Final.PathID != 11 {
		t.Fatalf("final %+v", a.Final)
	}

	if root := path(t, a, 11); root.PathType != "NestLoop" {
		t.Errorf("final path is %s, want NestLoop", root.PathType)
	}

	// path 8 is displaced by BitmapHeapScan 9 (#13) and stays its bitmapqual
	if p := path(t, a, 8); p.Fate != FateChosen {
		t.Errorf("path 8: %s, want chosen", p.Fate)
	}

	if !hasCaveat(a, CaveatChosenAsBitmapQual, func(c Caveat) bool { return c.PathID == 8 && c.Params["seq"] == "13" }) {
		t.Error("no chosen_as_bitmapqual caveat on path 8")
	}
}

// pathprobe_graph.sql section 8: cN (4) removes cA (2), then cB (3) rejects cN.
func TestRemoved(t *testing.T) {
	a := analyze(t, "removed.json")

	if a.Final.Status != FinalExact || a.Final.PathID != 3 {
		t.Fatalf("final %+v", a.Final)
	}

	if p := path(t, a, 2); p.Fate != FateRemoved || p.By != 4 || p.BySeq != 7 || cause(p) != CauseLostNotOnCost ||
		*p.Cause.Gap >= 0 {
		t.Errorf("cA: fate %s by %d/#%d cause %+v", p.Fate, p.By, p.BySeq, p.Cause)
	}

	if p := path(t, a, 4); p.Fate != FateRejected || p.By != 3 || p.BySeq != 5 || cause(p) != CauseLostNotOnCost {
		t.Errorf("cN: fate %s by %d/#%d cause %+v", p.Fate, p.By, p.BySeq, p.Cause)
	}

	if p := path(t, a, 5); p.Fate != FateRejected || p.By != 3 || cause(p) != CauseCostlierTotal {
		t.Errorf("cD: fate %s by %d cause %+v", p.Fate, p.By, p.Cause)
	}

	if p := path(t, a, 1); p.Fate != FateDisplaced || p.By != 2 || cause(p) != CauseCostlierTotal {
		t.Errorf("seq scan: fate %s by %d cause %+v", p.Fate, p.By, p.Cause)
	}
}

// pathprobe.min_gap drops the remover's rejected event; the remover has no events left.
func TestRemoved_RemoverNotRecorded(t *testing.T) {
	a := analyzeBody(t, []byte(`{
		"events": [
			{"seq": 1, "src": "wp", "rel": "{1}", "pathtype": "CustomScan", "cost_startup": 0, "cost_total": 10, "kind": "accepted", "path_id": 2},
			{"seq": 2, "src": "wp", "rel": "{1}", "pathtype": "CustomScan", "cost_startup": 0, "cost_total": 10, "kind": "removed", "path_id": 2,
			 "remover_path_id": 4, "remover_seq": null, "remover_pathtype": "CustomScan"}
		],
		"paths": [
			{"path_id": 2, "pathtype": "CustomScan", "rel": "{1}", "rows": 1},
			{"path_id": 4, "pathtype": "CustomScan", "rel": "{1}", "rows": 1}
		],
		"coverage": {"survived_snapshot": true, "add_path_accepted": true, "add_path_rejected": false,
			"add_path_displaced": true, "add_path_removed": true, "add_path_precheck": true, "join_excluded": true}
	}`))

	if p := path(t, a, 2); p.Fate != FateRemoved || p.By != 4 || p.BySeq != 0 || cause(p) != CauseUnknown || p.Cause.Gap != nil {
		t.Errorf("path 2: fate %s by %d/#%d cause %+v", p.Fate, p.By, p.BySeq, p.Cause)
	}

	if p := path(t, a, 4); p.Fate != FateUnknown || p.FateReason != ReasonNoEvents {
		t.Errorf("remover: %s %s", p.Fate, p.FateReason)
	}

	if !hasCaveat(a, CaveatCoverageMissing, func(c Caveat) bool { return c.Params["point"] == "add_path_rejected" }) {
		t.Error("no coverage_missing for add_path_rejected")
	}
}

// child_path_ids: the chosen BitmapHeapScan reaches both indexes through BitmapOr.
func TestBitmapOr(t *testing.T) {
	a := analyze(t, "bitmap-or.json")

	if a.Final.Status != FinalExact || a.Final.PathID != 2 || !slices.Equal(a.Final.Tree, []int{2, 3, 4, 5}) {
		t.Fatalf("final %+v", a.Final)
	}

	for _, name := range []string{"gr_o_customer_idx", "gr_o_pkey"} {
		var ch *IndexChain

		for i := range a.Indexes {
			if a.Indexes[i].Index == name {
				ch = &a.Indexes[i]
			}
		}

		if ch == nil || ch.Outcome != IndexInPlan || len(ch.Leaves) != 1 ||
			len(ch.Leaves[0].Up) != 1 || ch.Leaves[0].Up[0].PathID != 3 ||
			len(ch.Leaves[0].Up[0].Up) != 1 || ch.Leaves[0].Up[0].Up[0].PathID != 2 {
			t.Errorf("%s: %+v", name, ch)
		}
	}

	if hasCaveat(a, CaveatEdgesAbsent, nil) {
		t.Error("edges_absent on a trace with all edges")
	}
}

// Paths of a set operation's inputs are planned with the subquery's range table.
func TestSetOp(t *testing.T) {
	a := analyze(t, "setop.json")

	if a.Final.Status != FinalExact || a.Final.PathID != 5 || !slices.Equal(a.Final.Tree, []int{5, 3, 1, 4, 2}) {
		t.Fatalf("final %+v", a.Final)
	}

	if !path(t, a, 1).InSubquery || path(t, a, 3).InSubquery {
		t.Error("InSubquery: want path 1 only below the SubqueryScan")
	}

	if !hasCaveat(a, CaveatSubqueryRels, nil) {
		t.Error("no subquery_rels caveat")
	}
}

func TestEdgesAbsent(t *testing.T) {
	a := analyzeBody(t, []byte(`{
		"events": [{"seq": 1, "src": "p1", "rel": "?", "pathtype": "Agg", "cost_startup": 0, "cost_total": 50, "kind": "accepted", "path_id": 1}],
		"paths": [{"path_id": 1, "pathtype": "Agg", "rel": "?", "rows": 10}]
	}`))

	if !hasCaveat(a, CaveatEdgesAbsent, func(c Caveat) bool { return c.PathID == 1 }) {
		t.Error("no edges_absent on an Agg without subpath_id")
	}
}

func TestDominatorNotReported(t *testing.T) {
	a := analyzeBody(t, []byte(`{
		"events": [{"seq": 1, "src": "p1", "rel": "{1}", "pathtype": "SeqScan", "cost_total": 50, "kind": "rejected", "path_id": 1,
			"dominator_path_id": null, "dominator_pathtype": null}],
		"paths": [{"path_id": 1, "pathtype": "SeqScan", "rel": "{1}", "rows": 10}]
	}`))

	if !hasCaveat(a, CaveatDominatorNotReported, nil) {
		t.Error("no dominator_not_reported caveat")
	}

	if p := path(t, a, 1); p.Fate != FateRejected || p.By != 0 || cause(p) != CauseUnknown {
		t.Errorf("path 1: %s by %d cause %+v", p.Fate, p.By, p.Cause)
	}
}

// Acceptance 6: without acceptance logging an accepted path's later fate is
// unknown, not "survived", and the page says why.
func TestNoAccepted(t *testing.T) {
	a := analyze(t, "no-accepted.json")

	if !hasCaveat(a, CaveatCoverageMissing, func(c Caveat) bool { return c.Params["point"] == "add_path_displaced" }) {
		t.Error("no coverage_missing for add_path_displaced")
	}

	if hasCaveat(a, CaveatCoverageMissing, func(c Caveat) bool { return c.Params["point"] == "index_build" }) {
		t.Error("coverage_missing for index_build without hypothetical indexes")
	}

	// the dominator's accepted event is not logged: its cost comes from its survived event
	if p := path(t, a, 3); p.Fate != FateRejected || p.By != 2 || p.BySeq != 0 || cause(p) != CauseCostlierTotal {
		t.Errorf("path 3: %s by %d/#%d cause %+v", p.Fate, p.By, p.BySeq, p.Cause)
	}

	if p := path(t, a, 4); p.Fate != FateChildOnly {
		t.Errorf("path 4 (bitmapqual of 3): %s", p.Fate)
	}
}

// Acceptance 7.
func TestTruncated(t *testing.T) {
	a := analyze(t, "truncated.json")

	if !hasCaveat(a, CaveatEventsTruncated, nil) {
		t.Error("no events_truncated caveat")
	}

	// no upper events and no survivors: the trace cannot name a final path
	if a.Final.Status != FinalNone || !hasCaveat(a, CaveatFinalNone, nil) {
		t.Errorf("final %+v", a.Final)
	}
}

// Invariants on every trace of testdata.
func TestInvariants(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("..", "pathprobe", "testdata", "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no traces: %v", err)
	}

	for _, name := range names {
		if strings.HasSuffix(name, ".plan.json") {
			continue // EXPLAIN of a demo query, not a trace
		}

		t.Run(filepath.Base(name), func(t *testing.T) {
			a := analyze(t, filepath.Base(name))

			if hasCaveat(a, CaveatChosenButLost, nil) {
				t.Error("a path of the final tree was pushed out")
			}

			if hasCaveat(a, CaveatDominatorNotReported, nil) {
				t.Error("rejected without a dominator: testdata comes from the current core patch")
			}

			bySeq := map[int]pathprobe.Event{}
			for _, ev := range a.Timeline {
				bySeq[ev.Seq] = ev
			}

			for _, ev := range a.Timeline {
				if ev.Kind != pathprobe.KindRemoved || ev.BySeq == 0 {
					continue
				}

				if r := bySeq[ev.BySeq]; r.Kind != pathprobe.KindRejected || r.PathID != ev.ByID || r.Seq > ev.Seq {
					t.Errorf("removed #%d: remover event #%d is %s of path %d", ev.Seq, ev.BySeq, r.Kind, r.PathID)
				}
			}

			for _, p := range a.Paths {
				if p.By != 0 {
					if _, ok := a.Path(p.By); !ok {
						t.Errorf("path %d lost to %d, which does not exist", p.ID, p.By)
					}
				}

				switch p.Fate {
				case FateRejected, FateDisplaced, FateRemoved:
					if p.By == 0 || p.Cause == nil {
						t.Errorf("path %d %s without a winner or a cause", p.ID, p.Fate)
					}
				case FateChosen, FateSurvived, FateChildOnly, FateUnknown:
				}
			}
		})
	}
}
