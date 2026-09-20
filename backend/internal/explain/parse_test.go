package explain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParse_TextMatchesJSON is the acceptance test for the parsers: the same
// statement logged in both formats has to yield the same tree, on every
// supported version. A difference here is a parser bug, not a server one.
func TestParse_TextMatchesJSON(t *testing.T) {
	for _, version := range versions {
		for _, verbose := range []string{"verbose_off", "verbose_on"} {
			t.Run(version+"/"+verbose, func(t *testing.T) {
				text := parseFixture(t, version+"/text_"+verbose+".txt", SourceLog)
				js := parseFixture(t, version+"/json_"+verbose+".json", SourceLog)

				if text.QueryText != js.QueryText {
					t.Errorf("query text:\n text %q\n json %q", text.QueryText, js.QueryText)
				}

				compareNodes(t, "root", &text.Root, &js.Root)

				if got, want := Hash(text.Root), Hash(js.Root); got != want {
					t.Errorf("hash: text %s, json %s", got, want)
				}

				if text.Caps != js.Caps {
					t.Errorf("capabilities: text %+v, json %+v", text.Caps, js.Caps)
				}

				if text.HasQueryID != js.HasQueryID || text.QueryID != js.QueryID {
					t.Errorf("query id: text %d/%t, json %d/%t", text.QueryID, text.HasQueryID, js.QueryID, js.HasQueryID)
				}
			})
		}
	}
}

// TestParse_ShapeCorpusMatches holds every case the probe logged in both
// formats to the same rule. The pair is named <case>.txt / <case>.json.
func TestParse_ShapeCorpusMatches(t *testing.T) {
	pairs := 0

	for _, version := range versions {
		entries, err := os.ReadDir(filepath.Join("testdata", version))
		if err != nil {
			t.Fatalf("read testdata/%s: %v", version, err)
		}

		for _, entry := range entries {
			name, ok := strings.CutSuffix(entry.Name(), ".json")
			if !ok {
				continue
			}

			if _, err := os.Stat(filepath.Join("testdata", version, name+".txt")); err != nil {
				continue
			}

			pairs++

			t.Run(version+"/"+name, func(t *testing.T) {
				text := parseFixture(t, filepath.Join(version, name+".txt"), SourceLog)
				js := parseFixture(t, filepath.Join(version, name+".json"), SourceLog)

				if text.QueryText != js.QueryText {
					t.Errorf("query text:\n text %q\n json %q", text.QueryText, js.QueryText)
				}

				if text.QueryParams != js.QueryParams {
					t.Errorf("query parameters: text %q, json %q", text.QueryParams, js.QueryParams)
				}

				compareNodes(t, "root", &text.Root, &js.Root)

				if got, want := Hash(text.Root), Hash(js.Root); got != want {
					t.Errorf("hash: text %s, json %s", got, want)
				}

				if text.Caps != js.Caps {
					t.Errorf("capabilities: text %+v, json %+v", text.Caps, js.Caps)
				}
			})
		}
	}

	if pairs == 0 {
		t.Skip("no shape corpus in testdata")
	}
}

// compareNodes checks everything both formats state about a node. The measured
// numbers are left out: the two fixtures are two runs of the same statement.
func compareNodes(t *testing.T, path string, text, js *Node) {
	t.Helper()

	fields := []struct {
		name       string
		text, json any
	}{
		{"type", text.Type, js.Type},
		{"strategy", text.Strategy, js.Strategy},
		{"partial mode", text.PartialMode, js.PartialMode},
		{"join type", text.JoinType, js.JoinType},
		{"operation", text.Operation, js.Operation},
		{"scan direction", text.ScanDirection, js.ScanDirection},
		{"schema", text.Schema, js.Schema},
		{"relation", text.Relation, js.Relation},
		{"alias", text.Alias, js.Alias},
		{"index name", text.IndexName, js.IndexName},
		{"parent relationship", text.ParentRel, js.ParentRel},
		{"parallel", text.Parallel, js.Parallel},
		{"startup cost", text.StartupCost, js.StartupCost},
		{"total cost", text.TotalCost, js.TotalCost},
		{"plan rows", text.PlanRows, js.PlanRows},
		{"plan width", text.PlanWidth, js.PlanWidth},
		{"filter", text.Filter, js.Filter},
		{"index cond", text.IndexCond, js.IndexCond},
		{"hash cond", text.HashCond, js.HashCond},
		{"merge cond", text.MergeCond, js.MergeCond},
		{"join filter", text.JoinFilter, js.JoinFilter},
		{"children", len(text.Children), len(js.Children)},
	}

	for _, f := range fields {
		if f.text != f.json {
			t.Errorf("%s: %s text %v, json %v", path, f.name, f.text, f.json)
		}
	}

	if (text.Actual == nil) != (js.Actual == nil) {
		t.Errorf("%s: actual present in text %t, in json %t", path, text.Actual != nil, js.Actual != nil)
	}

	if (text.Buffers == nil) != (js.Buffers == nil) {
		t.Errorf("%s: buffers present in text %t, in json %t", path, text.Buffers != nil, js.Buffers != nil)
	}

	if len(text.Children) != len(js.Children) {
		return
	}

	for i := range text.Children {
		compareNodes(t, path+"/"+text.Children[i].Type, &text.Children[i], &js.Children[i])
	}
}

