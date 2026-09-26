package explain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"slices"
	"strconv"
)

// Hash fingerprints the shape of a plan: node types with the modifiers that
// change how the node works, the relations and indexes it touches, and the
// tree structure. Costs, row estimates, measured numbers and condition texts
// are left out — "(k = 42)" and "(k = 43)" are the same plan, and normalising
// literals here would be a second masker drifting apart from the one the log
// pipeline already applies.
func Hash(n Node) string {
	h := sha256.New()
	hashNode(h, &n)

	return hex.EncodeToString(h.Sum(nil)[:8])
}

func hashNode(h hash.Hash, n *Node) {
	write(h, n.Type)
	write(h, n.Strategy)
	write(h, n.PartialMode)
	write(h, n.JoinType)
	write(h, n.Operation)
	write(h, n.ScanDirection)
	write(h, n.Schema)
	write(h, n.Relation)
	write(h, n.IndexName)
	write(h, n.ParentRel)
	write(h, strconv.FormatBool(n.Parallel))
	write(h, strconv.Itoa(len(n.Children)))

	// Parallel Append orders its children by cost, so near-equal partitions swap
	// places between runs of one plan.
	if n.Type == "Append" && n.Parallel {
		subs := make([]string, len(n.Children))
		for i := range n.Children {
			subs[i] = Hash(n.Children[i])
		}

		slices.Sort(subs)

		for _, sub := range subs {
			write(h, sub)
		}

		return
	}

	for i := range n.Children {
		hashNode(h, &n.Children[i])
	}
}

// write is length-prefixed so a separator inside a value cannot shift a boundary.
func write(h hash.Hash, s string) {
	_, _ = fmt.Fprintf(h, "%d:%s", len(s), s)
}
