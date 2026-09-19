package indexadvisor

import (
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/logs/insights"
)

var (
	evidenceFrom = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	evidenceTo   = evidenceFrom.Add(time.Hour)
)

func reportOf(candidates ...Candidate) Report {
	return Report{ //nolint:exhaustruct
		Candidates: candidates,
	}
}

func candidateOn(table string, ids ...int64) Candidate {
	return Candidate{ //nolint:exhaustruct
		Schema:   testSchema,
		Table:    table,
		Columns:  []string{"customer_id"},
		Evidence: Evidence{State: EvidenceNotSearched}, //nolint:exhaustruct
		Covered:  []CoveredQuery{{QueryIDs: ids}},      //nolint:exhaustruct
	}
}

func seqScan(schema, relation string, timeMs, loops, removed float64) explain.Node {
	return explain.Node{ //nolint:exhaustruct
		Type:     "Seq Scan",
		Schema:   schema,
		Relation: relation,
		Actual: &explain.Actual{
			StartupTime: 0,
			TotalTime:   timeMs,
			Rows:        10,
			Loops:       loops,
		},
		RowsRemovedByFilter: &removed,
	}
}

func groupOf(ord int, queryID int64, count int, root explain.Node) insights.PlanGroup {
	return insights.PlanGroup{ //nolint:exhaustruct
		Ord:        ord,
		QueryID:    queryID,
		HasQueryID: true,
		Count:      count,
		Sample:     explain.Plan{Root: root}, //nolint:exhaustruct
	}
}

func TestAttachEvidenceFound(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	window := insights.PlanWindow{
		Groups:  []insights.PlanGroup{groupOf(0, 11, 4, seqScan(testSchema, "orders", 120, 2, 5000))},
		Partial: false,
	}

	AttachEvidence(&rep, window, evidenceFrom, evidenceTo)

	ev := rep.Candidates[0].Evidence
	if ev.State != EvidenceFound {
		t.Fatalf("state = %q, want found", ev.State)
	}

	if ev.Plans != 4 || ev.SeqScanNodes != 1 {
		t.Errorf("plans = %d, nodes = %d; want 4 and 1", ev.Plans, ev.SeqScanNodes)
	}

	// Measured on the sample across its loops, never multiplied by Count.
	if ev.ActualTimeMs != 240 || ev.RowsRemoved != 10000 {
		t.Errorf("time = %v, removed = %v; want 240 and 10000", ev.ActualTimeMs, ev.RowsRemoved)
	}

	if !ev.From.Equal(evidenceFrom) || !ev.To.Equal(evidenceTo) {
		t.Errorf("window = %v..%v", ev.From, ev.To)
	}
}

// A plan of the right statement that scans another table is not evidence for
// this candidate.
func TestAttachEvidenceOtherTable(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	window := insights.PlanWindow{
		Groups:  []insights.PlanGroup{groupOf(0, 11, 3, seqScan(testSchema, "customers", 120, 1, 0))},
		Partial: false,
	}

	AttachEvidence(&rep, window, evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceNotFound {
		t.Fatalf("state = %q, want not_found", got)
	}
}

// The window was read, holds plans, and none of them belongs to this statement:
// an argument against the index, and not the same answer as never having looked.
func TestAttachEvidenceNotFoundIsNotNotSearched(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))

	AttachEvidence(&rep,
		insights.PlanWindow{Groups: nil, Partial: false, PlanRecords: 7},
		evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceNotFound {
		t.Fatalf("state = %q, want not_found", got)
	}
}

// auto_explain wrote nothing over the window: a log source that answered with no
// plan at all is not an argument against any candidate.
func TestAttachEvidenceWindowWithoutPlans(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))

	AttachEvidence(&rep,
		insights.PlanWindow{Groups: nil, Partial: false, PlanRecords: 0},
		evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceNotSearched {
		t.Fatalf("state = %q, want not_searched", got)
	}
}

// Build leaves every candidate unsearched, and a report nothing attached to
// keeps saying so.
func TestBuildLeavesEvidenceUnsearched(t *testing.T) {
	rep := Build(
		workloadOf(entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000)),
		ordersCatalog(),
		Config{}, //nolint:exhaustruct
	)

	if got := onlyCandidate(t, rep).Evidence.State; got != EvidenceNotSearched {
		t.Fatalf("state = %q, want not_searched", got)
	}
}

// A plan logged with log_analyze = off proves the scan without pricing it.
func TestAttachEvidenceWithoutActual(t *testing.T) {
	node := explain.Node{Type: "Seq Scan", Schema: testSchema, Relation: "orders"} //nolint:exhaustruct

	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep,
		insights.PlanWindow{Groups: []insights.PlanGroup{groupOf(0, 11, 2, node)}, Partial: false},
		evidenceFrom, evidenceTo)

	ev := rep.Candidates[0].Evidence
	if ev.State != EvidenceFound || ev.Plans != 2 || ev.ActualTimeMs != 0 {
		t.Fatalf("evidence = %+v", ev)
	}
}

// A plan logged without VERBOSE carries no schema, and the bare name has to match.
func TestAttachEvidenceWithoutSchema(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep,
		insights.PlanWindow{
			Groups:  []insights.PlanGroup{groupOf(0, 11, 1, seqScan("", "orders", 10, 1, 0))},
			Partial: false,
		},
		evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceFound {
		t.Fatalf("state = %q, want found", got)
	}
}

func TestAttachEvidencePartial(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep,
		insights.PlanWindow{
			Groups:  []insights.PlanGroup{groupOf(0, 11, 1, seqScan(testSchema, "orders", 10, 1, 0))},
			Partial: true,
		},
		evidenceFrom, evidenceTo)

	if !rep.Candidates[0].Evidence.Partial {
		t.Error("partial window did not reach the evidence")
	}
}

