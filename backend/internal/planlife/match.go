package planlife

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/dbulashev/dasha/internal/explain"
)

// explainTypes: pathprobe prints the plan node tag of a path; EXPLAIN prints its own name.
var explainTypes = map[string]string{
	"SeqScan":         "Seq Scan",
	"SampleScan":      "Sample Scan",
	"IndexScan":       "Index Scan",
	"IndexOnlyScan":   "Index Only Scan",
	"BitmapHeapScan":  "Bitmap Heap Scan",
	"TidScan":         "Tid Scan",
	"TidRangeScan":    "Tid Range Scan",
	"SubqueryScan":    "Subquery Scan",
	"FunctionScan":    "Function Scan",
	"ValuesScan":      "Values Scan",
	"CteScan":         "CTE Scan",
	"WorkTableScan":   "WorkTable Scan",
	"ForeignScan":     "Foreign Scan",
	"CustomScan":      "Custom Scan",
	"MergeAppend":     "Merge Append",
	"Material":        "Materialize",
	"IncrementalSort": "Incremental Sort",
	"Agg":             "Aggregate",
	"RecursiveUnion":  "Recursive Union",
	"NestLoop":        "Nested Loop",
	"MergeJoin":       "Merge Join",
	"HashJoin":        "Hash Join",
	"GatherMerge":     "Gather Merge",
}

var bitmapParents = map[string]bool{"BitmapHeapScan": true, "BitmapAnd": true, "BitmapOr": true}

type matcher struct {
	s     *state
	m     *Match
	pairs map[int][]int // path id -> plan node
	nodes map[int]*explain.Node
}

// finalByPlan: of several final candidates, the first whose tree matches the plan (design §5).
func (s *state) finalByPlan(plan *explain.Plan) {
	if s.a.Final.Status != FinalAmbiguous {
		return
	}

	for _, id := range s.a.Final.Candidates {
		mt := s.newMatcher()
		if mt.node(id, &plan.Root, []int{}, nil) {
			s.a.Final.PathID = id
			s.a.Final.Status = FinalByPlan
			s.a.Final.Tree = s.subtree(id)

			return
		}
	}
}

func (s *state) matchPlan(plan *explain.Plan) *Match {
	mt := s.newMatcher()
	mt.m.QueryText = queryTextCheck(s.tr.Query, plan.QueryText)

	switch {
	case mt.m.QueryText == QueryTextDiffers:
		mt.m.Status = MatchMismatch

		return mt.m
	case s.a.Final.PathID == 0:
		mt.m.Status = MatchNoFinal

		return mt.m
	}

	if !mt.node(s.a.Final.PathID, &plan.Root, []int{}, nil) {
		mt.m.Status = MatchMismatch

		return mt.m
	}

	mt.m.Status = MatchOK
	if len(mt.m.Notes) > 0 || len(mt.m.Unmatched) > 0 {
		mt.m.Status = MatchWithNotes
	}

	for id, at := range mt.pairs {
		pv := s.paths[id]
		pv.PlanNode = at

		if n := mt.nodes[id]; n.Actual != nil && n.Actual.Loops > 0 {
			if ratio, ok := explain.RowMisestimate(n.PlanRows, n.Actual.Rows); ok {
				pv.RowsRatio = &ratio
			}
		}
	}

	return mt.m
}

func (s *state) newMatcher() *matcher {
	return &matcher{
		s:     s,
		m:     &Match{Status: MatchOK, QueryText: QueryTextMissing}, //nolint:exhaustruct
		pairs: map[int][]int{},
		nodes: map[int]*explain.Node{},
	}
}

// node matches path id to plan node n at plan path at; false on the first disagreement.
func (mt *matcher) node(id int, n *explain.Node, at []int, parent *PathView) bool {
	pv, ok := mt.s.paths[id]
	if !ok {
		return mt.diff(at, id, "path #"+strconv.Itoa(id), n.Type)
	}

	want := expectedType(pv, parent)

	if n.Type != want {
		return mt.skip(pv, n, at, parent, want)
	}

	if exp, ok := mt.sameScan(pv, n); !ok {
		return mt.diff(at, id, want+" "+exp, n.Type+" "+scanText(n))
	}

	mt.pairs[id] = at
	mt.nodes[id] = n

	kids := mt.mainChildren(n, at)

	switch {
	case pv.OuterID > 0 || pv.InnerID > 0:
		return mt.join(pv, n, at, kids)
	case pv.SubID > 0:
		if len(kids) != 1 {
			return mt.diff(at, id, want+" with one input", n.Type+" with "+strconv.Itoa(len(kids)))
		}

		return mt.node(pv.SubID, &n.Children[kids[0]], child(at, kids[0]), pv)
	case len(pv.ChildIDs) > 0:
		return mt.list(pv, n, at, kids)
	}

	reason := UnmatchedUnknownShape
	if singleInput[pv.PathType] {
		reason = UnmatchedNoEdges
	}

	for _, k := range kids {
		mt.unmatched(child(at, k), reason)
	}

	return true
}

