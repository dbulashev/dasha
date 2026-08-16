package indexadvisor

import (
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
	// The indexes/missing signal's own thresholds, repeated so a candidate can
	// tell when the two signals are about the same table.
	missingSignalIdxPct  = 95.0
	missingSignalMinRows = 10000
	// btreeMethod is the only access method this step proposes or compares against.
	btreeMethod = "btree"
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
			PgssAvailable:    w.Available,
			AnalyzedQueries:  len(w.Entries),
			CollapsedGroups:  len(groups),
			NotParsedCount:   sumCounts(b.skipped),
			CoveredTimePct:   coveredTimePct(candidates),
			CatalogTruncated: cat.Truncated,
		},
	}

	rep.DurationMs = time.Since(started).Milliseconds()

	return rep
}

// collapse folds entries sharing a fingerprint into one unit of work. Two things
// depend on it: a statement written twice with different constants must weigh
// once, and pgpro_stats keys its view by planid as well, so the same statement
// arrives as several rows there.
func collapse(entries []WorkloadEntry) []WorkloadEntry {
	out := make([]WorkloadEntry, 0, len(entries))
	index := make(map[string]int, len(entries))

	for _, e := range entries {
		i, ok := index[e.Fingerprint]
		if !ok || e.Fingerprint == "" {
			e.QueryIDs = slices.Clone(e.QueryIDs)
			index[e.Fingerprint] = len(out)

			out = append(out, e)

			continue
		}

		g := &out[i]
		g.QueryIDs = append(g.QueryIDs, e.QueryIDs...)
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
	key          RelKey
	columns      []string
	covered      map[string]CoveredQuery
	order        []string // fingerprints in insertion order, for a stable output
	statsMissing bool
	wideIndex    bool
	requested    int // columns the statements asked for, before the width cap
}

type builder struct {
	cat       Catalog
	cfg       Config
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
	eq, rng, ordering := splitRoles(usages)

	columns, statsMissing := b.orderEquality(eq, cols, rel.Rows)

	// One range column at most, and everything after it in the key is unordered
	// for the scan — which is why the ordering columns are only worth adding when
	// there is no range predicate at all.
	if len(rng) > 0 {
		columns = appendUnique(columns, rng[0])
	} else {
		for _, name := range ordering {
			columns = appendUnique(columns, name)
		}
	}

	columns, unindexable, unknown := filterIndexable(columns, cols)
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

	requested := len(columns)
	if len(columns) > b.cfg.MaxIndexColumns {
		columns = columns[:b.cfg.MaxIndexColumns]
	}

	if b.coveredByExisting(key, columns) {
		return nil, ReasonAlreadyIndexed
	}

	return &draft{
		key:          key,
		columns:      columns,
		covered:      make(map[string]CoveredQuery),
		statsMissing: statsMissing,
		wideIndex:    requested > len(columns),
		requested:    requested,
	}, ""
}

// splitRoles turns the column usages of one statement into three ordered, deduped
// lists. Ordering columns are dropped when their directions disagree: this step
// only proposes all-ascending keys, and such a key serves ORDER BY only when every
// column sorts the same way (forwards or, scanned backwards, all reversed).
func splitRoles(usages []sqlparse.Usage) (equality, ranges, ordering []string) {
	var (
		orderBy  []sqlparse.Usage
		groupBy  []sqlparse.Usage
		mixedDir bool
	)

	for _, u := range usages {
		switch u.Role {
		case sqlparse.RoleEquality, sqlparse.RoleJoin:
			equality = appendUnique(equality, u.Column)
		case sqlparse.RoleRange:
			ranges = appendUnique(ranges, u.Column)
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
		return equality, ranges, nil
	}

	for _, u := range sortSpec {
		ordering = appendUnique(ordering, u.Column)
	}

	return equality, ranges, ordering
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

	items := make([]item, 0, len(names))
	statsMissing := false

	for i, name := range names {
		c, known := cols[name]
		if !known || !c.StatsKnown {
			statsMissing = true
		}

		items = append(items, item{name: name, distinct: distinctCount(c, rows), seq: i})
	}

	if statsMissing {
		return slices.Clone(names), true
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

// coveredByExisting reports whether an index already answers the candidate.
//
// Partial and expression indexes never count: the first answers a narrower
// question, the second a different one. An invalid index does not count either —
// it serves no query, so a candidate repeating it is not a duplicate but the fix.
func (b *builder) coveredByExisting(key RelKey, columns []string) bool {
	for _, idx := range b.cat.Indexes[key] {
		if idx.Method != btreeMethod || !idx.Valid || idx.Partial || idx.Expression {
			continue
		}

		if len(idx.Columns) < len(columns) {
			continue
		}

		if slices.Equal(idx.Columns[:len(columns)], columns) {
			return true
		}
	}

	return false
}

// attach records the statement against its candidate, merging into an identical
// one from another statement.
func (b *builder) attach(d *draft, e WorkloadEntry) {
	target := b.find(d.key, d.columns)
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
		QueryIDs:    e.QueryIDs,
		Fingerprint: e.Fingerprint,
		Query:       e.Query,
		WeightPct:   b.weight(e),
		Calls:       e.Calls,
	}
	target.order = append(target.order, e.Fingerprint)
}

func (b *builder) find(key RelKey, columns []string) *draft {
	for _, d := range b.drafts[key] {
		if slices.Equal(d.columns, columns) {
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

		return slices.Compare(out[i].Columns, out[j].Columns) < 0
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
			if !isPrefix(d.columns, k.columns) {
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

	partitioned := rel.Kind == partitionedKind
	t := trade{writeCalls: b.writeCalls[key], readCalls: readCalls}

	return Candidate{
		Schema:         key.Schema,
		Table:          key.Name,
		Columns:        d.columns,
		DDL:            ddlFor(key, d.columns, partitioned),
		WeightPct:      weight,
		Covered:        covered,
		TableRows:      rel.Rows,
		Writes:         writes,
		Warnings:       warningsFor(d, rel.Kind, writes, weight, t),
		PlannerChecked: false,
	}
}

// trade is the write and read calls a candidate is weighed between.
type trade struct {
	writeCalls int64
	readCalls  int64
}

func warningsFor(d *draft, kind string, w Writes, weight float64, t trade) []Warning {
	var out []Warning

	if d.statsMissing {
		out = append(out, Warning{Code: WarnStatsMissing, Params: nil})
	}

	if d.wideIndex {
		out = append(out, Warning{Code: WarnWideIndex, Params: map[string]float64{
			ParamColumns:   float64(len(d.columns)),
			ParamRequested: float64(d.requested),
		}})
	}

	if kind == partitionedKind {
		out = append(out, Warning{Code: WarnPartitionRoot, Params: nil})
	}

	if kind == matviewKind {
		out = append(out, Warning{Code: WarnMatview, Params: nil})
	}

	if writeHeavy(t) {
		out = append(out, Warning{Code: WarnWriteHeavy, Params: map[string]float64{
			ParamWriteCalls: float64(t.writeCalls),
			ParamReadCalls:  float64(t.readCalls),
		}})
	}

	if weight < lowWeightPct {
		out = append(out, Warning{Code: WarnLowWeight, Params: map[string]float64{
			ParamWeightPct: weight,
		}})
	}

	if matchesMissingSignal(w) {
		out = append(out, Warning{Code: WarnOverlapsMissingSignal, Params: map[string]float64{
			ParamIdxScanPct: idxScanPct(w),
			ParamRows:       float64(w.LiveTuples),
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

// matchesMissingSignal repeats the thresholds of the indexes/missing report, so
// that a candidate can point out when the two are talking about the same table
// instead of looking like independent evidence.
func matchesMissingSignal(w Writes) bool {
	if w.IdxScans <= 0 || w.LiveTuples < missingSignalMinRows {
		return false
	}

	return idxScanPct(w) < missingSignalIdxPct
}

func idxScanPct(w Writes) float64 {
	scans := w.SeqScans + w.IdxScans
	if scans == 0 {
		return 0
	}

	return 100 * float64(w.IdxScans) / float64(scans)
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