func TestParseText_SortStillInProgress(t *testing.T) {
	p := parseFixture(t, "pg17/sort_spill.txt", SourceLog)

	sort := findNode(&p, "Sort")
	if sort == nil {
		t.Fatal("no Sort node")
	}

	if sort.SortMethod != "still in progress" {
		t.Errorf("sort method: %q", sort.SortMethod)
	}

	if sort.SortSpaceType != "Memory" || sort.SortSpaceKB == nil || *sort.SortSpaceKB != 0 {
		t.Errorf("sort space: %q %v", sort.SortSpaceType, sort.SortSpaceKB)
	}

	if len(sort.SortKey) != 1 || sort.SortKey[0] != "payload" {
		t.Errorf("sort key: %v", sort.SortKey)
	}
}

// A per-worker block is indented past the node's own attributes and repeats the
// keys; the node must keep its own numbers.
func TestParseText_WorkerBlockKeepsNodeBuffers(t *testing.T) {
	p := parseFixture(t, "pg17/text_verbose_on.txt", SourceLog)

	partial := findNode(&p, "Seq Scan")
	if partial == nil || partial.Buffers == nil {
		t.Fatal("no Seq Scan with buffers")
	}

	if partial.Buffers.SharedHit != 576 || partial.Buffers.SharedRead != 2232 {
		t.Errorf("buffers: %+v", partial.Buffers)
	}

	if partial.Schema != "public" || partial.Relation != "probe_t" {
		t.Errorf("relation: %q.%q", partial.Schema, partial.Relation)
	}

	if !p.HasQueryID || p.QueryID != -4452854032459450605 {
		t.Errorf("query id: %d (%t)", p.QueryID, p.HasQueryID)
	}
}

func TestParseText_NoAnalyze(t *testing.T) {
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			p := parseFixture(t, version+"/analyze_off.txt", SourceLog)

			p.Walk(func(_ []int, n *Node) bool {
				if n.Actual != nil {
					t.Errorf("%s: actual numbers in a plan built without ANALYZE", n.Type)
				}

				return true
			})

			if p.Caps.Actual || p.Caps.Timing {
				t.Errorf("capabilities: %+v", p.Caps)
			}
		})
	}
}

func TestParseText_TimingOff(t *testing.T) {
	p := parseFixture(t, "synthetic/timing_off.txt", SourceLog)

	if !p.Caps.Actual {
		t.Error("row counts are present, so Actual must be set")
	}

	if p.Caps.Timing {
		t.Error("no times were measured, so Timing must stay unset")
	}

	scan := findNode(&p, "Seq Scan")
	if scan == nil || scan.Actual == nil || scan.Actual.Rows != 300 {
		t.Fatalf("seq scan actual: %+v", scan)
	}
}

func TestParseText_Buffers(t *testing.T) {
	p := parseFixture(t, "synthetic/sort_external.txt", SourceLog)

	sort := findNode(&p, "Sort")
	if sort == nil || sort.Buffers == nil {
		t.Fatal("no Sort with buffers")
	}

	if sort.Buffers.SharedHit != 2816 || sort.Buffers.TempRead != 3201 || sort.Buffers.TempWritten != 3210 {
		t.Errorf("buffers: %+v", sort.Buffers)
	}

	if len(sort.SortKey) != 2 || sort.SortKey[1] != "id DESC" {
		t.Errorf("sort key: %v", sort.SortKey)
	}
}

