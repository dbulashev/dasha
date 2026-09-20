package explain

import (
	"testing"

	"github.com/dbulashev/dasha/internal/health"
)

func TestRegistry_Complete(t *testing.T) {
	seen := make(map[string]bool, len(registry))

	for _, r := range registry {
		if seen[r.Code] {
			t.Errorf("duplicate rule code: %s", r.Code)
		}

		seen[r.Code] = true

		if r.Eval == nil || r.Severity == "" {
			t.Errorf("%s: a rule needs a severity and an evaluator", r.Code)
		}
	}

	if _, ok := LookupRule(RuleSeqScanLarge); !ok {
		t.Error("lookup misses a registered rule")
	}

	if _, ok := LookupRule("nope"); ok {
		t.Error("lookup invented a rule")
	}
}

func TestEvaluate_DormantWithoutFacts(t *testing.T) {
	p := parseFixture(t, "pg17/analyze_off.txt", SourceLog)

	findings, dormant := Evaluate(&p, Context{})

	for _, f := range findings {
		if rule, _ := LookupRule(f.Code); rule.Requires != 0 {
			t.Errorf("%s fired on a plan that proves nothing", f.Code)
		}
	}

	missing := map[string][]string{}
	for _, d := range dormant {
		missing[d.Code] = d.Missing
	}

	for _, code := range []string{RuleRowMisestimate, RuleSortSpillActual, RuleLoopsBlowup, RuleBitmapLossy} {
		if got := missing[code]; len(got) != 1 || got[0] != MissingActual {
			t.Errorf("%s: missing %v, want [%s]", code, got, MissingActual)
		}
	}

	if got := missing[RuleSortEstimateSpill]; len(got) != 1 || got[0] != MissingWorkMem {
		t.Errorf("%s: missing %v, want [%s]", RuleSortEstimateSpill, got, MissingWorkMem)
	}

	if got := missing[RuleJITOverhead]; len(got) != 1 || got[0] != MissingTiming {
		t.Errorf("%s: missing %v, want [%s]", RuleJITOverhead, got, MissingTiming)
	}
}

func TestSeqScanLarge_SeverityFollowsWhatIsKnown(t *testing.T) {
	p := parseFixture(t, "synthetic/generic_plan.json", SourceExplain)

	findings, _ := Evaluate(&p, Context{})

	f, ok := codes(findings)[RuleSeqScanLarge]
	if !ok {
		t.Fatal("no finding on a filtered sequential scan")
	}

	if f.Severity != health.SeverityLow || f.Params[ParamEstimateOnly] != true {
		t.Errorf("without table sizes the finding rests on the estimate alone: %+v", f)
	}

	withSizes, _ := Evaluate(&p, Context{TableRows: map[string]int64{"public.orders": 4_000_000}})

	f, ok = codes(withSizes)[RuleSeqScanLarge]
	if !ok {
		t.Fatal("no finding with table sizes")
	}

	if f.Severity != health.SeverityMedium || f.Params[ParamTableRows] != int64(4_000_000) {
		t.Errorf("with table sizes: %+v", f)
	}

	small, _ := Evaluate(&p, Context{TableRows: map[string]int64{"public.orders": 900}})
	if _, fired := codes(small)[RuleSeqScanLarge]; fired {
		t.Error("a scan of a small table is not a finding")
	}

	bare, _ := Evaluate(&p, Context{TableRows: map[string]int64{"orders": 4_000_000}})

	f, ok = codes(bare)[RuleSeqScanLarge]
	if !ok || f.Severity != health.SeverityMedium {
		t.Errorf("a plan carrying a schema still matches the unqualified alias: %+v", f)
	}
}

