package indexadvisor

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/sqlparse"
)

// Judgement calls that are not worth a configuration knob: they shape a warning,
// never whether a candidate exists, so getting one slightly wrong costs a line of
// caveat rather than a wrong recommendation.
const (
	// lowWeightPct is where a candidate stops being worth acting on first.
	lowWeightPct = 1.0
	// writeHeavyRatio and writeHeavyMinCalls decide when maintenance is likely
	// to outweigh the reads — see writeHeavy.
	writeHeavyRatio    = 10.0
	writeHeavyMinCalls = 1000
	// btreeMethod is the only access method this step proposes or compares against.
	btreeMethod = "btree"
	// uniqueNDistinct: pg_stats writes -1 for a unique column (negative is a fraction).
	uniqueNDistinct = -1.0
	// nullPredicateMaxFrac is where IS NULL stops excluding anything worth an index.
	nullPredicateMaxFrac = 0.5
	// manyIndexesThreshold is when a table's index list is itself the finding.
	manyIndexesThreshold = 10
	// partitionedKind is the relkind of a partitioned table.
	partitionedKind = "p"
	// matviewKind is the relkind of a materialized view.
	matviewKind = "m"
)

// Build turns one database's workload and catalog into ranked index candidates.
//
// It performs no I/O and asks no planner: a candidate here means "these
// statements filter on these columns and no existing index serves that", which
// is a structural claim, not a promise of a speed-up. Everything that could make
// the claim wrong — missing statistics, a write-heavy table, a partial catalog —
// travels with the candidate as a warning rather than being silently averaged in.
func Build(w Workload, cat Catalog, cfg Config) Report {
	started := time.Now()
	cfg = cfg.WithDefaults()

	groups := collapse(w.Entries)

	b := &builder{
		cat:        cat,
		cfg:        cfg,
		ddl:        newDDLScope(cat),
		drafts:     make(map[RelKey][]*draft),
		skipped:    make(map[string]int, len(w.NotParsed)),
		columnsBy:  make(map[RelKey]map[string]Column),
		writeCalls: make(map[RelKey]int64),
	}

	// Both need the catalog, so they run on the built builder.
	b.totalTime = b.workloadTime(groups)
	b.collectWrites(groups)

	// The collector's own failures and the ones found here share a tally: from
	// the user's side both answer the same question — why is this list short.
	for code, n := range w.NotParsed {
		b.skipped[code] += n
	}

	for _, g := range groups {
		if reason := b.addStatement(g); reason != "" {
			b.skipped[reason]++
		}
	}

	candidates := b.candidates()

	rep := Report{
		Candidates: candidates,
		NotParsed:  notParsedList(b.skipped),
		Summary: Summary{
			PgssAvailable:     w.Available,
			AnalyzedQueries:   len(w.Entries),
			CollapsedGroups:   len(groups),
			NotParsedCount:    sumCounts(b.skipped),
			CoveredTimePct:    coveredTimePct(candidates),
			CatalogTruncated:  cat.Truncated,
			Hosts:             sortedHosts(w.Hosts),
			HostsWithoutStats: sortedHosts(w.NoStats),
		},
		UnreachableHosts: sortedHosts(w.Unreachable),
	}

	rep.DurationMs = time.Since(started).Milliseconds()

	return rep
}