// skip applies the §8.2 rules for a path without a node of its own, or a node without a path.
func (mt *matcher) skip(pv *PathView, n *explain.Node, at []int, parent *PathView, want string) bool {
	switch {
	case pv.PathType == "Result" && pv.SubID > 0:
		mt.note(NoteProjectionOmitted, pv.ID, at)

		return mt.node(pv.SubID, n, at, parent)
	case pv.PathType == "SubqueryScan" && pv.SubID > 0:
		mt.note(NoteSubqueryScanRemoved, pv.ID, at)

		return mt.node(pv.SubID, n, at, parent)
	case (pv.PathType == "Append" || pv.PathType == "MergeAppend") && len(pv.ChildIDs) == 1:
		mt.note(NoteAppendRemoved, pv.ID, at)

		return mt.node(pv.ChildIDs[0], n, at, parent)
	case (n.Type == "Gather" || n.Type == "Gather Merge") && len(n.Children) == 1:
		mt.unmatched(at, UnmatchedPartial)

		return mt.node(pv.ID, &n.Children[0], child(at, 0), parent)
	}

	return mt.diff(at, pv.ID, want, n.Type)
}

func (mt *matcher) join(pv *PathView, n *explain.Node, at []int, kids []int) bool {
	if len(kids) != 2 || pv.OuterID == 0 || pv.InnerID == 0 {
		return mt.diff(at, pv.ID, expectedType(pv, nil)+" with two inputs", n.Type+" with "+strconv.Itoa(len(kids)))
	}

	if !mt.node(pv.OuterID, &n.Children[kids[0]], child(at, kids[0]), pv) {
		return false
	}

	inner, innerAt := &n.Children[kids[1]], child(at, kids[1])

	// the inner path of a Hash Join sits under a Hash node that has no path
	if pv.PathType == "HashJoin" && inner.Type == "Hash" && len(inner.Children) == 1 {
		inner, innerAt = &inner.Children[0], child(innerAt, 0)
	}

	return mt.node(pv.InnerID, inner, innerAt, pv)
}

func (mt *matcher) list(pv *PathView, n *explain.Node, at []int, kids []int) bool {
	if len(kids) == len(pv.ChildIDs) {
		for i, k := range kids {
			if !mt.node(pv.ChildIDs[i], &n.Children[k], child(at, k), pv) {
				return false
			}
		}

		return true
	}

	// run-time pruning drops Append children; which ones is not in the plan
	if (pv.PathType == "Append" || pv.PathType == "MergeAppend") && len(kids) < len(pv.ChildIDs) {
		mt.note(NoteAppendChildren, pv.ID, at)

		for _, k := range kids {
			mt.unmatched(child(at, k), UnmatchedAppendChild)
		}

		return true
	}

	return mt.diff(at, pv.ID, expectedType(pv, nil)+" with "+strconv.Itoa(len(pv.ChildIDs))+" inputs", n.Type+" with "+strconv.Itoa(len(kids)))
}

// mainChildren: the node's inputs; InitPlan and SubPlan children are planned apart.
func (mt *matcher) mainChildren(n *explain.Node, at []int) []int {
	var out []int

	for i := range n.Children {
		switch n.Children[i].ParentRel {
		case explain.RelInitPlan, explain.RelSubPlan:
			mt.unmatched(child(at, i), UnmatchedSubplan)
		default:
			out = append(out, i)
		}
	}

	return out
}

// sameScan compares the relation alias and the index; rels below a SubqueryScan have no alias.
func (mt *matcher) sameScan(pv *PathView, n *explain.Node) (string, bool) {
	if pv.IndexName != nil && n.IndexName != "" && *pv.IndexName != n.IndexName {
		return "using " + *pv.IndexName, false
	}

	members := pv.Rel.Members()
	if len(members) != 1 || pv.InSubquery {
		return "", true
	}

	alias, nodeAlias := mt.s.tr.Rels[members[0]], n.Alias
	if nodeAlias == "" {
		nodeAlias = n.Relation
	}

	if alias == "" || nodeAlias == "" || alias == nodeAlias {
		return "", true
	}

	return "on " + alias, false
}

func scanText(n *explain.Node) string {
	switch {
	case n.IndexName != "" && n.Alias != "":
		return "using " + n.IndexName + " on " + n.Alias
	case n.IndexName != "":
		return "using " + n.IndexName
	case n.Alias != "":
		return "on " + n.Alias
	default:
		return "on " + n.Relation
	}
}

func expectedType(pv *PathView, parent *PathView) string {
	if pv.PathType == "IndexScan" && parent != nil && bitmapParents[parent.PathType] {
		return "Bitmap Index Scan"
	}

	if t, ok := explainTypes[pv.PathType]; ok {
		return t
	}

	return pv.PathType
}

func (mt *matcher) diff(at []int, id int, expected, found string) bool {
	mt.m.Mismatch = &MatchDiff{PlanNode: at, PathID: id, Expected: strings.TrimSpace(expected), Found: strings.TrimSpace(found)}

	return false
}

func (mt *matcher) note(code string, id int, at []int) {
	mt.m.Notes = append(mt.m.Notes, MatchNote{Code: code, PathID: id, PlanNode: at})
}

func (mt *matcher) unmatched(at []int, reason string) {
	mt.m.Unmatched = append(mt.m.Unmatched, Unmatched{PlanNode: at, Reason: reason})
}

func child(at []int, i int) []int {
	return append(append(make([]int, 0, len(at)+1), at...), i)
}

var spaces = regexp.MustCompile(`\s+`)

// queryTextCheck compares the texts up to whitespace, case and a trailing semicolon.
func queryTextCheck(trace, plan string) QueryTextCheck {
	if strings.TrimSpace(plan) == "" {
		return QueryTextMissing
	}

	norm := func(q string) string {
		q = strings.ToLower(spaces.ReplaceAllString(strings.TrimSpace(q), " "))

		return strings.TrimSpace(strings.TrimRight(q, "; "))
	}

	if norm(trace) == norm(plan) {
		return QueryTextSame
	}

	return QueryTextDiffers
}
