package explain

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	nodeLineRe  = regexp.MustCompile(`^(\s*)(->\s+)?(\S.*?)\s\s\((cost=|actual |never executed)`)
	costRe      = regexp.MustCompile(`\(cost=([0-9]+\.?[0-9]*)\.\.([0-9]+\.?[0-9]*) rows=([0-9]+\.?[0-9]*) width=([0-9]+)\)`)
	actualRe    = regexp.MustCompile(`\(actual (?:time=([0-9]+\.?[0-9]*)\.\.([0-9]+\.?[0-9]*) )?rows=([0-9]+\.?[0-9]*) loops=([0-9]+\.?[0-9]*)\)`)
	triggerRe   = regexp.MustCompile(`^Trigger (.+?)(?: on (\S+))?: time=([0-9.]+) calls=([0-9.]+)`)
	jitTimeRe   = regexp.MustCompile(`(Generation|Inlining|Optimization|Emission|Total) ([0-9.]+) ms`)
	sortSpaceRe = regexp.MustCompile(`\s\s(Memory|Disk): ([0-9]+)kB`)
	subplanRe   = regexp.MustCompile(`^(InitPlan|SubPlan|CTE)\b`)
)

type textFrame struct {
	node     *Node
	indent   int
	attrsAt  int // indent of this node's own attribute lines, -1 until the first one
	lastAttr string
}

// ParseText reads the indented tree format. Both auto_explain and EXPLAIN print
// the same layout; the difference is only which optional blocks are present.
func ParseText(body string, src Source) (Plan, error) {
	lines := splitLines(body)
	p := Plan{Source: src, Format: FormatText}

	i := 0
	var queryText []string

	for ; i < len(lines); i++ {
		if _, ok := matchNodeLine(lines[i]); ok {
			break
		}

		line := strings.TrimLeft(lines[i], " \t")
		switch {
		case strings.HasPrefix(line, "Query Text: "):
			queryText = append(queryText, strings.TrimPrefix(line, "Query Text: "))
		// The bind parameters stand between the query text and the plan.
		case strings.HasPrefix(line, "Query Parameters: "):
			p.QueryParams = strings.TrimPrefix(line, "Query Parameters: ")
		case len(queryText) > 0:
			queryText = append(queryText, lines[i])
		}
	}

	p.QueryText = strings.TrimRight(strings.Join(queryText, "\n"), " \n")

	if i == len(lines) {
		return Plan{}, parseErrorf("no plan node found")
	}

	root, _ := matchNodeLine(lines[i])
	p.Root = *root
	p.Caps.Timing = hasTiming(lines[i])
	rootIndent := nodeIndent(lines[i])
	stack := []*textFrame{{node: &p.Root, indent: rootIndent, attrsAt: -1}}
	i++

	var (
		pending string
		inTail  bool
	)

	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := nodeIndent(line)
		trimmed := strings.TrimLeft(line, " \t")

		if inTail {
			applyTail(&p, trimmed, indent > rootIndent)

			continue
		}

		if node, ok := matchNodeLine(line); ok {
			for len(stack) > 1 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}

			parent := stack[len(stack)-1]
			node.ParentRel = childRole(parent.node, planChildren(parent.node))

			if pending != "" {
				node.SubplanName = pending
				node.ParentRel = subplanRole(pending)
				pending = ""
			}

			if hasTiming(line) {
				p.Caps.Timing = true
			}

			parent.node.Children = append(parent.node.Children, *node)
			child := &parent.node.Children[len(parent.node.Children)-1]
			stack = append(stack, &textFrame{node: child, indent: indent, attrsAt: -1})

			continue
		}

		if subplanRe.MatchString(trimmed) && !strings.Contains(trimmed, ": ") {
			pending = trimmed
			for len(stack) > 1 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}

			continue
		}

		if indent <= rootIndent && applyTail(&p, trimmed, false) {
			inTail = true

			continue
		}

		for len(stack) > 1 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		applyTextAttr(stack[len(stack)-1], trimmed, indent)
	}

	fillTextCaps(&p)

	return p, nil
}

