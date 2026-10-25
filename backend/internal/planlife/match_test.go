package planlife

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/pathprobe"
)

func testdata(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "pathprobe", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func analyzePair(t *testing.T, trace, plan string) Analysis {
	t.Helper()

	tr, err := pathprobe.Parse([]byte(trace))
	if err != nil {
		t.Fatal(err)
	}

	p, err := explain.Parse(plan, explain.SourceUpload)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	return AnalyzeWithPlan(tr, &p)
}

func planNode(t *testing.T, a Analysis, id int) []int {
	t.Helper()

	return path(t, a, id).PlanNode
}

// Acceptance 5: both demo traces match the EXPLAIN of their own run, JSON and text.
func TestMatch_Demo(t *testing.T) {
	cases := []struct {
		trace, plan string
		want        map[int][]int // path id -> plan node
	}{
		// Hash Join (Seq Scan o, Hash -> Index Only Scan c)
		{"demo-join.json", "demo-join.plan.json", map[int][]int{24: {}, 6: {0}, 2: {1, 0}}},
		{"demo-join.json", "demo-join.plan.txt", map[int][]int{24: {}, 6: {0}, 2: {1, 0}}},
		// Nested Loop (Index Only Scan c, Bitmap Heap Scan o -> Bitmap Index Scan)
		{"demo-index.json", "demo-index.plan.json", map[int][]int{11: {}, 2: {0}, 9: {1}, 8: {1, 0}}},
		{"demo-index.json", "demo-index.plan.txt", map[int][]int{11: {}, 2: {0}, 9: {1}, 8: {1, 0}}},
	}

	for _, c := range cases {
		t.Run(c.plan, func(t *testing.T) {
			a := analyzePair(t, testdata(t, c.trace), testdata(t, c.plan))

			if a.Match == nil || a.Match.Status != MatchOK || a.Match.QueryText != QueryTextMissing {
				t.Fatalf("match %+v", a.Match)
			}

			for id, want := range c.want {
				if got := planNode(t, a, id); got == nil || !slices.Equal(got, want) {
					t.Errorf("path %d: plan node %v, want %v", id, got, want)
				}
			}

			for _, p := range a.Paths {
				if _, in := c.want[p.ID]; !in && p.PlanNode != nil {
					t.Errorf("path %d outside the final tree matched to %v", p.ID, p.PlanNode)
				}

				if p.RowsRatio != nil {
					t.Errorf("path %d: rows ratio %v; the demo estimates are close", p.ID, *p.RowsRatio)
				}
			}

			if !hasCaveat(a, CaveatPlanQueryText, nil) || hasCaveat(a, CaveatPartialPaths, nil) {
				t.Errorf("caveats %+v", a.Caveats)
			}
		})
	}
}

// Acceptance 5: the plan of one demo query against the trace of the other.
func TestMatch_OtherQuery(t *testing.T) {
	a := analyzePair(t, testdata(t, "demo-join.json"), testdata(t, "demo-index.plan.json"))

	if a.Match.Status != MatchMismatch || a.Match.Mismatch == nil {
		t.Fatalf("match %+v", a.Match)
	}

	d := a.Match.Mismatch
	if len(d.PlanNode) != 0 || d.PathID != 24 || d.Expected != "Hash Join" || d.Found != "Nested Loop" {
		t.Errorf("mismatch %+v", d)
	}

	for _, p := range a.Paths {
		if p.PlanNode != nil {
			t.Errorf("path %d matched in a mismatching plan", p.ID)
		}
	}
}

func TestMatch_QueryText(t *testing.T) {
	trace := testdata(t, "demo-join.json")
	plan := `{"Query Text": %s, "Plan": {"Node Type": "Seq Scan", "Relation Name": "t", "Alias": "t",
		"Startup Cost": 0, "Total Cost": 1, "Plan Rows": 1, "Plan Width": 4}}`

	tr, err := pathprobe.Parse([]byte(trace))
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		text string
		want QueryTextCheck
	}{
		{"SELECT 1", QueryTextDiffers},
		{"  " + tr.Query + " ; ", QueryTextSame},
	} {
		text, err := json.Marshal(c.text)
		if err != nil {
			t.Fatal(err)
		}

		p, err := explain.Parse(fmt.Sprintf(plan, text), explain.SourceUpload)
		if err != nil {
			t.Fatal(err)
		}

		a := AnalyzeWithPlan(tr, &p)
		if a.Match.QueryText != c.want || a.Match.Status != MatchMismatch {
			t.Errorf("%q: %+v", c.text, a.Match)
		}

		if hasCaveat(a, CaveatPlanQueryText, nil) {
			t.Errorf("%q: plan_query_text_missing with a query text", c.text)
		}
	}
}