// Shapes of two statements, both covered: the counts add up over both.
func TestAttachEvidenceSeveralShapes(t *testing.T) {
	c := candidateOn("orders", 11)
	c.Covered = append(c.Covered, CoveredQuery{QueryIDs: []int64{12}}) //nolint:exhaustruct

	rep := reportOf(c)
	AttachEvidence(&rep, insights.PlanWindow{
		Groups: []insights.PlanGroup{
			groupOf(0, 11, 2, seqScan(testSchema, "orders", 100, 1, 10)),
			groupOf(1, 12, 3, seqScan(testSchema, "orders", 50, 1, 5)),
			groupOf(2, 99, 9, seqScan(testSchema, "orders", 900, 1, 900)),
		},
		Partial: false,
	}, evidenceFrom, evidenceTo)

	ev := rep.Candidates[0].Evidence
	if ev.Plans != 5 || ev.SeqScanNodes != 2 || ev.ActualTimeMs != 150 || ev.RowsRemoved != 15 {
		t.Fatalf("evidence = %+v", ev)
	}
}

func misestimateGroup(ord int, queryID int64, relation string, ratio float64) insights.PlanGroup {
	g := groupOf(ord, queryID, 1, seqScan(testSchema, relation, 100, 1, 0))
	g.Findings = []explain.Finding{{
		Code:     explain.RuleRowMisestimate,
		Severity: health.SeverityHigh,
		Path:     nil,
		NodeType: "Seq Scan",
		Relation: relation,
		Params: map[string]any{
			explain.ParamRatio:      ratio,
			explain.ParamPlanRows:   10.0,
			explain.ParamActualRows: 10.0 * ratio,
		},
	}}

	return g
}

func TestAttachEvidenceStaleStatistics(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep, insights.PlanWindow{
		Groups: []insights.PlanGroup{
			misestimateGroup(0, 11, "orders", 12),
			misestimateGroup(1, 11, "orders", 140),
		},
		Partial: false,
	}, evidenceFrom, evidenceTo)

	w := warningOf(rep.Candidates[0], WarnStaleStatistics)
	if w.Code == "" {
		t.Fatal("stale_statistics not raised")
	}

	// The worst misestimate is the one worth quoting.
	if w.Params[ParamRatio] != 140 || w.Params[ParamActualRows] != 1400 {
		t.Errorf("params = %+v", w.Params)
	}
}

// A misestimate somewhere else in the plan says nothing about the statistics of
// the table this index would sit on.
func TestAttachEvidenceStaleStatisticsOtherTable(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep, insights.PlanWindow{
		Groups:  []insights.PlanGroup{misestimateGroup(0, 11, "customers", 200)},
		Partial: false,
	}, evidenceFrom, evidenceTo)

	if hasWarning(rep.Candidates[0], WarnStaleStatistics) {
		t.Error("stale_statistics raised on another table's misestimate")
	}
}

func TestAttachEvidenceIgnoresPlansWithoutQueryID(t *testing.T) {
	g := groupOf(0, 11, 3, seqScan(testSchema, "orders", 100, 1, 0))
	g.HasQueryID = false

	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep, insights.PlanWindow{Groups: []insights.PlanGroup{g}, Partial: false},
		evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceNotFound {
		t.Fatalf("state = %q, want not_found", got)
	}
}

// A window the scan stopped short of holds no argument against the index: the
// plans that would have carried one may be the ones it never read.
func TestAttachEvidencePartialWithoutAMatchIsNotSearched(t *testing.T) {
	rep := reportOf(candidateOn("orders", 11))
	AttachEvidence(&rep, insights.PlanWindow{
		Groups:  []insights.PlanGroup{groupOf(0, 11, 3, seqScan(testSchema, "customers", 120, 1, 0))},
		Partial: true,
	}, evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceNotSearched {
		t.Fatalf("state = %q, want not_searched", got)
	}
}

// Two schemas holding a table of the same name, and a plan logged without
// VERBOSE naming neither: the scan belongs to one of the candidates, and
// crediting both would invent evidence for the other.
func TestAttachEvidenceSchemalessNodeWithTheNameInTwoSchemas(t *testing.T) {
	other := candidateOn("orders", 11)
	other.Schema = "archive"

	rep := reportOf(candidateOn("orders", 11), other)
	AttachEvidence(&rep, insights.PlanWindow{
		Groups:  []insights.PlanGroup{groupOf(0, 11, 3, seqScan("", "orders", 120, 1, 0))},
		Partial: false,
	}, evidenceFrom, evidenceTo)

	for _, c := range rep.Candidates {
		if c.Evidence.State != EvidenceNotSearched {
			t.Errorf("%s.%s: state = %q, want not_searched", c.Schema, c.Table, c.Evidence.State)
		}
	}
}

// The same two candidates, and a plan that does name the schema: it is evidence
// for that one alone.
func TestAttachEvidenceSchemaQualifiedNodeWithTheNameInTwoSchemas(t *testing.T) {
	other := candidateOn("orders", 11)
	other.Schema = "archive"

	rep := reportOf(candidateOn("orders", 11), other)
	AttachEvidence(&rep, insights.PlanWindow{
		Groups:  []insights.PlanGroup{groupOf(0, 11, 3, seqScan("archive", "orders", 120, 1, 0))},
		Partial: false,
	}, evidenceFrom, evidenceTo)

	if got := rep.Candidates[0].Evidence.State; got != EvidenceNotFound {
		t.Errorf("public.orders: state = %q, want not_found", got)
	}

	if got := rep.Candidates[1].Evidence.State; got != EvidenceFound {
		t.Errorf("archive.orders: state = %q, want found", got)
	}
}