func splitLines(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")

	return strings.Split(strings.TrimRight(body, "\n"), "\n")
}

func nodeIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 8
		default:
			return n
		}
	}

	return n
}

// matchNodeLine recognises a node line and fills everything the line itself
// carries: the label modifiers, the estimates and the measured numbers.
func matchNodeLine(line string) (*Node, bool) {
	m := nodeLineRe.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}

	node := &Node{}
	parseLabel(m[3], node)

	if c := costRe.FindStringSubmatch(line); c != nil {
		node.StartupCost = parseFloat(c[1])
		node.TotalCost = parseFloat(c[2])
		node.PlanRows = parseFloat(c[3])
		node.PlanWidth = int(parseFloat(c[4]))
	}

	if a := actualRe.FindStringSubmatch(line); a != nil {
		node.Actual = &Actual{
			StartupTime: parseFloat(a[1]),
			TotalTime:   parseFloat(a[2]),
			Rows:        parseFloat(a[3]),
			Loops:       parseFloat(a[4]),
		}
	} else if strings.Contains(line, "(never executed)") {
		node.Actual = &Actual{}
	}

	return node, true
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}

	return v
}

// aliasOnlyTypes print an alias where other scans print a relation.
var aliasOnlyTypes = map[string]bool{"Subquery Scan": true, "Values Scan": true}

// parseLabel splits the printed node name into the canonical type plus the
// modifiers the JSON format keeps in fields of their own.
func parseLabel(label string, n *Node) {
	label = strings.TrimSpace(label)

	if rest, ok := strings.CutPrefix(label, "Parallel "); ok {
		n.Parallel = true
		label = rest
	}

	// Both prefixes can stand on one node, parallel first.
	label = strings.TrimPrefix(label, "Async ")

	for _, mode := range []string{"Partial", "Finalize"} {
		if rest, ok := strings.CutPrefix(label, mode+" "); ok {
			n.PartialMode = mode
			label = rest

			break
		}
	}

	if head, tail, ok := strings.Cut(label, " on "); ok {
		label = head
		parseOnClause(tail, n)
	}

	if head, tail, ok := strings.Cut(label, " using "); ok {
		label = head
		n.IndexName = unquoteIdent(tail)
	}

	if rest, ok := strings.CutSuffix(label, " Backward"); ok {
		n.ScanDirection = "Backward"
		label = rest
	}

	label = trimSetOpCommand(label)

	normalizeType(label, n)

	if aliasOnlyTypes[n.Type] && n.Relation != "" {
		n.Alias, n.Relation = n.Relation, ""
	}

	if n.Type == "Bitmap Index Scan" && n.IndexName == "" {
		n.IndexName, n.Relation = n.Relation, ""
	}

	if (n.Type == "Index Scan" || n.Type == "Index Only Scan") && n.ScanDirection == "" {
		n.ScanDirection = "Forward"
	}
}

func parseOnClause(s string, n *Node) {
	fields := splitIdents(s)
	if len(fields) == 0 {
		return
	}

	n.Schema, n.Relation = splitQualified(fields[0])

	if len(fields) > 1 {
		alias := unquoteIdent(fields[1])
		if alias != n.Relation {
			n.Alias = alias
		}
	}
}

// splitIdents cuts the "on" clause into identifiers, keeping a quoted one whole
// even when it holds a space, as "*SELECT* 1" does.
func splitIdents(s string) []string {
	var (
		out   []string
		token strings.Builder
		quote bool
	)

	flush := func() {
		if token.Len() > 0 {
			out = append(out, token.String())
			token.Reset()
		}
	}

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			quote = !quote

			token.WriteByte(c)
		case c == ' ' && !quote:
			flush()
		default:
			token.WriteByte(c)
		}
	}

	flush()

	return out
}