// collapse folds entries sharing a fingerprint into one unit of work. Three
// things depend on it: a statement written twice with different constants must
// weigh once, pgpro_stats keys its view by planid as well, so the same statement
// arrives as several rows there, and the same statement runs on every host of the
// cluster — its weight is the load it puts on the cluster, not on one instance.
//
// Sums are what makes the cluster-wide reading right: a query costing 40 % of the
// time on each of three replicas is 40 % of the cluster's time, not 120 % of one
// host's, because the denominator is summed over the same hosts.
func collapse(entries []WorkloadEntry) []WorkloadEntry {
	out := make([]WorkloadEntry, 0, len(entries))
	index := make(map[string]int, len(entries))

	for _, e := range entries {
		i, ok := index[e.Fingerprint]
		if !ok || e.Fingerprint == "" {
			e.QueryIDs = slices.Clone(e.QueryIDs)
			e.Hosts = slices.Clone(e.Hosts)
			e.QueryIDByHost = maps.Clone(e.QueryIDByHost)
			index[e.Fingerprint] = len(out)

			out = append(out, e)

			continue
		}

		g := &out[i]

		// Identifiers repeat across hosts: queryid is derived from the parse tree,
		// so the same statement carries the same one on every instance, and listing
		// it three times would only make the covered statement look ambiguous.
		for _, id := range e.QueryIDs {
			if !slices.Contains(g.QueryIDs, id) {
				g.QueryIDs = append(g.QueryIDs, id)
			}
		}

		for _, h := range e.Hosts {
			g.Hosts = appendUnique(g.Hosts, h)
		}

		// The pairing survives the fold, which is the whole reason it is carried:
		// after this the two lists above can no longer say which host answered
		// with which identifier. The first row read on a host wins — a host can
		// hold two statements of one fingerprint, and either identifier is a real
		// answer for it.
		if len(e.QueryIDByHost) > 0 && g.QueryIDByHost == nil {
			g.QueryIDByHost = make(map[string]int64, len(e.QueryIDByHost))
		}

		for h, id := range e.QueryIDByHost {
			if _, seen := g.QueryIDByHost[h]; !seen {
				g.QueryIDByHost[h] = id
			}
		}

		// Folded rows can carry different spellings of the same statement: pg_stat_statements
		// normalizes, but not identically across server versions, and the hosts of a cluster
		// need not run the same one. Keeping the longest makes the text shown deterministic
		// rather than a function of which host answered first.
		if len(e.Query) > len(g.Query) {
			g.Query = e.Query
		}

		g.Calls += e.Calls
		g.TotalTimeMs += e.TotalTimeMs
		g.Rows += e.Rows
	}

	return out
}

// workloadTime is the denominator of every weight. Monitoring is left out of it:
// on a polled database it is most of pg_stat_statements.
func (b *builder) workloadTime(entries []WorkloadEntry) float64 {
	var sum float64

	for _, e := range entries {
		if b.systemOnly(e.Stmt) {
			continue
		}

		sum += e.TotalTimeMs
	}

	return sum
}

// collectWrites reads INSERTs too: they yield no candidate but price every one.
func (b *builder) collectWrites(entries []WorkloadEntry) {
	for _, e := range entries {
		for _, r := range e.Stmt.Written {
			key, why := b.resolveRef(r)
			if why != "" {
				continue
			}

			b.writeCalls[key] += e.Calls
		}
	}
}

// systemOnly is false for a catalog joined to a real table: that still says how
// the table is queried.
func (b *builder) systemOnly(stmt sqlparse.Statement) bool {
	if len(stmt.Tables) == 0 {
		return false
	}

	for _, r := range stmt.Tables {
		if _, why := b.resolveRef(r); why != ReasonSystemRelation {
			return false
		}
	}

	return true
}

// isSystemRef: the catalog scan returns every ordinary table in every user
// schema, so a pg_-prefixed name it missed is a catalog or a monitoring view.
func isSystemRef(r sqlparse.Ref) bool {
	if r.Schema == "information_schema" || strings.HasPrefix(r.Schema, "pg_") {
		return true
	}

	return strings.HasPrefix(r.Name, "pg_")
}

func unknownReason(r sqlparse.Ref) string {
	if isSystemRef(r) {
		return ReasonSystemRelation
	}

	return ReasonUnknownRelation
}

func sumCounts(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}

	return total
}

// draft is a candidate under construction: the same columns on the same table
// may be reached from several statements, and each adds its weight.
type draft struct {
	key     RelKey
	columns []string
	// predicate are the IS NULL columns of a partial candidate, sorted.
	predicate    []string
	covered      map[string]CoveredQuery
	order        []string // fingerprints in insertion order, for a stable output
	statsMissing bool
	wideIndex    bool
	requested    int // columns the statements asked for, before the width cap
}