func TestRules_NestedLoop(t *testing.T) {
	p := parseFixture(t, "synthetic/nested_loop.txt", SourceLog)

	findings, _ := Evaluate(&p, Context{})
	got := codes(findings)

	blowup, ok := got[RuleNestedLoopBlowup]
	if !ok {
		t.Fatal("nested loop over a large outer side is a finding")
	}

	if blowup.Params[ParamPairs] != 100000.0*50000.0 {
		t.Errorf("pairs: %v", blowup.Params[ParamPairs])
	}

	candidate, ok := got[RuleIndexCandidateJoin]
	if !ok {
		t.Fatal("a sequential scan on the inner side is an index candidate")
	}

	if candidate.Relation != "b" || candidate.NodeType != "Seq Scan" {
		t.Errorf("the finding points past Materialize to the scan: %+v", candidate)
	}

	discards, ok := got[RuleFilterDiscardsRows]
	if !ok {
		t.Fatal("a join filter dropping 99% of the pairs is a finding")
	}

	if share, _ := discards.Params[ParamRemovedShare].(float64); share < 0.9 {
		t.Errorf("removed share: %v", share)
	}
}

func TestRules_InitPlanKeepsJoinSides(t *testing.T) {
	p := parseFixture(t, "synthetic/initplan_join.txt", SourceLog)

	findings, _ := Evaluate(&p, Context{})
	got := codes(findings)

	blowup, ok := got[RuleNestedLoopBlowup]
	if !ok {
		t.Fatal("an InitPlan on the join holds neither side of it")
	}

	if blowup.Params[ParamPairs] != 100000.0*50000.0 {
		t.Errorf("pairs: %v", blowup.Params[ParamPairs])
	}

	candidate, ok := got[RuleIndexCandidateJoin]
	if !ok {
		t.Fatal("a sequential scan on the inner side is an index candidate")
	}

	if candidate.Relation != "b" {
		t.Errorf("the candidate is the inner scan, not the outer one: %+v", candidate)
	}

	if _, ok := got[RuleLoopsBlowup]; ok {
		t.Error("loops that match the outer estimate are not a blow-up")
	}
}

func TestRules_LoopsBlowup(t *testing.T) {
	p := parseFixture(t, "synthetic/loops_blowup.txt", SourceLog)

	findings, _ := Evaluate(&p, Context{})
	got := codes(findings)

	loops, ok := got[RuleLoopsBlowup]
	if !ok {
		t.Fatal("5000 loops against an estimate of 12 is a finding")
	}

	if loops.Severity != health.SeverityHigh || loops.Params[ParamLoops] != 5000.0 {
		t.Errorf("loops finding: %+v", loops)
	}

	if _, ok := got[RuleRowMisestimate]; !ok {
		t.Error("the estimate that caused it is a finding of its own")
	}

	if _, ok := got[RuleNestedLoopBlowup]; ok {
		t.Error("a small outer side is not a blow-up")
	}

	if _, ok := got[RuleSeqScanLarge]; ok {
		t.Error("5000 scanned rows are not a large scan")
	}
}

func TestRules_ActualSignals(t *testing.T) {
	cases := []struct {
		fixture string
		code    string
	}{
		{fixture: "synthetic/index_only_scan.txt", code: RuleHeapFetchesHigh},
		{fixture: "synthetic/sort_external.txt", code: RuleSortSpillActual},
		{fixture: "synthetic/bitmap_lossy.txt", code: RuleBitmapLossy},
		{fixture: "pg17/nested.txt", code: RuleWorkersNotLaunched},
		{fixture: "synthetic/triggers_jit.txt", code: RuleTriggerTime},
		{fixture: "synthetic/trigger_no_relname.txt", code: RuleTriggerTime},
		{fixture: "synthetic/triggers_jit.txt", code: RuleJITOverhead},
	}

	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			p := parseFixture(t, c.fixture, SourceLog)

			findings, _ := Evaluate(&p, Context{})
			if _, ok := codes(findings)[c.code]; !ok {
				t.Errorf("%s did not fire on %s", c.code, c.fixture)
			}
		})
	}
}

// A sort cut short by LIMIT reports "still in progress", which is not a spill.
func TestSortSpillActual_SkipsInterruptedSort(t *testing.T) {
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			p := parseFixture(t, version+"/sort_spill.txt", SourceLog)

			findings, _ := Evaluate(&p, Context{})
			if _, ok := codes(findings)[RuleSortSpillActual]; ok {
				t.Error("an interrupted sort is not a spill")
			}
		})
	}
}