// splitQualified cuts schema.relation at the dot that stands outside quotes and
// unquotes the halves apart: a quoted name may hold a dot of its own.
func splitQualified(name string) (string, string) {
	quote := false

	for i := 0; i < len(name); i++ {
		switch name[i] {
		case '"':
			quote = !quote
		case '.':
			if !quote {
				return unquoteIdent(name[:i]), unquoteIdent(name[i+1:])
			}
		}
	}

	return "", unquoteIdent(name)
}

func unquoteIdent(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}

	return s
}

var joinTypes = []string{"Left", "Right Semi", "Right Anti", "Right", "Full", "Semi", "Anti"}

var setOpCommands = []string{"Intersect All", "Intersect", "Except All", "Except"}

// trimSetOpCommand drops the command the text format appends to a SetOp label;
// JSON keeps it in a field of its own.
func trimSetOpCommand(label string) string {
	if !strings.HasPrefix(label, "SetOp") && !strings.HasPrefix(label, "HashSetOp") {
		return label
	}

	for _, cmd := range setOpCommands {
		if rest, ok := strings.CutSuffix(label, " "+cmd); ok {
			return rest
		}
	}

	return label
}

// normalizeType maps the printed name onto the JSON node type plus strategy,
// join type or operation — "HashAggregate" is an Aggregate with a Hashed
// strategy, "Hash Left Join" a Hash Join with a Left join type.
func normalizeType(label string, n *Node) {
	switch label {
	case "HashAggregate":
		n.Type, n.Strategy = "Aggregate", "Hashed"

		return
	case "GroupAggregate":
		n.Type, n.Strategy = "Aggregate", "Sorted"

		return
	case "MixedAggregate":
		n.Type, n.Strategy = "Aggregate", "Mixed"

		return
	case "Aggregate":
		n.Type, n.Strategy = "Aggregate", "Plain"

		return
	case "HashSetOp":
		n.Type, n.Strategy = "SetOp", "Hashed"

		return
	case "SetOp":
		n.Type, n.Strategy = "SetOp", "Sorted"

		return
	case "Insert", "Update", "Delete", "Merge":
		n.Type, n.Operation = "ModifyTable", label

		return
	case "Foreign Scan":
		n.Type, n.Operation = "Foreign Scan", "Select"

		return
	case "Foreign Insert", "Foreign Update", "Foreign Delete":
		n.Type, n.Operation = "Foreign Scan", strings.TrimPrefix(label, "Foreign ")

		return
	}

	if base, ok := strings.CutSuffix(label, " Join"); ok {
		for _, jt := range joinTypes {
			if head, found := strings.CutSuffix(base, " "+jt); found {
				n.JoinType = jt
				n.Type = joinTypeName(head)

				return
			}
		}

		n.JoinType = "Inner"
		n.Type = joinTypeName(base)

		return
	}

	if label == "Nested Loop" {
		n.Type, n.JoinType = "Nested Loop", "Inner"

		return
	}

	n.Type = label
}

// joinTypeName restores the node type from the head of a join label: the text
// format drops "Join" from the middle ("Merge Left Join", not "Merge Join Left").
func joinTypeName(head string) string {
	switch head {
	case "Nested Loop":
		return "Nested Loop"
	case "Merge":
		return "Merge Join"
	case "Hash":
		return "Hash Join"
	default:
		return head + " Join"
	}
}

// planChildren counts the children that hold a position in the tree: an
// InitPlan or a SubPlan sits beside them without taking one.
func planChildren(parent *Node) int {
	n := 0

	for i := range parent.Children {
		if parent.Children[i].SubplanName == "" {
			n++
		}
	}

	return n
}

func childRole(parent *Node, index int) string {
	switch parent.Type {
	case "Append", "Merge Append", "BitmapAnd", "BitmapOr":
		return RelMember
	case "Subquery Scan":
		return RelSubquery
	}

	switch index {
	case 0:
		return RelOuter
	case 1:
		return RelInner
	default:
		return RelMember
	}
}