type builder struct {
	cat Catalog
	cfg Config
	// ddl carries the partition tree and the names already used, and hands out the
	// index names the ATTACH of a partitioned candidate has to spell out — so it
	// is per report, and shared by every candidate of it.
	ddl       *ddlScope
	totalTime float64
	drafts    map[RelKey][]*draft
	skipped   map[string]int
	columnsBy map[RelKey]map[string]Column
	// writeCalls covers the same window as the reads, unlike Catalog.Writes.
	writeCalls map[RelKey]int64
}

// addStatement adds whatever one statement is worth. The returned code is the
// reason it was worth nothing — empty when it produced at least one candidate.
func (b *builder) addStatement(e WorkloadEntry) string {
	// An INSERT is load on the table, not a reason to index it; utility
	// statements are neither. Neither is a failure worth reporting.
	if !yieldsCandidates(e.Stmt.Kind) {
		return ""
	}

	tables, tableReason := b.resolveTables(e.Stmt.Tables)
	if len(tables) == 0 {
		return firstNonEmpty(tableReason, ReasonUnknownRelation)
	}

	byTable, bindReason := b.bindUsages(e.Stmt.Usages, tables)

	produced := false
	dropReason := ""

	for _, key := range sortedKeys(byTable) {
		d, reason := b.buildDraft(key, byTable[key])
		if d == nil {
			dropReason = firstNonEmpty(dropReason, reason)

			continue
		}

		b.attach(d, e)

		produced = true
	}

	if produced {
		return ""
	}

	return firstNonEmpty(dropReason, bindReason, tableReason, statementReason(e.Stmt))
}

// yieldsCandidates: a statement whose WHERE clause an index could serve.
func yieldsCandidates(kind sqlparse.Kind) bool {
	switch kind {
	case sqlparse.KindSelect, sqlparse.KindUpdate, sqlparse.KindDelete, sqlparse.KindMerge:
		return true
	case sqlparse.KindInsert, sqlparse.KindUtility, sqlparse.KindOther:
		return false
	default:
		return false
	}
}

// statementReason explains a statement that resolved cleanly and still produced
// nothing. What the parser had to skip explains it better than a generic code.
func statementReason(stmt sqlparse.Statement) string {
	if len(stmt.Unsupported) > 0 {
		return stmt.Unsupported[0]
	}

	return ReasonNoIndexablePredicate
}

func (b *builder) resolveTables(refs []sqlparse.Ref) ([]RelKey, string) {
	var (
		out    []RelKey
		reason string
	)

	seen := make(map[RelKey]bool, len(refs))

	for _, r := range refs {
		key, why := b.resolveRef(r)
		if why != "" {
			reason = firstNonEmpty(reason, why)

			continue
		}

		if seen[key] {
			continue
		}

		seen[key] = true

		out = append(out, key)
	}

	return out, reason
}

// resolveRef maps a name as the statement wrote it onto a catalog relation.
//
// An unqualified name that several schemas answer to is refused rather than
// guessed: pg_stat_statements records no search_path, so the guess would be a
// coin toss, and a wrong table is worse advice than no advice.
func (b *builder) resolveRef(r sqlparse.Ref) (RelKey, string) {
	key := RelKey{Schema: r.Schema, Name: r.Name}

	if r.Schema == "" {
		keys := b.cat.ByName[r.Name]

		switch len(keys) {
		case 0:
			return RelKey{}, unknownReason(r)
		case 1:
			key = keys[0]
		default:
			return RelKey{}, ReasonAmbiguousName
		}
	}

	rel, ok := b.cat.Relations[key]
	if !ok {
		return RelKey{}, unknownReason(r)
	}

	// An index belongs on the partitioned root, whichever partition the statement
	// happened to name — PostgreSQL propagates it down from there.
	if rel.IsPartition() {
		if _, ok := b.cat.Relations[rel.Root]; ok {
			return rel.Root, ""
		}
	}

	return key, ""
}

