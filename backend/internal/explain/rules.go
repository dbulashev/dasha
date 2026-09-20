package explain

import (
	"math"
	"strings"

	"github.com/dbulashev/dasha/internal/health"
)

// Rule codes. They are stable strings: the locale files, the MCP knowledge base
// and the API all key on them, so renaming one changes the contract.
const (
	RuleSeqScanLarge       = "seq_scan_large"
	RuleCostHotspot        = "cost_hotspot"
	RuleNestedLoopBlowup   = "nested_loop_blowup"
	RuleIndexCandidateJoin = "index_candidate_join"
	RuleSortEstimateSpill  = "sort_estimate_spill"
	RuleCTEMaterialize     = "cte_materialize"
	RuleRowMisestimate     = "row_misestimate"
	RuleSortSpillActual    = "sort_spill_actual"
	RuleFilterDiscardsRows = "filter_discards_rows"
	RuleHeapFetchesHigh    = "heap_fetches_high"
	RuleLoopsBlowup        = "loops_blowup"
	RuleBitmapLossy        = "bitmap_lossy"
	RuleWorkersNotLaunched = "workers_not_launched"
	RuleJITOverhead        = "jit_overhead"
	RuleTriggerTime        = "trigger_time"
)

// Finding parameter keys, passed to i18n as-is.
const (
	ParamTableRows       = "table_rows"
	ParamPlanRows        = "plan_rows"
	ParamActualRows      = "actual_rows"
	ParamScannedRows     = "scanned_rows"
	ParamRatio           = "ratio"
	ParamSelfCost        = "self_cost"
	ParamTotalCost       = "total_cost"
	ParamCostShare       = "cost_share"
	ParamOuterRows       = "outer_rows"
	ParamInnerRows       = "inner_rows"
	ParamPairs           = "pairs"
	ParamEstimatedKB     = "estimated_kb"
	ParamWorkMemKB       = "work_mem_kb"
	ParamRowsRemoved     = "rows_removed"
	ParamRemovedShare    = "removed_share"
	ParamHeapFetches     = "heap_fetches"
	ParamSortMethod      = "sort_method"
	ParamSortSpaceKB     = "sort_space_kb"
	ParamLoops           = "loops"
	ParamExpectedLoops   = "expected_loops"
	ParamLossyBlocks     = "lossy_blocks"
	ParamExactBlocks     = "exact_blocks"
	ParamWorkersPlanned  = "workers_planned"
	ParamWorkersLaunched = "workers_launched"
	ParamTimeMs          = "time_ms"
	ParamTotalTimeMs     = "total_time_ms"
	ParamTimeShare       = "time_share"
	ParamEstimateOnly    = "estimate_only"
)

// Thresholds. Findings quote the value they tripped on, so the numbers reach
// the user from here rather than from a copy in the prose.
const (
	seqScanMinTableRows    = 100_000.0
	seqScanMinCost         = 1_000.0
	hotspotMinShare        = 0.5
	hotspotMinCost         = 100.0
	nestedLoopMinOuterRows = 1_000.0
	nestedLoopMinPairs     = 1_000_000.0
	materializeMinRows     = 100_000.0
	misestimateFactor      = 10.0
	misestimateMinRows     = 100.0
	discardMinShare        = 0.9
	discardMinRows         = 10_000.0
	heapFetchMinShare      = 0.2
	heapFetchMinRows       = 1_000.0
	loopsFactor            = 10.0
	loopsMin               = 1_000.0
	lossyMinShare          = 0.1
	overheadMinShare       = 0.25
	overheadMinMs          = 10.0
)

var registry = []Rule{
	{Code: RuleSeqScanLarge, Severity: health.SeverityMedium, Eval: evalSeqScanLarge},
	{Code: RuleCostHotspot, Severity: health.SeverityLow, Eval: evalCostHotspot},
	{Code: RuleNestedLoopBlowup, Severity: health.SeverityMedium, Eval: evalNestedLoopBlowup},
	{Code: RuleIndexCandidateJoin, Severity: health.SeverityHigh, Eval: evalIndexCandidateJoin},
	{Code: RuleSortEstimateSpill, Severity: health.SeverityMedium, Requires: ReqWorkMem, Eval: evalSortEstimateSpill},
	{Code: RuleCTEMaterialize, Severity: health.SeverityLow, Eval: evalCTEMaterialize},
	{Code: RuleRowMisestimate, Severity: health.SeverityHigh, Requires: ReqActual, Eval: evalRowMisestimate},
	{Code: RuleSortSpillActual, Severity: health.SeverityMedium, Requires: ReqActual, Eval: evalSortSpillActual},
	{Code: RuleFilterDiscardsRows, Severity: health.SeverityMedium, Requires: ReqActual, Eval: evalFilterDiscardsRows},
	{Code: RuleHeapFetchesHigh, Severity: health.SeverityMedium, Requires: ReqActual, Eval: evalHeapFetchesHigh},
	{Code: RuleLoopsBlowup, Severity: health.SeverityHigh, Requires: ReqActual, Eval: evalLoopsBlowup},
	{Code: RuleBitmapLossy, Severity: health.SeverityLow, Requires: ReqActual, Eval: evalBitmapLossy},
	{Code: RuleWorkersNotLaunched, Severity: health.SeverityLow, Requires: ReqActual, Eval: evalWorkersNotLaunched},
	{Code: RuleJITOverhead, Severity: health.SeverityLow, Requires: ReqTiming, Eval: evalJITOverhead},
	{Code: RuleTriggerTime, Severity: health.SeverityLow, Requires: ReqTiming, Eval: evalTriggerTime},
}