func subplanRole(header string) string {
	if strings.HasPrefix(header, "SubPlan") {
		return RelSubPlan
	}

	return RelInitPlan
}

func applyTextAttr(f *textFrame, line string, indent int) {
	if f.attrsAt == -1 {
		f.attrsAt = indent
	}

	if indent > f.attrsAt {
		// Deeper than this node's own attributes: a per-worker block, or the
		// wrapped tail of the previous attribute.
		if !strings.Contains(line, ": ") && f.lastAttr != "" {
			appendToAttr(f.node, f.lastAttr, line)
		}

		return
	}

	key, value, ok := strings.Cut(line, ": ")
	if !ok {
		return
	}

	f.lastAttr = key
	setTextAttr(f.node, key, strings.TrimSpace(value), line)
}

func setTextAttr(n *Node, key, value, raw string) {
	switch key {
	case "Filter", "One-Time Filter":
		n.Filter = value
	case "Index Cond", "TID Cond":
		n.IndexCond = value
	case "Recheck Cond":
		n.RecheckCond = value
	case "Hash Cond", "Merge Cond", "Join Filter":
		n.JoinCond = value
	case "Rows Removed by Filter":
		n.RowsRemovedByFilter = ptrFloat(parseFloat(value))
	case "Rows Removed by Join Filter":
		n.RowsRemovedByJoinFilter = ptrFloat(parseFloat(value))
	case "Heap Fetches":
		n.HeapFetches = ptrFloat(parseFloat(value))
	case "Sort Key":
		n.SortKey = splitTopLevel(value)
	case "Sort Method":
		setSortMethod(n, raw)
	case "Workers Planned":
		n.WorkersPlanned = ptrInt(int(parseFloat(value)))
	case "Workers Launched":
		n.WorkersLaunched = ptrInt(int(parseFloat(value)))
	case "Heap Blocks":
		setHeapBlocks(n, value)
	case "Buffers":
		n.Buffers = parseTextBuffers(value)
	}
}

func appendToAttr(n *Node, key, line string) {
	line = strings.TrimSpace(line)

	switch key {
	case "Filter", "One-Time Filter":
		n.Filter += " " + line
	case "Index Cond", "TID Cond":
		n.IndexCond += " " + line
	case "Recheck Cond":
		n.RecheckCond += " " + line
	case "Hash Cond", "Merge Cond", "Join Filter":
		n.JoinCond += " " + line
	}
}

// setSortMethod takes the whole line: the method itself may contain spaces
// ("external merge", "still in progress") and the space used follows it on the
// same line.
func setSortMethod(n *Node, raw string) {
	_, value, ok := strings.Cut(raw, ": ")
	if !ok {
		return
	}

	if m := sortSpaceRe.FindStringSubmatch(value); m != nil {
		n.SortSpaceType = m[1]
		n.SortSpaceKB = ptrFloat(parseFloat(m[2]))
		value = value[:strings.Index(value, m[0])]
	}

	n.SortMethod = strings.TrimSpace(value)
}

func setHeapBlocks(n *Node, value string) {
	for _, part := range strings.Fields(value) {
		kind, num, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}

		switch kind {
		case "exact":
			n.HeapBlocksExact = ptrFloat(parseFloat(num))
		case "lossy":
			n.HeapBlocksLossy = ptrFloat(parseFloat(num))
		}
	}
}

// parseTextBuffers reads "shared hit=2 read=1 dirtied=1, temp read=5 written=5".
func parseTextBuffers(value string) *Buffers {
	b := &Buffers{}

	for _, group := range strings.Split(value, ",") {
		fields := strings.Fields(group)
		if len(fields) < 2 {
			continue
		}

		scope := fields[0]
		for _, f := range fields[1:] {
			kind, num, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}

			v := parseFloat(num)
			switch scope + " " + kind {
			case "shared hit":
				b.SharedHit = v
			case "shared read":
				b.SharedRead = v
			case "shared dirtied":
				b.SharedDirtied = v
			case "shared written":
				b.SharedWritten = v
			case "local hit":
				b.LocalHit = v
			case "local read":
				b.LocalRead = v
			case "local dirtied":
				b.LocalDirtied = v
			case "local written":
				b.LocalWritten = v
			case "temp read":
				b.TempRead = v
			case "temp written":
				b.TempWritten = v
			}
		}
	}

	return b
}