func (b *builder) bindUsages(
	usages []sqlparse.Usage,
	tables []RelKey,
) (map[RelKey][]sqlparse.Usage, string) {
	out := make(map[RelKey][]sqlparse.Usage)
	reason := ""

	for _, u := range usages {
		key, why := b.bindUsage(u, tables)
		if why != "" {
			reason = firstNonEmpty(reason, why)

			continue
		}

		out[key] = append(out[key], u)
	}

	return out, reason
}

// bindUsage decides which table a column belongs to. The parser leaves a bare
// column unattributed when the statement had several tables in scope, and only
// the catalog can finish the job — when exactly one of those tables has the
// column. Two candidates for the owner means no candidate for the index.
func (b *builder) bindUsage(u sqlparse.Usage, tables []RelKey) (RelKey, string) {
	if u.Ref.Name != "" {
		return b.resolveRef(u.Ref)
	}

	var (
		found   RelKey
		matches int
	)

	for _, key := range tables {
		if _, ok := b.columns(key)[u.Column]; ok {
			found = key
			matches++
		}
	}

	switch matches {
	case 0:
		return RelKey{}, ReasonUnknownColumn
	case 1:
		return found, ""
	default:
		return RelKey{}, ReasonAmbiguousColumn
	}
}

func (b *builder) columns(key RelKey) map[string]Column {
	if cached, ok := b.columnsBy[key]; ok {
		return cached
	}

	byName := make(map[string]Column, len(b.cat.Columns[key]))
	for _, c := range b.cat.Columns[key] {
		byName[c.Name] = c
	}

	b.columnsBy[key] = byName

	return byName
}

// buildDraft assembles the key of one candidate: equality columns first, then at
// most one range column, then the ordering columns when they can still be served.
func (b *builder) buildDraft(key RelKey, usages []sqlparse.Usage) (*draft, string) {
	rel, ok := b.cat.Relations[key]
	if !ok {
		return nil, ReasonUnknownRelation
	}

	// Below this size the planner will pick a sequential scan anyway, and an
	// index would be maintenance for nothing.
	if rel.Rows < b.cfg.MinTableRows {
		return nil, ReasonTableTooSmall
	}

	cols := b.columns(key)
	r := splitRoles(usages)

	// A column the statement also scans by stays in the key, where IS NULL is an
	// indexable condition of its own; as a predicate it would pin that key column
	// to one value. An anti-join writes both roles: it joins by a column and then
	// tests the same column for NULL.
	nulls := slices.DeleteFunc(slices.Clone(r.nulls), func(name string) bool {
		return slices.Contains(r.equality, name) ||
			slices.Contains(r.ranges, name) ||
			slices.Contains(r.ordering, name)
	})

	predicate, predicateStats := selectivePredicate(nulls, cols)

	if b.servedByUniqueIndex(key, r.equality, predicate) {
		return nil, ReasonAlreadyIndexed
	}

	columns, statsMissing := b.orderEquality(r.equality, cols, rel.Rows)
	statsMissing = statsMissing || predicateStats

	// One range column at most, and everything after it in the key is unordered
	// for the scan — which is why the ordering columns are only worth adding when
	// there is no range predicate at all.
	if len(r.ranges) > 0 {
		columns = appendUnique(columns, r.ranges[0])
	} else {
		for _, name := range r.ordering {
			columns = appendUnique(columns, name)
		}
	}

	columns, unindexable, unknown := filterIndexable(columns, cols)

	// An IS NULL-only statement lands here: no key column, no candidate.
	if len(columns) == 0 {
		switch {
		case unindexable:
			return nil, ReasonUnsupportedType
		case unknown:
			return nil, ReasonUnknownColumn
		default:
			return nil, ReasonNoIndexablePredicate
		}
	}

	columns = truncateAtUnique(columns, r.equality, cols)

	requested := len(columns)
	if len(columns) > b.cfg.MaxIndexColumns {
		columns = columns[:b.cfg.MaxIndexColumns]
	}

	if b.coveredByExisting(key, columns, r.equality, predicate) {
		return nil, ReasonAlreadyIndexed
	}

	return &draft{
		key:          key,
		columns:      columns,
		predicate:    predicate,
		covered:      make(map[string]CoveredQuery),
		statsMissing: statsMissing,
		wideIndex:    requested > len(columns),
		requested:    requested,
	}, ""
}

