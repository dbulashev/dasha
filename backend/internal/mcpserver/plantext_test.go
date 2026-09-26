package mcpserver

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/gen/apiclient"
)

func ptr[T any](v T) *T { return &v }

func scanNode(rel string) apiclient.PlanNode {
	return apiclient.PlanNode{ //nolint:exhaustruct
		Type:      "Seq Scan",
		Relation:  ptr(rel),
		Schema:    ptr("public"),
		PlanRows:  100,
		PlanWidth: 8,
		TotalCost: 1000,
		Children:  []apiclient.PlanNode{},
	}
}

func TestRenderPlan_TreeConditionsAndMarks(t *testing.T) {
	t.Parallel()

	inner := scanNode("orders")
	inner.Alias = ptr("o")
	inner.Filter = ptr(strings.Repeat("a", 50))
	inner.FilterOmittedBytes = ptr(1000)
	inner.RowsRemovedByFilter = ptr(12345.0)
	inner.Actual = &apiclient.PlanNodeActual{Loops: 1, Rows: 40000, StartupTimeMs: ptr(0.1), TotalTimeMs: ptr(480.5)} //nolint:exhaustruct

	root := apiclient.PlanNode{ //nolint:exhaustruct
		Type:      "Nested Loop",
		JoinType:  ptr("Left"),
		PlanRows:  10,
		TotalCost: 1234,
		Children:  []apiclient.PlanNode{scanNode("customers"), inner},
	}

	findings := []apiclient.PlanFinding{{ //nolint:exhaustruct
		Code: apiclient.SeqScanLarge, Severity: apiclient.MEDIUM, NodeType: "Seq Scan", Path: []int{1},
	}}

	got := renderPlan(root, findings, planTextOpts{condBytes: 20, childCap: 10})
	lines := strings.Split(got, "\n")

	if !strings.HasPrefix(lines[0], "Nested Loop [Left] (cost=0.00..1234.00 rows=10") {
		t.Errorf("root line = %q", lines[0])
	}

	want := "  -> Seq Scan on public.orders o (cost=0.00..1000.00 rows=100 width=8) " +
		"(actual time=0.100..480.500 rows=40000 loops=1)  !! seq_scan_large"
	if !strings.Contains(got, want+"\n") {
		t.Errorf("marked scan line missing:\n%s", got)
	}

	if !strings.Contains(got, "Filter: "+strings.Repeat("a", 20)+"…[+1030 bytes]") {
		t.Errorf("filter must be clipped and count the API's cut too:\n%s", got)
	}

	if !strings.Contains(got, "Rows Removed by Filter: 12345") {
		t.Errorf("rows removed missing:\n%s", got)
	}

	if strings.Count(got, "!!") != 1 {
		t.Errorf("only the scan carries a finding:\n%s", got)
	}
}

func TestRenderPlan_FoldsChildrenButKeepsFindingPath(t *testing.T) {
	t.Parallel()

	root := apiclient.PlanNode{Type: "Append", Children: []apiclient.PlanNode{}} //nolint:exhaustruct
	for i := range 30 {
		root.Children = append(root.Children, scanNode("p"+strconv.Itoa(i)))
	}

	findings := []apiclient.PlanFinding{{Code: apiclient.SeqScanLarge, Path: []int{25}}} //nolint:exhaustruct

	got := renderPlan(root, findings, planTextOpts{condBytes: 100, childCap: 3})

	for _, want := range []string{"public.p0 ", "public.p2 ", "public.p25 ", "26 more children folded"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "public.p3 ") {
		t.Errorf("child past the cap without a finding must be folded:\n%s", got)
	}
}

func TestNodeLabel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		n    apiclient.PlanNode
		want string
	}{
		{apiclient.PlanNode{Type: "Aggregate", Strategy: ptr("Hashed"), PartialMode: ptr("Partial")}, "Partial Aggregate [Hashed]"}, //nolint:exhaustruct
		{apiclient.PlanNode{Type: "Aggregate", Strategy: ptr("Plain")}, "Aggregate"},                                                //nolint:exhaustruct
		{apiclient.PlanNode{Type: "Hash Join", JoinType: ptr("Inner")}, "Hash Join"},                                                //nolint:exhaustruct
		{apiclient.PlanNode{Type: "Seq Scan", Parallel: true}, "Parallel Seq Scan"},                                                 //nolint:exhaustruct
		{apiclient.PlanNode{Type: "ModifyTable", Operation: ptr("Update")}, "ModifyTable [Update]"},                                 //nolint:exhaustruct
	} {
		if got := nodeLabel(tc.n); got != tc.want {
			t.Errorf("nodeLabel = %q, want %q", got, tc.want)
		}
	}
}

func TestClipOmitted(t *testing.T) {
	t.Parallel()

	if got := clipOmitted("short", 10, 0); got != "short" {
		t.Errorf("unclipped = %q", got)
	}

	if got := clipOmitted("short", 10, 7); got != "short…[+7 bytes]" {
		t.Errorf("API-clipped = %q", got)
	}

	if got := clipOmitted("жжжж", 3, 0); got != "ж…[+6 bytes]" {
		t.Errorf("rune boundary = %q", got)
	}
}

func TestNodeDetails_WorkersLaunchedOnlyWhenMeasured(t *testing.T) {
	t.Parallel()

	n := scanNode("orders")
	n.WorkersPlanned = ptr(4)

	if got := strings.Join(nodeDetails(n, 100), "\n"); !strings.Contains(got, "Workers Planned: 4") || strings.Contains(got, "Launched") {
		t.Errorf("estimate-only = %q", got)
	}

	n.WorkersLaunched = ptr(0)

	if got := strings.Join(nodeDetails(n, 100), "\n"); !strings.Contains(got, "Workers Planned: 4 Launched: 0") {
		t.Errorf("measured = %q", got)
	}
}