// applyTail handles everything printed after the tree. It reports whether the
// line belongs there, which is also what ends the tree.
func applyTail(p *Plan, line string, nested bool) bool {
	if nested {
		return applyJITLine(p, line)
	}

	switch {
	case strings.HasPrefix(line, "Planning Time: "):
		p.PlanningTime = ptrFloat(parseFloat(strings.TrimSuffix(strings.TrimPrefix(line, "Planning Time: "), " ms")))
	case strings.HasPrefix(line, "Execution Time: "):
		p.ExecutionTime = ptrFloat(parseFloat(strings.TrimSuffix(strings.TrimPrefix(line, "Execution Time: "), " ms")))
	case strings.HasPrefix(line, "Query Identifier: "):
		id, err := strconv.ParseInt(strings.TrimPrefix(line, "Query Identifier: "), 10, 64)
		if err != nil {
			return true
		}

		p.QueryID, p.HasQueryID = id, true
	case strings.HasPrefix(line, "Settings: "):
		p.Settings = parseSettings(strings.TrimPrefix(line, "Settings: "))
	case line == "JIT:":
		p.JIT = &JIT{}
	case line == "Planning:":
		return true
	default:
		m := triggerRe.FindStringSubmatch(line)
		if m == nil {
			return false
		}

		p.Triggers = append(p.Triggers, Trigger{
			Name:     m[1],
			Relation: m[2],
			Time:     parseFloat(m[3]),
			Calls:    parseFloat(m[4]),
		})
	}

	return true
}

func applyJITLine(p *Plan, line string) bool {
	if p.JIT == nil {
		return true
	}

	switch {
	case strings.HasPrefix(line, "Functions: "):
		p.JIT.Functions = int(parseFloat(strings.TrimPrefix(line, "Functions: ")))
	case strings.HasPrefix(line, "Timing: "):
		for _, m := range jitTimeRe.FindAllStringSubmatch(line, -1) {
			v := ptrFloat(parseFloat(m[2]))
			switch m[1] {
			case "Generation":
				p.JIT.Generation = v
			case "Inlining":
				p.JIT.Inlining = v
			case "Optimization":
				p.JIT.Optimization = v
			case "Emission":
				p.JIT.Emission = v
			case "Total":
				p.JIT.Total = v
			}
		}
	}

	return true
}

// parseSettings reads "work_mem = '64MB', search_path = '\"$user\", public'".
func parseSettings(line string) map[string]string {
	out := map[string]string{}

	for _, part := range splitTopLevel(line) {
		key, value, ok := strings.Cut(part, " = ")
		if !ok {
			continue
		}

		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
		}

		out[strings.TrimSpace(key)] = value
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// splitTopLevel splits on commas that are neither inside quotes nor inside
// parentheses, the way sort keys and settings are printed.
func splitTopLevel(s string) []string {
	var (
		out   []string
		depth int
		quote byte
		start int
	)

	for i := range len(s) {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}

	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}

	return out
}

// hasTiming separates a plan built with TIMING OFF, where a node prints only
// its row count, from one that measured time.
func hasTiming(line string) bool {
	return strings.Contains(line, "(actual time=")
}

func fillTextCaps(p *Plan) {
	p.Caps.Settings = len(p.Settings) > 0
	if p.ExecutionTime != nil {
		p.Caps.Timing = true
	}

	p.Walk(func(_ []int, n *Node) bool {
		if n.Actual != nil {
			p.Caps.Actual = true
		}

		if n.Buffers != nil {
			p.Caps.Buffers = true
		}

		return true
	})
}