// selectivePredicate keeps the IS NULL columns whose null_frac says the predicate
// excludes rows, and reports whether one was dropped for want of statistics.
//
// null_frac = 0 is dropped as well: the table holds no NULL there — the column is
// NOT NULL, or the test came from an outer join's null-extension rather than from
// the table — and a partial index over no rows is one the planner cannot use.
func selectivePredicate(nulls []string, cols map[string]Column) (predicate []string, statsMissing bool) {
	for _, name := range nulls {
		c, ok := cols[name]
		if !ok {
			continue
		}

		switch {
		case !c.StatsKnown:
			statsMissing = true
		case c.NullFrac > 0 && c.NullFrac <= nullPredicateMaxFrac:
			predicate = append(predicate, name)
		}
	}

	sort.Strings(predicate)

	return predicate, statsMissing
}

// roles is the column usages of one statement, grouped by what an index could do.
type roles struct {
	equality []string
	ranges   []string
	ordering []string
	nulls    []string
}

// splitRoles turns the column usages of one statement into ordered, deduped
// lists. Ordering columns are dropped when their directions disagree: this step
// only proposes all-ascending keys, and such a key serves ORDER BY only when every
// column sorts the same way (forwards or, scanned backwards, all reversed).
func splitRoles(usages []sqlparse.Usage) roles {
	var (
		r        roles
		orderBy  []sqlparse.Usage
		groupBy  []sqlparse.Usage
		mixedDir bool
	)

	for _, u := range usages {
		switch u.Role {
		case sqlparse.RoleEquality, sqlparse.RoleJoin:
			r.equality = appendUnique(r.equality, u.Column)
		case sqlparse.RoleIsNull:
			r.nulls = appendUnique(r.nulls, u.Column)
		case sqlparse.RoleRange:
			r.ranges = appendUnique(r.ranges, u.Column)
		case sqlparse.RoleOrder:
			orderBy = append(orderBy, u)
		case sqlparse.RoleGroup:
			groupBy = append(groupBy, u)
		}
	}

	// GROUP BY benefits from ordered input the same way ORDER BY does, but an
	// explicit ORDER BY is the stronger signal, so it wins when both are present.
	sortSpec := orderBy
	if len(sortSpec) == 0 {
		sortSpec = groupBy
	}

	slices.SortStableFunc(sortSpec, func(a, c sqlparse.Usage) int { return a.Seq - c.Seq })

	for i, u := range sortSpec {
		if i > 0 && u.Desc != sortSpec[0].Desc {
			mixedDir = true
		}
	}

	if mixedDir {
		return r
	}

	for _, u := range sortSpec {
		r.ordering = appendUnique(r.ordering, u.Column)
	}

	return r
}

// orderEquality puts the most selective column first, which is what makes the
// index reusable by statements filtering on only part of the key.
//
// Without statistics the columns keep the order the statement wrote them and the
// candidate says so: a guessed order is not obviously wrong, but it is not a
// decision anyone made either.
func (b *builder) orderEquality(names []string, cols map[string]Column, rows int64) ([]string, bool) {
	type item struct {
		name     string
		distinct float64
		seq      int
	}

	for _, name := range names {
		if c, known := cols[name]; !known || !c.StatsKnown {
			return slices.Clone(names), true
		}
	}

	items := make([]item, 0, len(names))
	for i, name := range names {
		items = append(items, item{name: name, distinct: distinctCount(cols[name], rows), seq: i})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].distinct != items[j].distinct {
			return items[i].distinct > items[j].distinct
		}

		return items[i].seq < items[j].seq
	})

	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.name)
	}

	return out, false
}

