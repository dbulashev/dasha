package mcpserver

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dbulashev/dasha/gen/apiclient"
)

// planTextOpts bounds one rendered tree: the length of a condition and the
// children shown under one node.
type planTextOpts struct {
	condBytes int
	childCap  int
}

var impliedModifiers = map[string]bool{"Inner": true, "Forward": true, "Plain": true, "Simple": true}

// renderPlan prints a tree one node per line, EXPLAIN-like, with the node's
// conditions and counters on the lines below it and its findings marked on it.
func renderPlan(root apiclient.PlanNode, findings []apiclient.PlanFinding, o planTextOpts) string {
	marks := map[string][]string{}
	onPath := map[string]bool{}

	for _, f := range findings {
		key := pathKey(f.Path)
		marks[key] = append(marks[key], string(f.Code))

		for i := range f.Path {
			onPath[pathKey(f.Path[:i+1])] = true
		}
	}

	var b strings.Builder

	renderNode(&b, root, nil, 0, marks, onPath, o)

	return strings.TrimSuffix(b.String(), "\n")
}

func pathKey(path []int) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(p)
	}

	return strings.Join(parts, ".")
}

func renderNode(
	b *strings.Builder, n apiclient.PlanNode, path []int, depth int,
	marks map[string][]string, onPath map[string]bool, o planTextOpts,
) {
	indent := strings.Repeat("  ", depth)
	arrow := ""

	if depth > 0 {
		arrow = "-> "
	}

	b.WriteString(indent + arrow + nodeHeader(n))

	if m := marks[pathKey(path)]; len(m) > 0 {
		b.WriteString("  !! " + strings.Join(m, ", "))
	}

	b.WriteByte('\n')

	detail := indent + strings.Repeat(" ", len(arrow)+2)
	for _, line := range nodeDetails(n, o.condBytes) {
		b.WriteString(detail + line + "\n")
	}

	shown := 0

	for i, c := range n.Children {
		childPath := append(slices.Clone(path), i)
		if shown >= o.childCap && !onPath[pathKey(childPath)] {
			continue
		}

		shown++

		renderNode(b, c, childPath, depth+1, marks, onPath, o)
	}

	if folded := len(n.Children) - shown; folded > 0 {
		fmt.Fprintf(b, "%s  -> … %d more children folded, none with a finding\n", indent, folded)
	}
}

func nodeHeader(n apiclient.PlanNode) string {
	var parts []string

	if n.SubplanName != nil {
		parts = append(parts, *n.SubplanName+":")
	}

	parts = append(parts, nodeLabel(n))

	if n.Relation != nil {
		rel := *n.Relation
		if n.Schema != nil {
			rel = *n.Schema + "." + rel
		}

		if n.Alias != nil && *n.Alias != *n.Relation {
			rel += " " + *n.Alias
		}

		parts = append(parts, "on "+rel)
	}

	if n.IndexName != nil {
		parts = append(parts, "using "+*n.IndexName)
	}

	parts = append(parts, fmt.Sprintf("(cost=%.2f..%.2f rows=%s width=%d)",
		n.StartupCost, n.TotalCost, fmtNum(n.PlanRows), n.PlanWidth))

	if a := n.Actual; a != nil {
		switch {
		case a.Loops == 0:
			parts = append(parts, "(never executed)")
		case a.TotalTimeMs != nil:
			parts = append(parts, fmt.Sprintf("(actual time=%.3f..%.3f rows=%s loops=%s)",
				deref(a.StartupTimeMs), *a.TotalTimeMs, fmtNum(a.Rows), fmtNum(a.Loops)))
		default:
			parts = append(parts, fmt.Sprintf("(actual rows=%s loops=%s)", fmtNum(a.Rows), fmtNum(a.Loops)))
		}
	}

	return strings.Join(parts, " ")
}

func nodeLabel(n apiclient.PlanNode) string {
	name := n.Type
	if n.Parallel {
		name = "Parallel " + name
	}

	if n.PartialMode != nil && *n.PartialMode != "" && *n.PartialMode != "Simple" {
		name = *n.PartialMode + " " + name
	}

	var mods []string

	for _, m := range []*string{n.Strategy, n.JoinType, n.ScanDirection, n.Operation} {
		if m != nil && *m != "" && !impliedModifiers[*m] {
			mods = append(mods, *m)
		}
	}

	if len(mods) == 0 {
		return name
	}

	return name + " [" + strings.Join(mods, ", ") + "]"
}

