package planlife

import (
	"sort"
	"strings"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/pathprobe"
)

type state struct {
	tr     pathprobe.Trace
	paths  map[int]*PathView
	events map[int][]pathprobe.Event // by path id, in seq order
	a      *Analysis
}

func Analyze(tr pathprobe.Trace) Analysis { return analyzeTrace(tr, nil) }

// AnalyzeWithPlan also matches the final path to an EXPLAIN of the same query (design §8);
// of several final candidates it takes the one that matches the plan.
func AnalyzeWithPlan(tr pathprobe.Trace, plan *explain.Plan) Analysis { return analyzeTrace(tr, plan) }

func analyzeTrace(tr pathprobe.Trace, plan *explain.Plan) Analysis {
	a := Analysis{ //nolint:exhaustruct
		Query:    tr.Query,
		Rels:     tr.Rels,
		Coverage: tr.Coverage,
		Limits:   tr.Limits,
		Timeline: tr.Events,
		Plan:     plan,
	}

	s := &state{tr: tr, paths: map[int]*PathView{}, events: map[int][]pathprobe.Event{}, a: &a}

	s.collectPaths()
	a.Final = s.finalPath()

	if plan != nil {
		s.finalByPlan(plan)
	}

	s.fates()

	if plan != nil {
		a.Match = s.matchPlan(plan)
	}

	a.Relations = s.relations()
	a.Indexes = s.indexChains()

	ids := make([]int, 0, len(s.paths))
	for id := range s.paths {
		ids = append(ids, id)
	}

	sort.Ints(ids)

	for _, id := range ids {
		a.Paths = append(a.Paths, *s.paths[id])
	}

	a.Caveats = s.caveats()

	return a
}

// collectPaths also covers paths that only events mention (an incomplete paths[]).
func (s *state) collectPaths() {
	for id, n := range s.tr.Paths {
		s.paths[id] = &PathView{PathNode: n} //nolint:exhaustruct
	}

	for _, ev := range s.tr.Events {
		if ev.PathID == 0 {
			continue
		}

		s.events[ev.PathID] = append(s.events[ev.PathID], ev)

		pv, ok := s.paths[ev.PathID]
		if !ok {
			pv = &PathView{PathNode: pathprobe.PathNode{ID: ev.PathID, PathType: ev.PathType, Rel: ev.Rel}} //nolint:exhaustruct
			s.paths[ev.PathID] = pv
		}

		pv.Events = append(pv.Events, ev.Seq)

		if pv.CostTotal == nil && ev.CostTotal != nil {
			pv.CostTotal = ev.CostTotal
			pv.CostStartup = ev.CostStartup
		}

		if ev.Label != nil && pv.Label == nil {
			pv.Label = ev.Label
		}

		if ev.KeptReason != nil {
			pv.KeptReason = ev.KeptReason
		}
	}

	for id, pv := range s.paths {
		for _, child := range pv.Children() {
			if c, ok := s.paths[child]; ok {
				c.Parents = append(c.Parents, id)
			}
		}
	}

	for _, pv := range s.paths {
		sort.Ints(pv.Parents)
	}
}

// has: rel "" matches any relation.
func (s *state) has(id int, k pathprobe.Kind, rel pathprobe.RelKey) bool {
	_, ok := s.first(id, k, rel)

	return ok
}

func (s *state) first(id int, k pathprobe.Kind, rel pathprobe.RelKey) (pathprobe.Event, bool) {
	for _, ev := range s.events[id] {
		if ev.Kind == k && (rel == "" || ev.Rel == rel) {
			return ev, true
		}
	}

	return pathprobe.Event{}, false //nolint:exhaustruct
}

func (s *state) relations() []Relation {
	var (
		out   []Relation
		index = map[pathprobe.RelKey]int{}
		seen  = map[pathprobe.RelKey]map[int]bool{}
		group = map[pathprobe.RelKey]map[[2]string]int{}
		costs = map[pathprobe.RelKey]map[int]bool{} // groups with a cost seen
	)

	for _, ev := range s.tr.Events {
		i, ok := index[ev.Rel]
		if !ok {
			i = len(out)
			index[ev.Rel] = i
			seen[ev.Rel] = map[int]bool{}
			group[ev.Rel] = map[[2]string]int{}
			costs[ev.Rel] = map[int]bool{}
			out = append(out, Relation{Key: ev.Rel, Label: s.relLabel(ev.Rel), Counts: map[Fate]int{}}) //nolint:exhaustruct
		}

		r := &out[i]

		if ev.Kind == pathprobe.KindSkipped {
			reason := ""
			if ev.SkipReason != nil {
				reason = *ev.SkipReason
			}

			key := [2]string{ev.PathType, reason}

			g, ok := group[ev.Rel][key]
			if !ok {
				g = len(r.Precheck)
				group[ev.Rel][key] = g
				r.Precheck = append(r.Precheck, PrecheckGroup{Method: ev.PathType, Reason: reason}) //nolint:exhaustruct
			}

			pg := &r.Precheck[g]
			pg.Count++

			if ev.CostTotal != nil {
				c := *ev.CostTotal
				if !costs[ev.Rel][g] {
					pg.MinCost, pg.MaxCost = c, c
					costs[ev.Rel][g] = true
				}

				pg.MinCost = min(pg.MinCost, c)
				pg.MaxCost = max(pg.MaxCost, c)
			}

			continue
		}

		if ev.PathID == 0 || seen[ev.Rel][ev.PathID] {
			continue
		}

		seen[ev.Rel][ev.PathID] = true
		r.PathIDs = append(r.PathIDs, ev.PathID)
		r.Counts[s.paths[ev.PathID].Fate]++
	}

	// paths without events in their own rel: Memoize, Material, a chosen path put in place without add_path()
	for _, id := range s.sortedIDs() {
		pv := s.paths[id]
		if pv.Rel == "" || seen[pv.Rel][id] {
			continue
		}

		i, ok := index[pv.Rel]
		if !ok {
			i = len(out)
			index[pv.Rel] = i
			seen[pv.Rel] = map[int]bool{}
			out = append(out, Relation{Key: pv.Rel, Label: s.relLabel(pv.Rel), Counts: map[Fate]int{}}) //nolint:exhaustruct
		}

		seen[pv.Rel][id] = true
		out[i].PathIDs = append(out[i].PathIDs, id)
		out[i].Counts[pv.Fate]++
	}

	return out
}

func (s *state) relLabel(rel pathprobe.RelKey) string {
	if rel.IsUpper() {
		return "upper"
	}

	members := rel.Members()
	names := make([]string, 0, len(members))

	for _, m := range members {
		alias, ok := s.tr.Rels[m]
		if !ok || alias == "" {
			return string(rel) // no alias for one of them: keep the numbers
		}

		names = append(names, alias)
	}

	return strings.Join(names, " ⋈ ")
}
