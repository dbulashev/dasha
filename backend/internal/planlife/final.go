package planlife

import (
	"sort"

	"github.com/dbulashev/dasha/internal/pathprobe"
)

// finalPath: the roots of the upper relation, or the survivors of the top join
// without upper events; of several, the cheapest (ambiguous). Design §5.
func (s *state) finalPath() FinalPath {
	cands := s.upperRoots()
	if cands == nil {
		cands = s.topSurvivors()
	}

	fp := FinalPath{Status: FinalNone, Candidates: cands} //nolint:exhaustruct

	if len(cands) == 0 {
		return fp
	}

	best := cands[0]
	for _, id := range cands[1:] {
		if s.cost(id) < s.cost(best) {
			best = id
		}
	}

	fp.PathID = best
	fp.Status = FinalExact

	if len(cands) > 1 {
		fp.Status = FinalAmbiguous
	}

	fp.Tree = s.subtree(best)

	return fp
}

func (s *state) upperRoots() []int {
	upper := map[int]bool{}

	for _, ev := range s.tr.Events {
		if ev.Rel.IsUpper() && ev.PathID != 0 {
			upper[ev.PathID] = true
		}
	}

	if len(upper) == 0 {
		return nil
	}

	var out []int

	for id := range upper {
		in := s.has(id, pathprobe.KindAccepted, pathprobe.RelUpper) || s.has(id, pathprobe.KindSurvived, pathprobe.RelUpper)
		out1 := s.lost(id, pathprobe.RelUpper)

		if !in || out1 || s.childOfAny(id, upper) {
			continue
		}

		out = append(out, id)
	}

	sort.Ints(out)

	if out == nil {
		out = []int{}
	}

	return out
}

// lost: the path left the pathlist of rel ("" for any).
func (s *state) lost(id int, rel pathprobe.RelKey) bool {
	for _, k := range lostKinds {
		if s.has(id, k, rel) {
			return true
		}
	}

	return false
}

var lostKinds = []pathprobe.Kind{pathprobe.KindDisplaced, pathprobe.KindRemoved, pathprobe.KindRejected}

func (s *state) childOfAny(id int, set map[int]bool) bool {
	for _, p := range s.paths[id].Parents {
		if set[p] {
			return true
		}
	}

	return false
}

func (s *state) topSurvivors() []int {
	var top pathprobe.RelKey

	for _, ev := range s.tr.Events {
		if ev.Rel.IsUpper() {
			continue
		}

		if top == "" || len(ev.Rel.Members()) > len(top.Members()) {
			top = ev.Rel
		}
	}

	if top == "" {
		return []int{}
	}

	seen := map[int]bool{}
	out := []int{}

	kind := pathprobe.KindSurvived
	if !s.tr.Coverage.Survived {
		kind = pathprobe.KindAccepted
	}

	for _, ev := range s.tr.Events {
		if ev.Rel != top || ev.Kind != kind || ev.PathID == 0 || seen[ev.PathID] {
			continue
		}

		if s.lost(ev.PathID, "") {
			continue
		}

		seen[ev.PathID] = true
		out = append(out, ev.PathID)
	}

	sort.Ints(out)

	return out
}

func (s *state) cost(id int) float64 {
	if pv := s.paths[id]; pv != nil && pv.CostTotal != nil {
		return *pv.CostTotal
	}

	return 0
}

func (s *state) subtree(id int) []int {
	var (
		out  []int
		seen = map[int]bool{}
		walk func(int)
	)

	walk = func(id int) {
		pv, ok := s.paths[id]
		if !ok || seen[id] {
			return
		}

		seen[id] = true
		out = append(out, id)

		for _, c := range pv.Children() {
			walk(c)
		}
	}

	walk(id)

	return out
}
