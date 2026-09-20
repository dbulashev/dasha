// Package explain holds the plan model shared by the EXPLAIN track (plan built
// on a live connection) and the auto_explain track (plan taken from a log
// record): the tree, the parsers, the shape fingerprint and the rule catalog.
// It reaches nothing outside itself — no pgx, no log provider.
package explain

// Source says where the plan came from.
type Source string

const (
	SourceExplain Source = "explain"
	SourceLog     Source = "log"
)

// Format is the plan text the parser was handed.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Parent relationship values, as PostgreSQL prints them.
const (
	RelOuter    = "Outer"
	RelInner    = "Inner"
	RelMember   = "Member"
	RelSubquery = "Subquery"
	RelInitPlan = "InitPlan"
	RelSubPlan  = "SubPlan"
)

// Capabilities is what a plan can actually back up, decided while parsing and
// never from the source or the server configuration: a log plan with
// log_analyze = off and a GENERIC_PLAN from EXPLAIN are identical here, and the
// rules have to behave the same on both.
type Capabilities struct {
	Actual   bool
	Buffers  bool
	Timing   bool
	Settings bool
}

// Plan is one parsed plan.
type Plan struct {
	Source        Source
	Format        Format
	Generic       bool // GENERIC_PLAN: parameters are not substituted
	QueryText     string
	QueryParams   string // absent before pg16
	QueryID       int64
	HasQueryID    bool
	Duration      *float64 // ms, known from a log record only
	PlanningTime  *float64
	ExecutionTime *float64
	Root          Node
	Triggers      []Trigger
	JIT           *JIT
	Settings      map[string]string
	Caps          Capabilities
}

// Node is one plan node.
//
// Type is the canonical name JSON prints ("Aggregate", "Seq Scan"); the
// modifiers the text format folds into that name (Parallel, Finalize, HashAggregate,
// Hash Left Join, Index Scan Backward, Insert on) are kept apart so both formats
// yield the same tree.
type Node struct {
	Type          string
	Relation      string
	Schema        string
	Alias         string // empty when it repeats the relation name
	IndexName     string
	ParentRel     string
	SubplanName   string
	Parallel      bool
	PartialMode   string // Partial / Finalize
	Strategy      string // Aggregate: Plain / Sorted / Hashed / Mixed
	JoinType      string // Inner / Left / Right / Full / Semi / Anti
	ScanDirection string // Forward / Backward / NoMovement
	Operation     string // ModifyTable: Insert / Update / Delete / Merge

	StartupCost float64
	TotalCost   float64
	PlanRows    float64
	PlanWidth   int

	Actual *Actual // nil when the plan was built without ANALYZE

	Filter      string
	IndexCond   string
	RecheckCond string
	// A merge join prints both its merge condition and its own join filter, so
	// one field for all three would drop one of them.
	HashCond   string
	MergeCond  string
	JoinFilter string
	SortKey    []string
	// A join node counts the two apart: the join condition and the node's own
	// qual each throw rows away.
	RowsRemovedByFilter     *float64
	RowsRemovedByJoinFilter *float64
	HeapFetches             *float64
	SortMethod              string
	SortSpaceKB             *float64
	SortSpaceType           string
	WorkersPlanned          *int
	WorkersLaunched         *int
	HeapBlocksExact         *float64
	HeapBlocksLossy         *float64
	Buffers                 *Buffers

	Children []Node
}

// Actual holds the measured numbers of a node. Rows and the times are per
// single loop, the way PostgreSQL prints them: the node's whole output is
// Rows × Loops, and the multiplication is left to the caller.
type Actual struct {
	StartupTime float64
	TotalTime   float64
	Rows        float64
	Loops       float64
}

// TotalRows is the node's output across all loops.
func (a *Actual) TotalRows() float64 {
	if a == nil {
		return 0
	}

	return a.Rows * a.Loops
}

type Buffers struct {
	SharedHit     float64
	SharedRead    float64
	SharedDirtied float64
	SharedWritten float64
	LocalHit      float64
	LocalRead     float64
	LocalDirtied  float64
	LocalWritten  float64
	TempRead      float64
	TempWritten   float64
}

type Trigger struct {
	Name     string
	Relation string
	Time     float64
	Calls    float64
}

type JIT struct {
	Functions    int
	Generation   *float64
	Inlining     *float64
	Optimization *float64
	Emission     *float64
	Total        *float64
}

// Walk visits every node depth-first, passing the path from the root as the
// child indexes taken to reach it. Returning false prunes the subtree.
func (p *Plan) Walk(fn func(path []int, n *Node) bool) {
	walk(&p.Root, nil, fn)
}

func walk(n *Node, path []int, fn func([]int, *Node) bool) {
	if !fn(path, n) {
		return
	}

	for i := range n.Children {
		walk(&n.Children[i], append(append([]int{}, path...), i), fn)
	}
}

// NodeAt resolves a path produced by Walk.
func (p *Plan) NodeAt(path []int) *Node {
	n := &p.Root
	for _, i := range path {
		if i < 0 || i >= len(n.Children) {
			return nil
		}

		n = &n.Children[i]
	}

	return n
}

// NodeCount counts the nodes in the tree.
func (p *Plan) NodeCount() int {
	count := 0
	p.Walk(func(_ []int, _ *Node) bool {
		count++

		return true
	})

	return count
}

// SelfCost is the node's total cost minus what its children already cost.
func (n *Node) SelfCost() float64 {
	self := n.TotalCost
	for i := range n.Children {
		self -= n.Children[i].TotalCost
	}

	if self < 0 {
		return 0
	}

	return self
}

func ptrFloat(v float64) *float64 { return &v }

func ptrInt(v int) *int { return &v }