func evalSeqScanLarge(p *Plan, ctx Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Type != "Seq Scan" || n.Filter == "" {
			return
		}

		params := map[string]any{ParamPlanRows: n.PlanRows, ParamTotalCost: n.TotalCost}
		severity := health.SeverityMedium

		switch rows, known := ctx.tableRows(n); {
		case known:
			if float64(rows) < seqScanMinTableRows {
				return
			}

			params[ParamTableRows] = rows
		case scannedRows(n) > 0:
			scanned := scannedRows(n)
			if scanned < seqScanMinTableRows {
				return
			}

			params[ParamScannedRows] = scanned
		default:
			if n.TotalCost < seqScanMinCost {
				return
			}

			severity = health.SeverityLow
			params[ParamEstimateOnly] = true
		}

		out = append(out, finding(RuleSeqScanLarge, severity, path, n, params))
	})

	return out
}

// scannedRows is what the scan actually read: the rows it returned plus the
// ones the filter threw away, across all loops.
func scannedRows(n *Node) float64 {
	if n.Actual == nil {
		return 0
	}

	return n.Actual.TotalRows() + rowsRemoved(n)*n.Actual.Loops
}

// evalCostHotspot works on estimates only; a plan with actual numbers skips it.
func evalCostHotspot(p *Plan, _ Context) []Finding {
	total := p.Root.TotalCost
	if total <= 0 || p.Caps.Actual {
		return nil
	}

	var (
		best     *Node
		bestPath []int
		bestCost float64
	)

	eachNode(p, func(path []int, n, _ *Node) {
		if self := n.SelfCost(); self > bestCost {
			best, bestPath, bestCost = n, path, self
		}
	})

	if best == nil || bestCost < hotspotMinCost || bestCost/total < hotspotMinShare {
		return nil
	}

	return []Finding{finding(RuleCostHotspot, health.SeverityLow, bestPath, best, map[string]any{
		ParamSelfCost:  bestCost,
		ParamTotalCost: total,
		ParamCostShare: bestCost / total,
	})}
}

func evalNestedLoopBlowup(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Type != "Nested Loop" {
			return
		}

		outerIdx, innerIdx, ok := joinSides(n)
		if !ok {
			return
		}

		outer, inner := n.Children[outerIdx].PlanRows, n.Children[innerIdx].PlanRows
		pairs := outer * inner

		if outer < nestedLoopMinOuterRows || pairs < nestedLoopMinPairs {
			return
		}

		out = append(out, finding(RuleNestedLoopBlowup, health.SeverityMedium, path, n, map[string]any{
			ParamOuterRows: outer,
			ParamInnerRows: inner,
			ParamPairs:     pairs,
		}))
	})

	return out
}

func evalIndexCandidateJoin(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Type != "Nested Loop" {
			return
		}

		outerIdx, innerIdx, ok := joinSides(n)
		if !ok {
			return
		}

		inner, innerPath := unwrapBuffered(&n.Children[innerIdx], append(clonePath(path), innerIdx))
		if inner.Type != "Seq Scan" {
			return
		}

		// A cross join has nothing an index could serve.
		if n.HashCond == "" && n.MergeCond == "" && n.JoinFilter == "" && inner.Filter == "" {
			return
		}

		out = append(out, finding(RuleIndexCandidateJoin, health.SeverityHigh, innerPath, inner, map[string]any{
			ParamOuterRows: n.Children[outerIdx].PlanRows,
			ParamInnerRows: inner.PlanRows,
		}))
	})

	return out
}