// distinctCount reconciles the two conventions of pg_stats.n_distinct: negative
// is a fraction of the table's rows (-1 means unique), positive is the count
// itself. Comparing the raw values would rank a unique column below a flag.
func distinctCount(c Column, rows int64) float64 {
	if c.NDistinct < 0 {
		return -c.NDistinct * float64(rows)
	}

	return c.NDistinct
}

// filterIndexable drops what cannot be part of a btree key. It reports why, so a
// candidate that loses every column can say whether the type was wrong or the
// column was never in the catalog.
func filterIndexable(columns []string, cols map[string]Column) (kept []string, unindexable, unknown bool) {
	for _, name := range columns {
		c, ok := cols[name]

		switch {
		case !ok:
			unknown = true
		case !c.BtreeIndexable:
			unindexable = true
		default:
			kept = append(kept, name)
		}
	}

	return kept, unindexable, unknown
}

// coveredByExisting reports whether an index already answers the candidate. An
// invalid one does not count: repeating it is the fix, not a duplicate.
func (b *builder) coveredByExisting(key RelKey, columns, equality, predicate []string) bool {
	for _, idx := range b.cat.Indexes[key] {
		if idx.Method != btreeMethod || !idx.Valid || idx.Expression {
			continue
		}

		if holdsSameRows(idx, predicate) && coversKey(idx.Columns, columns, equality) {
			return true
		}
	}

	return false
}

// coversKey compares a candidate's key with an index's: the equality prefix as a
// set (the descent costs the same in any order), the tail position by position.
func coversKey(idxColumns, columns, equality []string) bool {
	if len(idxColumns) < len(columns) {
		return false
	}

	n := 0
	for n < len(columns) && slices.Contains(equality, columns[n]) {
		n++
	}

	if !sameColumns(idxColumns[:n], columns[:n]) {
		return false
	}

	return slices.Equal(idxColumns[n:len(columns)], columns[n:])
}

func sameColumns(a, b []string) bool { return len(a) == len(b) && containsAll(a, b) }

// holdsSameRows: a partial index serves the candidate only with the same predicate.
func holdsSameRows(idx Index, predicate []string) bool {
	if !idx.Partial {
		return true
	}

	return len(idx.NullPredicate) > 0 && slices.Equal(idx.NullPredicate, predicate)
}

// servedByUniqueIndex: equality on the whole key of a unique index already resolves
// the statement, so a wider candidate only moves a filter from the heap into it.
func (b *builder) servedByUniqueIndex(key RelKey, equality, predicate []string) bool {
	for _, idx := range b.cat.Indexes[key] {
		if !idx.Unique || idx.Method != btreeMethod || !idx.Valid || idx.Expression {
			continue
		}

		if !holdsSameRows(idx, predicate) {
			continue
		}

		if len(idx.Columns) > 0 && containsAll(equality, idx.Columns) {
			return true
		}
	}

	return false
}

// similarIndexes names indexes holding every column of the candidate behind other
// columns: they do not serve it, but a rebuild may beat another index.
func (b *builder) similarIndexes(key RelKey, columns []string) []string {
	var out []string

	for _, idx := range b.cat.Indexes[key] {
		if idx.Method != btreeMethod || !idx.Valid || idx.Expression {
			continue
		}

		if containsAll(idx.Columns, columns) {
			out = append(out, idx.Name)
		}
	}

	sort.Strings(out)

	return out
}

func containsAll(haystack, names []string) bool {
	for _, name := range names {
		if !slices.Contains(haystack, name) {
			return false
		}
	}

	return true
}

// truncateAtUnique cuts the key after an equality column the statistics call unique.
func truncateAtUnique(columns, equality []string, cols map[string]Column) []string {
	for i, name := range columns {
		if !slices.Contains(equality, name) {
			continue
		}

		if c, ok := cols[name]; ok && c.StatsKnown && c.NDistinct <= uniqueNDistinct {
			return columns[:i+1]
		}
	}

	return columns
}