func TestParseText_TailBlocks(t *testing.T) {
	p := parseFixture(t, "synthetic/triggers_jit.txt", SourceLog)

	if len(p.Triggers) != 1 || p.Triggers[0].Relation != "t" || p.Triggers[0].Time != 340.5 {
		t.Errorf("triggers: %+v", p.Triggers)
	}

	if p.JIT == nil || p.JIT.Functions != 8 || p.JIT.Total == nil || *p.JIT.Total != 320 {
		t.Errorf("jit: %+v", p.JIT)
	}

	if p.ExecutionTime == nil || *p.ExecutionTime != 905.3 {
		t.Errorf("execution time: %v", p.ExecutionTime)
	}

	if p.Settings["work_mem"] != "64MB" || p.Settings["search_path"] != `"$user", public` {
		t.Errorf("settings: %v", p.Settings)
	}

	if !p.Caps.Settings {
		t.Error("settings were parsed, so the capability must be set")
	}

	if root := p.Root; root.Type != "ModifyTable" || root.Operation != "Insert" || root.Relation != "t" {
		t.Errorf("root: %+v", root)
	}
}

// A statement with a single result relation prints the trigger without one.
func TestParseText_TriggerWithoutRelation(t *testing.T) {
	p := parseFixture(t, "synthetic/trigger_no_relname.txt", SourceLog)

	if len(p.Triggers) != 1 {
		t.Fatalf("triggers: %+v", p.Triggers)
	}

	if tr := p.Triggers[0]; tr.Name != "t_audit" || tr.Relation != "" || tr.Time != 340.5 || tr.Calls != 100000 {
		t.Errorf("trigger: %+v", tr)
	}

	if p.ExecutionTime == nil || *p.ExecutionTime != 905.3 {
		t.Errorf("execution time: %v", p.ExecutionTime)
	}
}

func TestParseText_Subplan(t *testing.T) {
	p := parseFixture(t, "synthetic/subplan.txt", SourceLog)

	if p.Root.Type != "Append" || len(p.Root.Children) != 2 {
		t.Fatalf("root: %s with %d children", p.Root.Type, len(p.Root.Children))
	}

	for i, child := range p.Root.Children {
		if child.ParentRel != RelMember {
			t.Errorf("append child %d: parent relationship %q", i, child.ParentRel)
		}
	}

	scanA := p.Root.Children[0]
	if len(scanA.Children) != 1 {
		t.Fatalf("the InitPlan belongs to the scan that references it, got %d children", len(scanA.Children))
	}

	init := scanA.Children[0]
	if init.ParentRel != RelInitPlan || init.SubplanName != "InitPlan 1 (returns $0)" {
		t.Errorf("initplan: %q %q", init.ParentRel, init.SubplanName)
	}

	if init.Type != "Aggregate" || len(init.Children) != 1 {
		t.Errorf("initplan node: %s with %d children", init.Type, len(init.Children))
	}
}

func TestParseText_MultilineQueryText(t *testing.T) {
	p := parseFixture(t, "pg17/join.txt", SourceLog)

	if !strings.HasPrefix(p.QueryText, "SELECT count(*) FROM probe_t a JOIN probe_t b") {
		t.Errorf("query text: %q", p.QueryText)
	}

	join := findNode(&p, "Hash Join")
	if join == nil {
		t.Fatal("no Hash Join node")
	}

	if !join.Parallel || join.JoinType != "Inner" || join.HashCond != "(a.k = b.id)" {
		t.Errorf("join: %+v", join)
	}
}

// A merge join prints its condition and its own qual as two lines; one field for
// both would drop whichever came second.
func TestParseText_MergeJoinKeepsConditionAndFilter(t *testing.T) {
	p := parseFixture(t, "synthetic/merge_join_filter.txt", SourceLog)

	if p.Root.MergeCond != "(newdata.* *= newdata2.*)" {
		t.Errorf("merge cond = %q", p.Root.MergeCond)
	}

	if p.Root.JoinFilter != "(newdata2.ctid <> newdata.ctid)" {
		t.Errorf("join filter = %q", p.Root.JoinFilter)
	}

	if p.Root.HashCond != "" {
		t.Errorf("hash cond = %q, want empty on a merge join", p.Root.HashCond)
	}

	if p.Root.RowsRemovedByJoinFilter == nil || *p.Root.RowsRemovedByJoinFilter != 14519 {
		t.Errorf("rows removed by join filter = %v", p.Root.RowsRemovedByJoinFilter)
	}
}

