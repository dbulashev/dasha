package indexadvisor

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// maxIdentLen is NAMEDATALEN - 1. PostgreSQL silently truncates anything
	// longer, and the script would then attach an index by a name the server
	// never created.
	maxIdentLen = 63
	// ddlMaxPartitions caps how many partitions the script spells out. Trees of
	// several hundred are ordinary, and nobody reads a thousand-line suggestion:
	// past the cap the script says what it left out instead of looking complete.
	ddlMaxPartitions = 20
)

// ddlScope is what a partitioned candidate's DDL needs beyond the candidate
// itself: the tree under the root, and every identifier pg_class already holds.
// ATTACH names both indexes, so the script cannot leave the naming to the server
// the way a plain CREATE INDEX does — and a name that collides fails the script.
type ddlScope struct {
	children map[RelKey][]RelKey
	kinds    map[RelKey]string
	taken    map[RelKey]bool
}

func newDDLScope(cat Catalog) *ddlScope {
	s := &ddlScope{
		children: make(map[RelKey][]RelKey),
		kinds:    make(map[RelKey]string, len(cat.Relations)),
		taken:    make(map[RelKey]bool, len(cat.Relations)),
	}

	for key, rel := range cat.Relations {
		s.kinds[key] = rel.Kind
		s.taken[key] = true

		if rel.Parent.Name != "" {
			s.children[rel.Parent] = append(s.children[rel.Parent], key)
		}
	}

	// Indexes share the namespace of tables, so they are taken names too.
	for key, indexes := range cat.Indexes {
		for _, idx := range indexes {
			s.taken[RelKey{Schema: key.Schema, Name: idx.Name}] = true
		}
	}

	for _, kids := range s.children {
		sort.Slice(kids, func(i, j int) bool { return kids[i].Name < kids[j].Name })
	}

	return s
}

// leaves counts the partitions that hold data. Those are the ones a concurrent
// build has to run on, and the ones that will maintain the index afterwards.
func (s *ddlScope) leaves(node RelKey) int {
	if s.kinds[node] != partitionedKind {
		return 1
	}

	n := 0
	for _, child := range s.children[node] {
		n += s.leaves(child)
	}

	return n
}

// reserve names an index the way PostgreSQL would and marks the name used, so
// two candidates on the same table cannot be handed the same one.
func (s *ddlScope) reserve(rel RelKey, columns []string) RelKey {
	base := rel.Name + "_" + strings.Join(columns, "_") + "_idx"

	for i := 0; ; i++ {
		suffix := ""
		if i > 0 {
			suffix = strconv.Itoa(i)
		}

		key := RelKey{Schema: rel.Schema, Name: truncIdent(base, suffix)}
		if !s.taken[key] {
			s.taken[key] = true

			return key
		}
	}
}

// ddlFor renders what the user is invited to run.
//
// CONCURRENTLY is the default because the alternative locks the table against
// writes for the whole build. A partitioned table cannot have it — PostgreSQL
// rejects CREATE INDEX CONCURRENTLY on the root outright — so there the script
// takes the way round the manual prescribes: an ON ONLY index on the root, which
// is created invalid and locks nothing, then a concurrent build on every
// partition and an ATTACH each. The root index turns valid with the last one.
func ddlFor(key RelKey, columns, predicate []string, s *ddlScope) string {
	cols := columnList(columns) + whereClause(predicate)

	if s.kinds[key] != partitionedKind {
		return "CREATE INDEX CONCURRENTLY ON " + key.quoted() + " " + cols + ";"
	}

	// A tree with no partitions holds no rows, so there is nothing for a lock to
	// hold up and nothing to attach.
	if len(s.children[key]) == 0 {
		return "CREATE INDEX ON " + key.quoted() + " " + cols + ";"
	}

	return partitionDDL(key, columns, cols, s)
}

func whereClause(predicate []string) string {
	if len(predicate) == 0 {
		return ""
	}

	return " WHERE " + predicateSQL(predicate)
}

func predicateSQL(predicate []string) string {
	tests := make([]string, 0, len(predicate))
	for _, c := range predicate {
		tests = append(tests, quoteIdent(c)+" IS NULL")
	}

	return strings.Join(tests, " AND ")
}

func partitionDDL(root RelKey, columns []string, cols string, s *ddlScope) string {
	w := &ddlWriter{
		scope:   s,
		columns: columns,
		cols:    cols,
		budget:  ddlMaxPartitions,
	}

	rootIdx := s.reserve(root, columns)

	w.line("CREATE INDEX " + quoteIdent(rootIdx.Name) + " ON ONLY " + root.quoted() + " " + w.cols + ";")
	w.children(root, rootIdx)

	if w.skipped > 0 {
		w.line("")
		w.line("-- " + strconv.Itoa(w.skipped) + " more partitions are not listed here: " +
			quoteIdent(rootIdx.Name) + " stays invalid until every one of them is built and attached the same way")
	}

	return w.buf.String()
}

// ddlWriter walks the partition tree from the root down, so that every level
// gets its own index and its own ATTACH: an index may only be attached to the
// index of the table its own table is a direct partition of.
type ddlWriter struct {
	scope   *ddlScope
	columns []string
	cols    string
	buf     strings.Builder
	budget  int
	skipped int
}

func (w *ddlWriter) children(node, nodeIdx RelKey) {
	for _, child := range w.scope.children[node] {
		if w.budget <= 0 {
			w.skipped += w.scope.leaves(child)

			continue
		}

		w.budget--

		w.line("")
		w.subtree(child, nodeIdx)
	}
}

func (w *ddlWriter) subtree(node, parentIdx RelKey) {
	nodeIdx := w.scope.reserve(node, w.columns)

	if w.scope.kinds[node] == partitionedKind {
		w.line("CREATE INDEX " + quoteIdent(nodeIdx.Name) + " ON ONLY " + node.quoted() + " " + w.cols + ";")
		w.children(node, nodeIdx)
	} else {
		w.line("CREATE INDEX CONCURRENTLY " + quoteIdent(nodeIdx.Name) + " ON " + node.quoted() + " " + w.cols + ";")
	}

	w.line("ALTER INDEX " + parentIdx.quoted() + " ATTACH PARTITION " + nodeIdx.quoted() + ";")
}

func (w *ddlWriter) line(s string) {
	if w.buf.Len() > 0 {
		w.buf.WriteByte('\n')
	}

	w.buf.WriteString(s)
}

func columnList(columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, c := range columns {
		quoted = append(quoted, quoteIdent(c))
	}

	return "(" + strings.Join(quoted, ", ") + ")"
}

func (k RelKey) quoted() string { return quoteIdent(k.Schema) + "." + quoteIdent(k.Name) }

// quoteIdent always quotes. An unquoted identifier would be folded to lower case
// and would break on a keyword, and this string is handed to a human to run
// against production — pg_dump quotes everything for the same reason.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func truncIdent(base, suffix string) string {
	limit := maxIdentLen - len(suffix)
	if len(base) <= limit {
		return base + suffix
	}

	base = base[:limit]
	// The cut is by bytes, and landing inside a character would make the name
	// unpronounceable to the server.
	for len(base) > 0 && !utf8.ValidString(base) {
		base = base[:len(base)-1]
	}

	return base + suffix
}
