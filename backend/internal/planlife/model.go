// Package planlife draws conclusions from a pathprobe trace; where the trace
// cannot back one up, it returns a Caveat instead.
package planlife

import (
	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/pathprobe"
)

type Fate string

const (
	FateChosen    Fate = "chosen" // in the tree of the final path
	FateSurvived  Fate = "survived"
	FateDisplaced Fate = "displaced"
	FateRemoved   Fate = "removed" // pushed out by a path that add_path() then rejected
	FateRejected  Fate = "rejected"
	FateChildOnly Fate = "child_only" // no events of its own, only a child of other paths
	FateUnknown   Fate = "unknown"    // see PathView.FateReason
)

const (
	ReasonDisplacedNotCovered = "displaced_not_covered"
	ReasonRemovedNotCovered   = "removed_not_covered"
	ReasonNoEvents            = "no_events"
)

// CauseCode is read off the trace's costs; pathprobe does not record which
// add_path() comparison decided.
type CauseCode string

const (
	CauseCostlierTotal CauseCode = "costlier_total"
	CauseLostOnStartup CauseCode = "lost_on_startup"
	// CauseLostNotOnCost: sort order, parameterization, rows or parallel safety; the trace does not say which.
	CauseLostNotOnCost CauseCode = "lost_not_on_cost"
	CauseUnknown       CauseCode = "unknown" // no winner, or no costs to compare
)

type Cause struct {
	Code          CauseCode
	Gap           *float64 // total minus the winner's; > 0 lost on total
	Startup       *float64
	WinnerStartup *float64
}

type PathView struct {
	pathprobe.PathNode

	Fate       Fate
	FateReason string

	By    int // displacer, dominator or remover; 0 when the trace does not say
	BySeq int // 0 when the winner's event was not recorded
	Cause *Cause

	CostStartup *float64
	CostTotal   *float64
	Label       *string
	KeptReason  *string

	Events  []int // seq
	Parents []int

	// PlanNode: child indexes from the plan root to the matched node; nil when not matched.
	PlanNode []int
	// RowsRatio: estimate vs actual rows of the matched node, set when it crosses the row_misestimate thresholds.
	RowsRatio *float64
}

type FinalStatus string

const (
	FinalExact     FinalStatus = "exact"
	FinalAmbiguous FinalStatus = "ambiguous" // the cheapest of several candidates
	FinalByPlan    FinalStatus = "by_plan"   // the one of several candidates that matches the plan
	FinalNone      FinalStatus = "none"
)

type FinalPath struct {
	Status     FinalStatus
	PathID     int
	Candidates []int
	Tree       []int // the final path and all its descendants
}

// PrecheckGroup: join paths of one method dropped by add_path_precheck() before they existed.
type PrecheckGroup struct {
	Method  string
	Reason  string
	Count   int
	MinCost float64
	MaxCost float64
}

type Relation struct {
	Key      pathprobe.RelKey
	Label    string // "c ⋈ o", "upper"
	PathIDs  []int  // in order of first appearance
	Precheck []PrecheckGroup
	Counts   map[Fate]int
}

type IndexOutcome string

const (
	IndexInPlan       IndexOutcome = "in_plan"
	IndexOnlyRejected IndexOutcome = "only_rejected"
	IndexLost         IndexOutcome = "lost"
)

// ChainNode: a path and the paths built on top of it.
type ChainNode struct {
	PathID int
	Up     []ChainNode
}

type IndexChain struct {
	Index        string
	OID          uint32 // 0 for a hypothetical index
	Hypothetical bool
	Outcome      IndexOutcome
	Leaves       []ChainNode
	Truncated    bool // stopped at maxChainNodes
	// Best: the cheapest path over the index at the highest level such paths reached; 0 when in the plan.
	Best int
	// BestAtFinalLevel: Best is in the relation of the final path, so their costs compare.
	BestAtFinalLevel bool
}

type CaveatScope string

const (
	ScopeTrace    CaveatScope = "trace"
	ScopeRelation CaveatScope = "relation"
	ScopePath     CaveatScope = "path"
)

