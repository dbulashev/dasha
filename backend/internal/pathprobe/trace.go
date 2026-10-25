// Package pathprobe reads the JSON trace of pathprobe_json(); format reference:
// docs/technical/json-format.md in the pathprobe repository, commit 9bcf24c.
package pathprobe

import (
	"sort"
	"strconv"
	"strings"
)

type Kind string

const (
	KindAccepted  Kind = "accepted"
	KindRejected  Kind = "rejected"
	KindDisplaced Kind = "displaced"
	KindRemoved   Kind = "removed" // pushed out by a new path that add_path() then rejected
	KindSurvived  Kind = "survived"
	KindSkipped   Kind = "skipped"
	KindExcluded  Kind = "excluded"
)

func (k Kind) known() bool {
	switch k {
	case KindAccepted, KindRejected, KindDisplaced, KindRemoved, KindSurvived, KindSkipped, KindExcluded:
		return true
	default:
		return false
	}
}

// RelKey is "{1,2}" for a base or join relation; all upper relations share RelUpper.
type RelKey string

const RelUpper RelKey = "?"

func (r RelKey) IsUpper() bool { return r == RelUpper }

// Members is nil for an upper relation.
func (r RelKey) Members() []int {
	if r.IsUpper() {
		return nil
	}

	ids, _ := parseRelids(string(r))

	return ids
}

type Event struct {
	Seq      int
	Src      string // pc p1 p0 j2 wp
	Rel      RelKey
	Kind     Kind
	PathType string
	// PathID is 0 when the event has no Path (skipped, excluded).
	PathID int

	CostStartup *float64
	CostTotal   *float64 // nil when pathprobe suppresses costs
	Gap         *float64 // nil when gap_cost is 0: pathprobe omits it then

	Param      bool
	Label      *string // proposed or hypothetical path name, "hypo:<name>"
	KeptReason *string // survived only: "startup", "order?"
	Origin     string  // real, proposed

	SkipReason *string

	// ByID: the displacer, the dominator or the remover, by Kind.
	ByID int
	// BySeq: the winner's accepted event, or the remover's rejected event; 0 when not recorded.
	BySeq  int
	ByType *string

	DisabledNodes int
}

type PathNode struct {
	ID       int
	PathType string
	Rel      RelKey

	SubID    int // also the bitmapqual of a BitmapHeapScan and the subquery path of a SubqueryScan
	OuterID  int
	InnerID  int
	ChildIDs []int // Append, MergeAppend, BitmapAnd, BitmapOr; [left, right] for SetOp, RecursiveUnion

	Rows          *float64 // nil for BitmapAnd / BitmapOr
	RequiredOuter []int
	IndexOID      uint32
	IndexName     *string
	IndexHypo     bool // named by the "hypo:<name>" label of its events

	// InSubquery: below a SubqueryScan, so Rel is in the subquery's numbering.
	InSubquery bool
}

// Children lists outer before inner, then the single input, then the list inputs.
func (n PathNode) Children() []int {
	var out []int

	for _, id := range []int{n.OuterID, n.InnerID, n.SubID} {
		if id > 0 {
			out = append(out, id)
		}
	}

	return append(out, n.ChildIDs...)
}

// Coverage: Known is false when the trace has no coverage object at all.
type Coverage struct {
	Known bool

	Survived  bool
	Accepted  bool
	Rejected  bool
	Displaced bool
	Removed   bool
	Precheck  bool
	Excluded  bool

	IndexBuild bool
}

type Limits struct {
	MaxEvents        int
	EventsTruncated  bool
	SummaryTruncated bool
}

type Trace struct {
	Query    string
	Rels     map[int]string // range table index -> alias
	Events   []Event
	Paths    map[int]PathNode // by path_id
	Coverage Coverage
	Limits   Limits

	Unknown []string // unknown enum values, "kind=..."
}

func parseRelids(s string) ([]int, bool) {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, false
	}

	body := s[1 : len(s)-1]
	if body == "" {
		return nil, true
	}

	parts := strings.Split(body, ",")
	out := make([]int, 0, len(parts))

	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return nil, false
		}

		out = append(out, n)
	}

	sort.Ints(out)

	return out, true
}

func sortStrings(s []string) { sort.Strings(s) }