// joinSides locates the two sides of a join. An InitPlan or a SubPlan shares
// the child slice without holding a side, and PostgreSQL prints it first.
func joinSides(n *Node) (outer, inner int, ok bool) {
	outer, inner = -1, -1

	for i := range n.Children {
		switch {
		case n.Children[i].ParentRel == RelOuter && outer < 0:
			outer = i
		case n.Children[i].ParentRel == RelInner && inner < 0:
			inner = i
		}
	}

	return outer, inner, outer >= 0 && inner >= 0
}

// unwrapBuffered steps through the nodes that only hold another node's output,
// where the scan underneath is what a new index would serve.
func unwrapBuffered(n *Node, path []int) (*Node, []int) {
	for (n.Type == "Materialize" || n.Type == "Memoize") && len(n.Children) == 1 {
		n = &n.Children[0]
		path = append(path, 0)
	}

	return n, path
}

func evalSortEstimateSpill(p *Plan, ctx Context) []Finding {
	if ctx.WorkMemKB == nil {
		return nil
	}

	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		switch n.Type {
		case "Sort", "Incremental Sort", "Hash":
		default:
			return
		}

		estimatedKB := n.PlanRows * float64(n.PlanWidth) / 1024
		if estimatedKB <= float64(*ctx.WorkMemKB) {
			return
		}

		out = append(out, finding(RuleSortEstimateSpill, health.SeverityMedium, path, n, map[string]any{
			ParamEstimatedKB: estimatedKB,
			ParamWorkMemKB:   *ctx.WorkMemKB,
			ParamPlanRows:    n.PlanRows,
		}))
	})

	return out
}

func evalCTEMaterialize(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Type != "CTE Scan" && n.Type != "Materialize" {
			return
		}

		if n.PlanRows < materializeMinRows {
			return
		}

		out = append(out, finding(RuleCTEMaterialize, health.SeverityLow, path, n, map[string]any{
			ParamPlanRows:  n.PlanRows,
			ParamTotalCost: n.TotalCost,
		}))
	})

	return out
}

// evalRowMisestimate compares per-loop numbers: both the estimate and the
// measured row count are what one pass of the node produced.
func evalRowMisestimate(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Actual == nil || n.Actual.Loops == 0 {
			return
		}

		estimated, actual := math.Max(n.PlanRows, 1), math.Max(n.Actual.Rows, 1)
		if math.Max(estimated, actual) < misestimateMinRows {
			return
		}

		ratio := math.Max(estimated/actual, actual/estimated)
		if ratio < misestimateFactor {
			return
		}

		out = append(out, finding(RuleRowMisestimate, health.SeverityHigh, path, n, map[string]any{
			ParamPlanRows:   n.PlanRows,
			ParamActualRows: n.Actual.Rows,
			ParamLoops:      n.Actual.Loops,
			ParamRatio:      ratio,
		}))
	})

	return out
}

func evalSortSpillActual(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if !strings.Contains(n.SortMethod, "external") {
			return
		}

		params := map[string]any{ParamSortMethod: n.SortMethod}
		if n.SortSpaceKB != nil {
			params[ParamSortSpaceKB] = *n.SortSpaceKB
		}

		out = append(out, finding(RuleSortSpillActual, health.SeverityMedium, path, n, params))
	})

	return out
}

// rowsRemoved is what the node threw away per loop: a join counts the rows its
// join condition rejected apart from the ones its own qual did.
func rowsRemoved(n *Node) float64 {
	removed := 0.0

	for _, v := range []*float64{n.RowsRemovedByFilter, n.RowsRemovedByJoinFilter} {
		if v != nil {
			removed += *v
		}
	}

	return removed
}

func evalFilterDiscardsRows(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Actual == nil || (n.RowsRemovedByFilter == nil && n.RowsRemovedByJoinFilter == nil) {
			return
		}

		removed := rowsRemoved(n) * n.Actual.Loops
		if removed < discardMinRows {
			return
		}

		share := removed / (removed + n.Actual.TotalRows())
		if share < discardMinShare {
			return
		}

		out = append(out, finding(RuleFilterDiscardsRows, health.SeverityMedium, path, n, map[string]any{
			ParamRowsRemoved:  removed,
			ParamActualRows:   n.Actual.TotalRows(),
			ParamRemovedShare: share,
		}))
	})

	return out
}

func evalHeapFetchesHigh(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Type != "Index Only Scan" || n.HeapFetches == nil || n.Actual == nil {
			return
		}

		fetches := *n.HeapFetches
		rows := n.Actual.TotalRows()

		if fetches < heapFetchMinRows || rows <= 0 || fetches/rows < heapFetchMinShare {
			return
		}

		out = append(out, finding(RuleHeapFetchesHigh, health.SeverityMedium, path, n, map[string]any{
			ParamHeapFetches: fetches,
			ParamActualRows:  rows,
			ParamRatio:       fetches / rows,
		}))
	})

	return out
}