const (
	CaveatCoverageMissing = "coverage_missing" // Params["point"]
	CaveatCoverageAbsent  = "coverage_absent"
	CaveatEventsTruncated = "events_truncated"
	// CaveatDominatorNotReported: rejected events without a dominator, a server with an older core patch.
	CaveatDominatorNotReported = "dominator_not_reported"
	CaveatNoPathGraph          = "no_path_graph"
	CaveatUnknownValues        = "unknown_values"  // Params["values"]
	CaveatFinalAmbiguous       = "final_ambiguous" // Params["candidates"]
	CaveatFinalNone            = "final_none"
	// CaveatEdgesAbsent: a path type that always has an input, with no edge to it (GroupingSets as Agg).
	CaveatEdgesAbsent      = "edges_absent"
	CaveatPrecheckNoInputs = "precheck_no_inputs"
	CaveatIndexNotExamined = "index_not_examined_unobservable"
	// CaveatSubqueryRels: paths below a SubqueryScan number their rels in the subquery's range table.
	CaveatSubqueryRels = "subquery_rels"
	// CaveatPartialPaths: the plan has Gather / Gather Merge; pathprobe does not see partial paths.
	CaveatPartialPaths  = "partial_paths_unobserved"
	CaveatChosenButLost = "chosen_but_displaced" // Params["seq"], ["kind"]
	// CaveatChosenAsBitmapQual: a displaced index path that is the bitmapqual of a
	// chosen BitmapHeapScan; add_path() does not free index paths. Informational.
	CaveatChosenAsBitmapQual = "chosen_as_bitmapqual"
)

type Caveat struct {
	Code   string
	Scope  CaveatScope
	Rel    pathprobe.RelKey // ScopeRelation
	PathID int              // ScopePath
	Params map[string]string
}

type MatchStatus string

const (
	MatchOK        MatchStatus = "match"
	MatchWithNotes MatchStatus = "match_with_notes" // see Match.Notes and Match.Unmatched
	MatchMismatch  MatchStatus = "mismatch"
	MatchNoFinal   MatchStatus = "no_final_path"
)

type QueryTextCheck string

const (
	QueryTextSame    QueryTextCheck = "same"
	QueryTextDiffers QueryTextCheck = "differs"
	QueryTextMissing QueryTextCheck = "missing" // the plan carries no query text
)

const (
	NoteProjectionOmitted   = "projection_omitted"    // a Projection path without a Result node
	NoteSubqueryScanRemoved = "subquery_scan_removed" // a trivial SubqueryScan removed by set_plan_references
	NoteAppendRemoved       = "append_removed"        // a single-child Append removed from the plan
	NoteAppendChildren      = "append_children_differ"
)

const (
	UnmatchedUnknownShape = "unknown_shape"
	UnmatchedNoEdges      = "no_edges"
	UnmatchedSubplan      = "subplan" // InitPlan / SubPlan: planned apart, not in the path tree
	UnmatchedPartial      = "partial_paths_unobserved"
	UnmatchedAppendChild  = "append_child"
)

type MatchNote struct {
	Code     string
	PathID   int
	PlanNode []int
}

// MatchDiff is the first place where the path tree and the plan disagree.
type MatchDiff struct {
	PlanNode []int
	PathID   int
	Expected string // EXPLAIN node type, with the relation or index when those differ
	Found    string
}

type Unmatched struct {
	PlanNode []int
	Reason   string
}

type Match struct {
	Status    MatchStatus
	QueryText QueryTextCheck
	Notes     []MatchNote
	Mismatch  *MatchDiff
	Unmatched []Unmatched
}

type Analysis struct {
	Query    string
	Rels     map[int]string
	Coverage pathprobe.Coverage
	Limits   pathprobe.Limits

	Final     FinalPath
	Paths     []PathView // by path id
	Relations []Relation
	Indexes   []IndexChain
	Timeline  []pathprobe.Event
	Match     *Match        // nil without a plan
	Plan      *explain.Plan // nil without a plan
	Caveats   []Caveat
}

func (a *Analysis) Path(id int) (*PathView, bool) {
	for i := range a.Paths {
		if a.Paths[i].ID == id {
			return &a.Paths[i], true
		}
	}

	return nil, false
}