// attach records the statement against its candidate, merging into an identical
// one from another statement.
func (b *builder) attach(d *draft, e WorkloadEntry) {
	target := b.find(d.key, d.columns, d.predicate)
	if target == nil {
		b.drafts[d.key] = append(b.drafts[d.key], d)
		target = d
	} else {
		target.statsMissing = target.statsMissing || d.statsMissing
		target.wideIndex = target.wideIndex || d.wideIndex
	}

	if _, seen := target.covered[e.Fingerprint]; seen {
		return
	}

	target.covered[e.Fingerprint] = CoveredQuery{
		QueryIDs:      e.QueryIDs,
		QueryIDByHost: e.QueryIDByHost,
		Fingerprint:   e.Fingerprint,
		Query:         e.Query,
		WeightPct:     b.weight(e),
		Calls:         e.Calls,
		Hosts:         sortedHosts(e.Hosts),
	}
	target.order = append(target.order, e.Fingerprint)
}

func (b *builder) find(key RelKey, columns, predicate []string) *draft {
	for _, d := range b.drafts[key] {
		if slices.Equal(d.columns, columns) && slices.Equal(d.predicate, predicate) {
			return d
		}
	}

	return nil
}

func (b *builder) weight(e WorkloadEntry) float64 {
	if b.totalTime <= 0 {
		return 0
	}

	return 100 * e.TotalTimeMs / b.totalTime
}

func (b *builder) candidates() []Candidate {
	out := make([]Candidate, 0, len(b.drafts))

	for _, key := range sortedKeys(b.drafts) {
		for _, d := range mergePrefixes(b.drafts[key]) {
			out = append(out, b.candidate(key, d))
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].WeightPct != out[j].WeightPct {
			return out[i].WeightPct > out[j].WeightPct
		}

		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}

		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}

		if cmp := slices.Compare(out[i].Columns, out[j].Columns); cmp != 0 {
			return cmp < 0
		}

		return out[i].Predicate < out[j].Predicate
	})

	if len(out) > b.cfg.MaxCandidates {
		out = out[:b.cfg.MaxCandidates]
	}

	return out
}

// mergePrefixes folds a candidate that is a prefix of a longer one on the same
// table into it. Two indexes where one would do is how an advisor makes a table
// slower than it found it.
func mergePrefixes(drafts []*draft) []*draft {
	sorted := slices.Clone(drafts)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].columns) > len(sorted[j].columns)
	})

	kept := make([]*draft, 0, len(sorted))

	for _, d := range sorted {
		merged := false

		for _, k := range kept {
			// Different predicates index different rows: not one index.
			if !slices.Equal(d.predicate, k.predicate) || !isPrefix(d.columns, k.columns) {
				continue
			}

			k.mergeFrom(d)

			merged = true

			break
		}

		if !merged {
			kept = append(kept, d)
		}
	}

	return kept
}

func (d *draft) mergeFrom(other *draft) {
	d.statsMissing = d.statsMissing || other.statsMissing
	d.wideIndex = d.wideIndex || other.wideIndex

	for _, fp := range other.order {
		if _, seen := d.covered[fp]; seen {
			continue
		}

		d.covered[fp] = other.covered[fp]
		d.order = append(d.order, fp)
	}
}

func (b *builder) candidate(key RelKey, d *draft) Candidate {
	rel := b.cat.Relations[key]
	writes := b.cat.Writes[key]

	covered := make([]CoveredQuery, 0, len(d.order))
	weight := 0.0
	readCalls := int64(0)

	for _, fp := range d.order {
		q := d.covered[fp]
		covered = append(covered, q)
		weight += q.WeightPct
		readCalls += q.Calls
	}

	sort.SliceStable(covered, func(i, j int) bool {
		return covered[i].WeightPct > covered[j].WeightPct
	})

	t := trade{writeCalls: b.writeCalls[key], readCalls: readCalls}

	partitions := 0
	if rel.Kind == partitionedKind {
		partitions = b.ddl.leaves(key)
	}

	return Candidate{
		Schema:         key.Schema,
		Table:          key.Name,
		Columns:        d.columns,
		Predicate:      predicateSQL(d.predicate),
		DDL:            ddlFor(key, d.columns, d.predicate, b.ddl),
		WeightPct:      weight,
		Covered:        covered,
		TableRows:      rel.Rows,
		Writes:         writes,
		Warnings:       b.warningsFor(d, rel.Kind, writes, weight, t, partitions),
		PlannerChecked: false,
	}
}