// Two upper candidates the trace cannot tell apart: the plan picks one.
func TestMatch_FinalByPlan(t *testing.T) {
	trace := `{
		"rels": {"1": "t"},
		"events": [
			{"seq": 1, "src": "p1", "rel": "{1}", "pathtype": "SeqScan", "cost_startup": 0, "cost_total": 8, "kind": "accepted", "path_id": 1},
			{"seq": 2, "src": "p1", "rel": "?", "pathtype": "Result", "cost_startup": 0, "cost_total": 10, "kind": "accepted", "path_id": 2},
			{"seq": 3, "src": "p1", "rel": "?", "pathtype": "Sort", "cost_startup": 15, "cost_total": 20, "kind": "accepted", "path_id": 3}
		],
		"paths": [
			{"path_id": 1, "pathtype": "SeqScan", "rel": "{1}", "rows": 100},
			{"path_id": 2, "pathtype": "Result", "rel": "?", "subpath_id": 1, "rows": 100},
			{"path_id": 3, "pathtype": "Sort", "rel": "?", "subpath_id": 1, "rows": 100}
		]
	}`
	plan := `[{"Plan": {"Node Type": "Sort", "Startup Cost": 15, "Total Cost": 20, "Plan Rows": 100, "Plan Width": 4,
		"Actual Rows": 5000, "Actual Loops": 1,
		"Plans": [{"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Relation Name": "t", "Alias": "t",
			"Startup Cost": 0, "Total Cost": 8, "Plan Rows": 100, "Plan Width": 4, "Actual Rows": 5000, "Actual Loops": 1}]}}]`

	tr, err := pathprobe.Parse([]byte(trace))
	if err != nil {
		t.Fatal(err)
	}

	if a := Analyze(tr); a.Final.Status != FinalAmbiguous || a.Final.PathID != 2 {
		t.Fatalf("without a plan: %+v", a.Final)
	}

	a := analyzePair(t, trace, plan)

	if a.Final.Status != FinalByPlan || a.Final.PathID != 3 || !slices.Equal(a.Final.Tree, []int{3, 1}) {
		t.Fatalf("final %+v", a.Final)
	}

	if hasCaveat(a, CaveatFinalAmbiguous, nil) || a.Match.Status != MatchOK {
		t.Errorf("match %+v caveats %+v", a.Match, a.Caveats)
	}

	if p := path(t, a, 2); p.Fate == FateChosen || p.PlanNode != nil {
		t.Errorf("path 2: %s %v", p.Fate, p.PlanNode)
	}

	// 100 estimated, 5000 actual: past the row_misestimate thresholds
	if p := path(t, a, 1); !slices.Equal(p.PlanNode, []int{0}) || p.RowsRatio == nil || *p.RowsRatio != 50 {
		t.Errorf("path 1: %v ratio %v", p.PlanNode, p.RowsRatio)
	}
}

// A Projection the plan does not need has no Result node; a Gather has no path.
func TestMatch_Rules(t *testing.T) {
	trace := `{
		"rels": {"1": "t"},
		"events": [{"seq": 1, "src": "p1", "rel": "?", "pathtype": "Result", "cost_startup": 0, "cost_total": 10, "kind": "accepted", "path_id": 2}],
		"paths": [
			{"path_id": 1, "pathtype": "SeqScan", "rel": "{1}", "rows": 100},
			{"path_id": 2, "pathtype": "Result", "rel": "?", "subpath_id": 1, "rows": 100}
		]
	}`
	plan := `[{"Plan": {"Node Type": "Gather", "Startup Cost": 0, "Total Cost": 10, "Plan Rows": 100, "Plan Width": 4,
		"Plans": [{"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Parallel Aware": true, "Relation Name": "t",
			"Alias": "t", "Startup Cost": 0, "Total Cost": 8, "Plan Rows": 100, "Plan Width": 4}]}}]`

	a := analyzePair(t, trace, plan)

	if a.Match.Status != MatchWithNotes || !slices.Equal(path(t, a, 1).PlanNode, []int{0}) {
		t.Fatalf("match %+v, path 1 at %v", a.Match, path(t, a, 1).PlanNode)
	}

	if len(a.Match.Notes) != 1 || a.Match.Notes[0].Code != NoteProjectionOmitted || a.Match.Notes[0].PathID != 2 {
		t.Errorf("notes %+v", a.Match.Notes)
	}

	if len(a.Match.Unmatched) != 1 || a.Match.Unmatched[0].Reason != UnmatchedPartial {
		t.Errorf("unmatched %+v", a.Match.Unmatched)
	}

	if !hasCaveat(a, CaveatPartialPaths, nil) {
		t.Error("no partial_paths_unobserved caveat")
	}
}

// The scan matches by type but not by alias.
func TestMatch_Alias(t *testing.T) {
	plan := `[{"Plan": {"Node Type": "Hash Join", "Startup Cost": 0, "Total Cost": 1, "Plan Rows": 1, "Plan Width": 4, "Plans": [
		{"Node Type": "Seq Scan", "Parent Relationship": "Outer", "Relation Name": "pp_customers", "Alias": "c",
			"Startup Cost": 0, "Total Cost": 1, "Plan Rows": 1, "Plan Width": 4},
		{"Node Type": "Hash", "Parent Relationship": "Inner", "Startup Cost": 0, "Total Cost": 1, "Plan Rows": 1, "Plan Width": 4, "Plans": [
			{"Node Type": "Index Only Scan", "Parent Relationship": "Outer", "Relation Name": "pp_customers", "Alias": "c",
				"Index Name": "pp_customers_pkey", "Startup Cost": 0, "Total Cost": 1, "Plan Rows": 1, "Plan Width": 4}]}]}}]`

	a := analyzePair(t, testdata(t, "demo-join.json"), plan)

	d := a.Match.Mismatch
	if a.Match.Status != MatchMismatch || d == nil || !slices.Equal(d.PlanNode, []int{0}) || d.PathID != 6 ||
		d.Expected != "Seq Scan on o" || d.Found != "Seq Scan on c" {
		t.Errorf("match %+v mismatch %+v", a.Match, d)
	}
}