func TestSortEstimateSpill_NeedsWorkMem(t *testing.T) {
	p := parseFixture(t, "synthetic/sort_external.txt", SourceLog)

	workMem := int64(4096)

	findings, dormant := Evaluate(&p, Context{WorkMemKB: &workMem})

	for _, d := range dormant {
		if d.Code == RuleSortEstimateSpill {
			t.Fatal("work_mem was given, the rule must run")
		}
	}

	f, ok := codes(findings)[RuleSortEstimateSpill]
	if !ok {
		t.Fatal("300k rows of 37 bytes do not fit in 4 MB")
	}

	if f.Params[ParamWorkMemKB] != workMem {
		t.Errorf("the finding quotes the threshold it tripped on: %+v", f.Params)
	}
}

func TestSortEstimateSpill_FallsBackToPlanSettings(t *testing.T) {
	p := parseFixture(t, "synthetic/sort_settings.txt", SourceLog)

	findings, dormant := Evaluate(&p, Context{})

	for _, d := range dormant {
		if d.Code == RuleSortEstimateSpill {
			t.Fatal("the plan printed work_mem, so the rule runs without a connection")
		}
	}

	f, ok := codes(findings)[RuleSortEstimateSpill]
	if !ok {
		t.Fatal("300k rows of 37 bytes do not fit in 4 MB")
	}

	if f.Params[ParamWorkMemKB] != int64(4096) {
		t.Errorf("the finding quotes the setting the plan printed: %+v", f.Params)
	}

	// The caller knows the instance default; the plan printed the value its own
	// session ran with, and that is the threshold the sort actually met.
	instance := int64(1 << 20)

	findings, _ = Evaluate(&p, Context{WorkMemKB: &instance})

	f, ok = codes(findings)[RuleSortEstimateSpill]
	if !ok {
		t.Fatal("the plan's own work_mem outranks the instance default")
	}

	if f.Params[ParamWorkMemKB] != int64(4096) {
		t.Errorf("the finding quotes the setting the plan printed: %+v", f.Params)
	}
}

func TestParseMemKB(t *testing.T) {
	sizes := map[string]int64{"8kB": 8, "4MB": 4096, "1GB": 1 << 20, "2TB": 2 << 30, "4096": 4096}

	for in, want := range sizes {
		got, ok := ParseMemKB(in)
		if !ok || got != want {
			t.Errorf("%s: %d (%v), want %d", in, got, ok, want)
		}
	}

	for _, in := range []string{"", "on", "-1", "abc"} {
		if _, ok := ParseMemKB(in); ok {
			t.Errorf("%q is not a size", in)
		}
	}
}

func TestCostHotspot_OnlyWithoutActuals(t *testing.T) {
	estimated := parseFixture(t, "pg17/analyze_off.txt", SourceLog)

	findings, _ := Evaluate(&estimated, Context{})
	if _, ok := codes(findings)[RuleCostHotspot]; !ok {
		t.Error("a plan without actual numbers names its costliest node")
	}

	analyzed := parseFixture(t, "pg17/text_verbose_off.txt", SourceLog)

	findings, _ = Evaluate(&analyzed, Context{})
	if f, ok := codes(findings)[RuleCostHotspot]; ok {
		t.Errorf("cost hotspot on a plan with actual numbers: %+v", f)
	}
}

func TestEvaluate_SortsBySeverity(t *testing.T) {
	p := parseFixture(t, "synthetic/nested_loop.txt", SourceLog)

	findings, _ := Evaluate(&p, Context{})
	if len(findings) < 2 {
		t.Fatalf("expected several findings, got %d", len(findings))
	}

	for i := 1; i < len(findings); i++ {
		if severityRank(findings[i-1].Severity) > severityRank(findings[i].Severity) {
			t.Fatalf("findings are out of order at %d: %v", i, findings)
		}
	}
}

func TestEvaluate_PathPointsAtTheNode(t *testing.T) {
	p := parseFixture(t, "synthetic/nested_loop.txt", SourceLog)

	findings, _ := Evaluate(&p, Context{})

	f, ok := codes(findings)[RuleIndexCandidateJoin]
	if !ok {
		t.Fatal("no finding to check")
	}

	node := p.NodeAt(f.Path)
	if node == nil || node.Type != f.NodeType || node.Relation != f.Relation {
		t.Errorf("path %v resolves to %+v", f.Path, node)
	}
}
