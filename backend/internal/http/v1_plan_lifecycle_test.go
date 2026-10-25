package http

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/gen/serverhttp"
)

func analyzeRequest(t *testing.T, trace string, plan *string) serverhttp.AnalyzePlanLifecycleResponseObject {
	t.Helper()

	s := &Handlers{} //nolint:exhaustruct

	resp, err := s.AnalyzePlanLifecycle(context.Background(), serverhttp.AnalyzePlanLifecycleRequestObject{
		Body: &serverhttp.PlanLifecycleRequest{Trace: trace, Plan: plan},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	return resp
}

func demoTrace(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "pathprobe", "testdata", "demo-join.json"))
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func TestAnalyzePlanLifecycle_DemoWithPlan(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("..", "pathprobe", "testdata", "demo-join.plan.json"))
	if err != nil {
		t.Fatal(err)
	}

	plan := string(body)

	resp, ok := analyzeRequest(t, demoTrace(t), &plan).(serverhttp.AnalyzePlanLifecycle200JSONResponse)
	if !ok {
		t.Fatal("want 200")
	}

	if resp.Plan == nil || resp.Match == nil || string(resp.Match.Status) != "match" || resp.Match.Mismatch != nil {
		t.Fatalf("plan %v match %+v", resp.Plan != nil, resp.Match)
	}

	for _, p := range resp.Paths {
		if p.PathId == 2 && (p.PlanNode == nil || len(*p.PlanNode) != 2 || (*p.PlanNode)[0] != 1 || (*p.PlanNode)[1] != 0) {
			t.Errorf("path 2 plan node %v", p.PlanNode)
		}
	}

	// the plan summary is Log Insights' mapping; the match is new
	raw, err := json.Marshal(resp.Match)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), ":null") {
		t.Error("match carries a null")
	}
}

func TestAnalyzePlanLifecycle_Demo(t *testing.T) {
	t.Parallel()

	resp, ok := analyzeRequest(t, demoTrace(t), nil).(serverhttp.AnalyzePlanLifecycle200JSONResponse)
	if !ok {
		t.Fatal("want 200")
	}

	if resp.Final.Status != serverhttp.Exact || resp.Final.PathId == nil || *resp.Final.PathId != 24 {
		t.Errorf("final %+v", resp.Final)
	}

	if len(resp.Paths) != 24 || len(resp.Timeline) != 48 || resp.Plan != nil {
		t.Errorf("%d paths, %d events, plan %v", len(resp.Paths), len(resp.Timeline), resp.Plan != nil)
	}

	if len(resp.Rels) != 2 || resp.Rels[0].Index != 1 || resp.Rels[0].Alias != "c" {
		t.Errorf("rels %+v", resp.Rels)
	}

	// required arrays are [] and never null
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), ":null") {
		t.Error("response carries a null")
	}
}

func TestAnalyzePlanLifecycle_WithPlan(t *testing.T) {
	t.Parallel()

	plan := `[{"Plan": {"Node Type": "Seq Scan", "Relation Name": "pp_orders", "Alias": "o",
		"Startup Cost": 0.00, "Total Cost": 28894.00, "Plan Rows": 1000000, "Plan Width": 20}}]`

	resp, ok := analyzeRequest(t, demoTrace(t), &plan).(serverhttp.AnalyzePlanLifecycle200JSONResponse)
	if !ok {
		t.Fatal("want 200")
	}

	if resp.Plan == nil || resp.Plan.Root.Type != "Seq Scan" {
		t.Errorf("plan %+v", resp.Plan)
	}
}

func TestAnalyzePlanLifecycle_Errors(t *testing.T) {
	t.Parallel()

	if _, ok := analyzeRequest(t, "  ", nil).(serverhttp.AnalyzePlanLifecycle400JSONResponse); !ok {
		t.Error("empty trace: want 400")
	}

	secretPlan := "{not a plan secret_relation"

	cases := []struct {
		name, trace string
		plan        *string
		input       serverhttp.PlanLifecycleParseErrorInput
		code        string
	}{
		{"trace not json", "SELECT secret_column FROM t", nil, serverhttp.PlanLifecycleParseErrorInputTrace, "trace_syntax"},
		{"trace map key", `{"rels": {"secret_alias": 5}, "events": []}`, nil,
			serverhttp.PlanLifecycleParseErrorInputTrace, "trace_type"},
		{"plan garbage", demoTrace(t), &secretPlan, serverhttp.PlanLifecycleParseErrorInputPlan, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			resp, ok := analyzeRequest(t, c.trace, c.plan).(serverhttp.AnalyzePlanLifecycle422JSONResponse)
			if !ok {
				t.Fatal("want 422")
			}

			if resp.Input != c.input || (c.code != "" && resp.Code != c.code) {
				t.Errorf("got %s %s, want %s %s", resp.Input, resp.Code, c.input, c.code)
			}

			raw, err := json.Marshal(resp)
			if err != nil {
				t.Fatal(err)
			}

			if strings.Contains(string(raw), "secret") {
				t.Errorf("422 quotes the input: %s", raw)
			}
		})
	}
}

func TestPlanLifecycleFailureHidesInput(t *testing.T) {
	t.Parallel()

	if strings.ContainsAny(errPlanLifecycleFailed.Error(), "{}\"") {
		t.Errorf("the 500 error is not a fixed text: %q", errPlanLifecycleFailed)
	}
}