func TestParseText_IndexScan(t *testing.T) {
	p := parseFixture(t, "synthetic/loops_blowup.txt", SourceLog)

	idx := findNode(&p, "Index Scan")
	if idx == nil {
		t.Fatal("no Index Scan node")
	}

	if idx.IndexName != "b_pkey" || idx.Relation != "b" || idx.ScanDirection != "Forward" {
		t.Errorf("index scan: %+v", idx)
	}

	if idx.IndexCond != "(id = a.b_id)" {
		t.Errorf("index cond: %q", idx.IndexCond)
	}

	if idx.ParentRel != RelInner {
		t.Errorf("parent relationship: %q", idx.ParentRel)
	}
}

func TestParseText_BitmapScans(t *testing.T) {
	p := parseFixture(t, "synthetic/bitmap_lossy.txt", SourceLog)

	heap := findNode(&p, "Bitmap Heap Scan")
	if heap == nil || heap.HeapBlocksLossy == nil || *heap.HeapBlocksLossy != 4300 {
		t.Fatalf("bitmap heap scan: %+v", heap)
	}

	if heap.HeapBlocksExact == nil || *heap.HeapBlocksExact != 1200 {
		t.Errorf("exact blocks: %v", heap.HeapBlocksExact)
	}

	index := findNode(&p, "Bitmap Index Scan")
	if index == nil || index.IndexName != "probe_t_k_idx" || index.Relation != "" {
		t.Fatalf("bitmap index scan: %+v", index)
	}
}

func TestParseText_AsyncForeignScan(t *testing.T) {
	p := parseFixture(t, "synthetic/async_foreign.txt", SourceLog)

	scan := findNode(&p, "Foreign Scan")
	if scan == nil {
		t.Fatal("no Foreign Scan node")
	}

	if scan.Relation != "part_fdw_1" || scan.Alias != "p_1" || scan.Parallel {
		t.Errorf("foreign scan: %+v", scan)
	}

	if scan.ParentRel != RelMember {
		t.Errorf("parent relationship: %q", scan.ParentRel)
	}
}

func TestParseText_QuotedRelation(t *testing.T) {
	p := parseFixture(t, "synthetic/quoted_ident.txt", SourceLog)

	scan := findNode(&p, "Seq Scan")
	if scan == nil {
		t.Fatal("no Seq Scan node")
	}

	if scan.Schema != "My Schema" || scan.Relation != "My Table" || scan.Alias != "t" {
		t.Errorf("relation: %q.%q as %q", scan.Schema, scan.Relation, scan.Alias)
	}
}

func TestParseJSON_ExplainArray(t *testing.T) {
	p := parseFixture(t, "synthetic/generic_plan.json", SourceExplain)

	if p.Root.Type != "Aggregate" || p.Root.PartialMode != "" {
		t.Errorf("root: %s %q", p.Root.Type, p.Root.PartialMode)
	}

	if p.Caps.Actual || p.Caps.Buffers || p.Caps.Timing {
		t.Errorf("a plan without ANALYZE proves nothing: %+v", p.Caps)
	}

	if !p.Caps.Settings || p.Settings["work_mem"] != "64MB" {
		t.Errorf("settings: %v", p.Settings)
	}

	if p.PlanningTime == nil || *p.PlanningTime != 0.234 {
		t.Errorf("planning time: %v", p.PlanningTime)
	}

	scan := findNode(&p, "Seq Scan")
	if scan == nil || scan.Schema != "public" || scan.Relation != "orders" || scan.Alias != "" {
		t.Fatalf("seq scan: %+v", scan)
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{name: "empty", body: "   ", code: CodeEmptyPlan},
		{name: "xml", body: `<explain xmlns="http://www.postgresql.org/2009/explain">`, code: CodeUnsupportedFormat},
		{name: "yaml", body: "Query Text: \"SELECT 1\"\nPlan: \n  Node Type: \"Result\"\n", code: CodeUnsupportedFormat},
		{name: "broken json", body: `{"Plan": {`, code: CodeParseError},
		{name: "json without a plan", body: `{"Query Text": "SELECT 1"}`, code: CodeParseError},
		{name: "text without a node", body: "Query Text: SELECT 1\n", code: CodeParseError},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.body, SourceLog)

			var pe *Error
			if !errors.As(err, &pe) || pe.Code != c.code {
				t.Fatalf("want %s, got %v", c.code, err)
			}
		})
	}
}