func nodeDetails(n apiclient.PlanNode, condBytes int) []string {
	var out []string

	for _, c := range []struct {
		label   string
		v       *string
		omitted *int
	}{
		{"Index Cond", n.IndexCond, n.IndexCondOmittedBytes},
		{"Recheck Cond", n.RecheckCond, n.RecheckCondOmittedBytes},
		{"Hash Cond", n.HashCond, n.HashCondOmittedBytes},
		{"Merge Cond", n.MergeCond, n.MergeCondOmittedBytes},
		{"Join Filter", n.JoinFilter, n.JoinFilterOmittedBytes},
		{"Filter", n.Filter, n.FilterOmittedBytes},
	} {
		if c.v != nil {
			out = append(out, c.label+": "+clipOmitted(*c.v, condBytes, deref(c.omitted)))
		}
	}

	if n.RowsRemovedByFilter != nil {
		out = append(out, "Rows Removed by Filter: "+fmtNum(*n.RowsRemovedByFilter))
	}

	if n.RowsRemovedByJoinFilter != nil {
		out = append(out, "Rows Removed by Join Filter: "+fmtNum(*n.RowsRemovedByJoinFilter))
	}

	if n.SortKey != nil && len(*n.SortKey) > 0 {
		out = append(out, "Sort Key: "+clipOmitted(strings.Join(*n.SortKey, ", "), condBytes, 0))
	}

	if n.SortMethod != nil {
		line := "Sort Method: " + *n.SortMethod
		if n.SortSpaceKb != nil {
			line += fmt.Sprintf("  %s: %skB", deref(n.SortSpaceType), fmtNum(*n.SortSpaceKb))
		}

		out = append(out, line)
	}

	if n.HeapFetches != nil {
		out = append(out, "Heap Fetches: "+fmtNum(*n.HeapFetches))
	}

	if n.HeapBlocksLossy != nil || n.HeapBlocksExact != nil {
		out = append(out, fmt.Sprintf("Heap Blocks: exact=%s lossy=%s",
			fmtNum(deref(n.HeapBlocksExact)), fmtNum(deref(n.HeapBlocksLossy))))
	}

	if n.WorkersPlanned != nil {
		line := fmt.Sprintf("Workers Planned: %d", *n.WorkersPlanned)
		if n.WorkersLaunched != nil {
			line += fmt.Sprintf(" Launched: %d", *n.WorkersLaunched)
		}

		out = append(out, line)
	}

	if line := buffersLine(n.Buffers); line != "" {
		out = append(out, line)
	}

	return out
}

func buffersLine(bf *apiclient.PlanBuffers) string {
	if bf == nil {
		return ""
	}

	var parts []string

	type counter struct {
		name string
		v    float64
	}

	for _, g := range []struct {
		scope string
		kv    []counter
	}{
		{"shared", []counter{{"hit", bf.SharedHit}, {"read", bf.SharedRead}, {"dirtied", bf.SharedDirtied}, {"written", bf.SharedWritten}}},
		{"local", []counter{{"hit", bf.LocalHit}, {"read", bf.LocalRead}, {"dirtied", bf.LocalDirtied}, {"written", bf.LocalWritten}}},
		{"temp", []counter{{"read", bf.TempRead}, {"written", bf.TempWritten}}},
	} {
		var kv []string

		for _, c := range g.kv {
			if c.v != 0 {
				kv = append(kv, c.name+"="+fmtNum(c.v))
			}
		}

		if len(kv) > 0 {
			parts = append(parts, g.scope+" "+strings.Join(kv, " "))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "Buffers: " + strings.Join(parts, ", ")
}

// clipOmitted cuts s to n bytes on a rune boundary; the marker counts every
// byte missing from the original, including the ones the API already dropped.
func clipOmitted(s string, n, omitted int) string {
	if len(s) > n {
		cut := n
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}

		omitted += len(s) - cut
		s = s[:cut]
	}

	if omitted == 0 {
		return s
	}

	return fmt.Sprintf("%s…[+%d bytes]", s, omitted)
}

func fmtNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