// trade is the write and read calls a candidate is weighed between.
type trade struct {
	writeCalls int64
	readCalls  int64
}

func (b *builder) warningsFor(
	d *draft, kind string, w Writes, weight float64, t trade, partitions int,
) []Warning {
	var out []Warning

	if similar := b.similarIndexes(d.key, d.columns); len(similar) > 0 {
		out = append(out, Warning{Code: WarnSimilarIndex, Params: nil, Names: similar})
	}

	if n := len(b.cat.Indexes[d.key]); n >= manyIndexesThreshold {
		out = append(out, Warning{
			Code:   WarnManyIndexes,
			Params: map[string]float64{ParamIndexes: float64(n)},
			Names:  nil,
		})
	}

	if d.statsMissing {
		out = append(out, Warning{Code: WarnStatsMissing, Params: nil, Names: nil})
	}

	if d.wideIndex {
		out = append(out, Warning{Code: WarnWideIndex, Names: nil, Params: map[string]float64{
			ParamColumns:   float64(len(d.columns)),
			ParamRequested: float64(d.requested),
		}})
	}

	if kind == partitionedKind {
		out = append(out, Warning{Code: WarnPartitionRoot, Names: nil, Params: map[string]float64{
			ParamPartitions: float64(partitions),
		}})
	}

	if kind == matviewKind {
		out = append(out, Warning{Code: WarnMatview, Params: nil, Names: nil})
	}

	if writeHeavy(t) {
		out = append(out, Warning{Code: WarnWriteHeavy, Names: nil, Params: map[string]float64{
			ParamWriteCalls: float64(t.writeCalls),
			ParamReadCalls:  float64(t.readCalls),
		}})
	}

	if weight < lowWeightPct {
		out = append(out, Warning{Code: WarnLowWeight, Names: nil, Params: map[string]float64{
			ParamWeightPct: weight,
		}})
	}

	return out
}

// writeHeavy weighs both sides over the same pg_stat_statements window.
// pg_stat_user_tables cannot: its counters carry the one-time load that filled
// the table, which outweighs every read since.
func writeHeavy(t trade) bool {
	if t.writeCalls < writeHeavyMinCalls {
		return false
	}

	return float64(t.writeCalls) > writeHeavyRatio*float64(max(t.readCalls, 1))
}

// coveredTimePct is the share of analyzed time the candidates touch. Statements
// covered by two candidates count once — the number answers "how much of the load
// is in scope", not "how much would be saved".
func coveredTimePct(candidates []Candidate) float64 {
	seen := make(map[string]bool)
	total := 0.0

	for _, c := range candidates {
		for _, q := range c.Covered {
			if seen[q.Fingerprint] {
				continue
			}

			seen[q.Fingerprint] = true
			total += q.WeightPct
		}
	}

	return total
}

// sortedHosts deduplicates and orders a host list. Hosts are read in parallel, so
// without this the same report would render in a different order on every call.
func sortedHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}

	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = appendUnique(out, h)
	}

	sort.Strings(out)

	return out
}

func appendUnique(names []string, name string) []string {
	if slices.Contains(names, name) {
		return names
	}

	return append(names, name)
}

func isPrefix(short, long []string) bool {
	if len(short) > len(long) {
		return false
	}

	return slices.Equal(long[:len(short)], short)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

func sortedKeys[V any](m map[RelKey]V) []RelKey {
	keys := make([]RelKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Schema != keys[j].Schema {
			return keys[i].Schema < keys[j].Schema
		}

		return keys[i].Name < keys[j].Name
	})

	return keys
}