func evalLoopsBlowup(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, parent *Node) {
		if n.Actual == nil || parent == nil || parent.Type != "Nested Loop" {
			return
		}

		if n.ParentRel != RelInner {
			return
		}

		outerIdx, _, ok := joinSides(parent)
		if !ok {
			return
		}

		// Loops count every execution of the inner side, so the outer estimate
		// holds per execution of the join itself.
		expected := math.Max(parent.Children[outerIdx].PlanRows, 1)
		if parent.Actual != nil && parent.Actual.Loops > 0 {
			expected *= parent.Actual.Loops
		}

		loops := n.Actual.Loops

		if loops < loopsMin || loops < expected*loopsFactor {
			return
		}

		out = append(out, finding(RuleLoopsBlowup, health.SeverityHigh, path, n, map[string]any{
			ParamLoops:         loops,
			ParamExpectedLoops: expected,
			ParamRatio:         loops / expected,
		}))
	})

	return out
}

func evalBitmapLossy(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.Type != "Bitmap Heap Scan" || n.HeapBlocksLossy == nil {
			return
		}

		lossy := *n.HeapBlocksLossy
		exact := 0.0

		if n.HeapBlocksExact != nil {
			exact = *n.HeapBlocksExact
		}

		if lossy <= 0 || lossy/(lossy+exact) < lossyMinShare {
			return
		}

		out = append(out, finding(RuleBitmapLossy, health.SeverityLow, path, n, map[string]any{
			ParamLossyBlocks: lossy,
			ParamExactBlocks: exact,
		}))
	})

	return out
}

func evalWorkersNotLaunched(p *Plan, _ Context) []Finding {
	var out []Finding

	eachNode(p, func(path []int, n, _ *Node) {
		if n.WorkersPlanned == nil || n.WorkersLaunched == nil || *n.WorkersLaunched >= *n.WorkersPlanned {
			return
		}

		out = append(out, finding(RuleWorkersNotLaunched, health.SeverityLow, path, n, map[string]any{
			ParamWorkersPlanned:  *n.WorkersPlanned,
			ParamWorkersLaunched: *n.WorkersLaunched,
		}))
	})

	return out
}

func evalJITOverhead(p *Plan, _ Context) []Finding {
	if p.JIT == nil || p.JIT.Total == nil {
		return nil
	}

	total := totalTimeMs(p)
	jit := *p.JIT.Total

	if total <= 0 || jit < overheadMinMs || jit/total < overheadMinShare {
		return nil
	}

	return []Finding{finding(RuleJITOverhead, health.SeverityLow, nil, &p.Root, map[string]any{
		ParamTimeMs:      jit,
		ParamTotalTimeMs: total,
		ParamTimeShare:   jit / total,
	})}
}

func evalTriggerTime(p *Plan, _ Context) []Finding {
	if len(p.Triggers) == 0 {
		return nil
	}

	spent := 0.0
	for _, t := range p.Triggers {
		spent += t.Time
	}

	total := totalTimeMs(p)
	if total <= 0 || spent < overheadMinMs || spent/total < overheadMinShare {
		return nil
	}

	return []Finding{finding(RuleTriggerTime, health.SeverityLow, nil, &p.Root, map[string]any{
		ParamTimeMs:      spent,
		ParamTotalTimeMs: total,
		ParamTimeShare:   spent / total,
	})}
}

// totalTimeMs is the plan's wall time: EXPLAIN reports it, auto_explain puts it
// in the log record instead, and a plan with neither falls back to the root node.
func totalTimeMs(p *Plan) float64 {
	switch {
	case p.ExecutionTime != nil:
		return *p.ExecutionTime
	case p.Duration != nil:
		return *p.Duration
	case p.Root.Actual != nil:
		return p.Root.Actual.TotalTime
	default:
		return 0
	}
}

func finding(code string, severity health.Severity, path []int, n *Node, params map[string]any) Finding {
	return Finding{
		Code:     code,
		Severity: severity,
		Path:     clonePath(path),
		NodeType: n.Type,
		Relation: n.Relation,
		Params:   params,
	}
}

func eachNode(p *Plan, fn func(path []int, n, parent *Node)) {
	var visit func(n, parent *Node, path []int)

	visit = func(n, parent *Node, path []int) {
		fn(path, n, parent)

		for i := range n.Children {
			visit(&n.Children[i], n, append(clonePath(path), i))
		}
	}

	visit(&p.Root, nil, nil)
}

// clonePath copies a path so callers can keep it after the walk moves on.
func clonePath(path []int) []int {
	if path == nil {
		return nil
	}

	return append([]int{}, path...)
}
