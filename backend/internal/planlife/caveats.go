package planlife

import (
	"strconv"
	"strings"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/pathprobe"
)

var coveragePoints = []struct {
	name string
	get  func(pathprobe.Coverage) bool
}{
	{"add_path_accepted", func(c pathprobe.Coverage) bool { return c.Accepted }},
	{"add_path_displaced", func(c pathprobe.Coverage) bool { return c.Displaced }},
	{"add_path_removed", func(c pathprobe.Coverage) bool { return c.Removed }},
	{"add_path_rejected", func(c pathprobe.Coverage) bool { return c.Rejected }},
	{"add_path_precheck", func(c pathprobe.Coverage) bool { return c.Precheck }},
	{"survived_snapshot", func(c pathprobe.Coverage) bool { return c.Survived }},
	{"join_excluded", func(c pathprobe.Coverage) bool { return c.Excluded }},
	{"index_build", func(c pathprobe.Coverage) bool { return c.IndexBuild }},
}

// singleInput: path types that always have one input; without subpath_id pathprobe did not record
// it (GroupingSets prints as Agg).
var singleInput = map[string]bool{
	"Agg": true, "Group": true, "WindowAgg": true, "Sort": true, "IncrementalSort": true, "Material": true,
	"Memoize": true, "Unique": true, "Limit": true, "LockRows": true, "Gather": true, "GatherMerge": true,
	"ProjectSet": true, "ModifyTable": true, "SubqueryScan": true, "BitmapHeapScan": true,
}

func (s *state) caveats() []Caveat {
	var out []Caveat

	trace := func(code string, params map[string]string) {
		out = append(out, Caveat{Code: code, Scope: ScopeTrace, Params: params}) //nolint:exhaustruct
	}

	if len(s.tr.Unknown) > 0 {
		trace(CaveatUnknownValues, map[string]string{"values": strings.Join(s.tr.Unknown, ",")})
	}

	if s.tr.Limits.EventsTruncated {
		trace(CaveatEventsTruncated, map[string]string{"max_events": strconv.Itoa(s.tr.Limits.MaxEvents)})
	}

	if !s.tr.Coverage.Known {
		trace(CaveatCoverageAbsent, nil)
	} else {
		for _, p := range coveragePoints {
			// index_build is observed only for hypothetical indexes
			if p.name == "index_build" && !s.hasHypoIndex() {
				continue
			}

			if !p.get(s.tr.Coverage) {
				trace(CaveatCoverageMissing, map[string]string{"point": p.name})
			}
		}
	}

	if len(s.tr.Paths) == 0 && len(s.tr.Events) > 0 {
		trace(CaveatNoPathGraph, nil)
	}

	for _, ev := range s.tr.Events {
		if ev.Kind == pathprobe.KindRejected && ev.ByID == 0 {
			trace(CaveatDominatorNotReported, nil)

			break
		}
	}

	for _, p := range s.tr.Paths {
		if p.InSubquery {
			trace(CaveatSubqueryRels, nil)

			break
		}
	}

	switch s.a.Final.Status {
	case FinalAmbiguous:
		ids := make([]string, len(s.a.Final.Candidates))
		for i, id := range s.a.Final.Candidates {
			ids[i] = strconv.Itoa(id)
		}

		trace(CaveatFinalAmbiguous, map[string]string{"candidates": strings.Join(ids, ",")})
	case FinalNone:
		trace(CaveatFinalNone, nil)
	case FinalExact, FinalByPlan:
	}

	if len(s.a.Indexes) > 0 {
		trace(CaveatIndexNotExamined, nil)
	}

	if s.a.Match != nil && s.a.Match.QueryText == QueryTextMissing {
		trace(CaveatPlanQueryText, nil)
	}

	if s.a.Plan != nil && hasGather(s.a.Plan) {
		trace(CaveatPartialPaths, nil)
	}

	for _, r := range s.a.Relations {
		if len(r.Precheck) > 0 {
			out = append(out, Caveat{Code: CaveatPrecheckNoInputs, Scope: ScopeRelation, Rel: r.Key}) //nolint:exhaustruct
		}
	}

	for _, id := range s.sortedIDs() {
		pv := s.paths[id]

		if singleInput[pv.PathType] && pv.SubID == 0 && len(pv.Events) > 0 {
			out = append(out, Caveat{Code: CaveatEdgesAbsent, Scope: ScopePath, PathID: id}) //nolint:exhaustruct
		}

		// add_path() frees the paths it pushes out except index paths; anything else here is unexplained
		if pv.Fate == FateChosen {
			for _, k := range lostKinds {
				ev, ok := s.first(id, k, "")
				if !ok {
					continue
				}

				code := CaveatChosenButLost
				if s.chosenBitmapQual(id) {
					code = CaveatChosenAsBitmapQual
				}

				out = append(out, Caveat{ //nolint:exhaustruct
					Code: code, Scope: ScopePath, PathID: id,
					Params: map[string]string{"seq": strconv.Itoa(ev.Seq), "kind": string(k)},
				})
			}
		}
	}

	return out
}

// chosenBitmapQual: id is the bitmapqual of a chosen BitmapHeapScan, directly or under BitmapAnd / BitmapOr.
func (s *state) chosenBitmapQual(id int) bool {
	for _, p := range s.paths[id].Parents {
		parent := s.paths[p]
		if parent.Fate != FateChosen {
			continue
		}

		switch parent.PathType {
		case "BitmapHeapScan":
			if parent.SubID == id {
				return true
			}
		case "BitmapAnd", "BitmapOr":
			if s.chosenBitmapQual(p) {
				return true
			}
		}
	}

	return false
}

func (s *state) hasHypoIndex() bool {
	for _, pv := range s.paths {
		if pv.IndexHypo {
			return true
		}
	}

	return false
}

func hasGather(p *explain.Plan) bool {
	found := false

	p.Walk(func(_ []int, n *explain.Node) bool {
		if n.Type == "Gather" || n.Type == "Gather Merge" {
			found = true
		}

		return !found
	})

	return found
}
