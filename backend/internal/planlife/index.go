package planlife

import (
	"sort"
	"strings"
)

// maxChainNodes: a chain is a DAG unrolled into a tree and grows fast on large joins.
const maxChainNodes = 2000

const hypoLabelPrefix = "hypo:"

func (s *state) indexChains() []IndexChain {
	byName := map[string]*IndexChain{}

	for _, id := range s.sortedIDs() {
		pv := s.paths[id]

		name, ok := s.indexName(pv)
		if !ok || s.childHasIndex(pv, name) {
			continue
		}

		ch, ok := byName[name]
		if !ok {
			ch = &IndexChain{Index: name, OID: pv.IndexOID, Hypothetical: pv.IndexHypo} //nolint:exhaustruct
			byName[name] = ch
		}

		budget := maxChainNodes
		ch.Leaves = append(ch.Leaves, s.chainUp(id, &budget, map[int]bool{}))

		if budget <= 0 {
			ch.Truncated = true
		}
	}

	out := make([]IndexChain, 0, len(byName))

	for _, ch := range byName {
		ch.Outcome = s.indexOutcome(ch)
		out = append(out, *ch)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })

	return out
}

// indexName: a hypothetical index is named only by its hypo: label.
func (s *state) indexName(pv *PathView) (string, bool) {
	if pv.IndexHypo {
		if pv.Label != nil && strings.HasPrefix(*pv.Label, hypoLabelPrefix) {
			return strings.TrimPrefix(*pv.Label, hypoLabelPrefix), true
		}

		return "", false
	}

	if pv.IndexName != nil && *pv.IndexName != "" {
		return *pv.IndexName, true
	}

	return "", false
}

// childHasIndex: a chain starts below a BitmapHeapScan, at its index path.
func (s *state) childHasIndex(pv *PathView, name string) bool {
	for _, c := range pv.Children() {
		if child, ok := s.paths[c]; ok {
			if n, ok := s.indexName(child); ok && n == name {
				return true
			}
		}
	}

	return false
}

func (s *state) chainUp(id int, budget *int, onPath map[int]bool) ChainNode {
	node := ChainNode{PathID: id} //nolint:exhaustruct
	*budget--

	onPath[id] = true
	defer delete(onPath, id)

	for _, p := range s.paths[id].Parents {
		if *budget <= 0 {
			break
		}

		if onPath[p] {
			continue // a cycle means a broken trace
		}

		node.Up = append(node.Up, s.chainUp(p, budget, onPath))
	}

	return node
}

func (s *state) indexOutcome(ch *IndexChain) IndexOutcome {
	allRejected := len(ch.Leaves) > 0

	for _, leaf := range ch.Leaves {
		f := s.paths[leaf.PathID].Fate
		if f == FateChosen {
			return IndexInPlan
		}

		if f != FateRejected {
			allRejected = false
		}
	}

	if allRejected {
		return IndexOnlyRejected
	}

	return IndexLost
}

func (s *state) sortedIDs() []int {
	ids := make([]int, 0, len(s.paths))
	for id := range s.paths {
		ids = append(ids, id)
	}

	sort.Ints(ids)

	return ids
}
