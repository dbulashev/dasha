// Package indexadvisor decides which indexes a database is missing, from the
// workload its pg_stat_statements holds and the catalog it already has.
//
// It performs no I/O: the repository reads the rows, this package reasons about
// them — the same split schemalint and indexadvice use, which is what makes the
// decisions testable without a database.
package indexadvisor

import (
	"slices"

	"github.com/dbulashev/dasha/internal/sqlparse"
)

// RelKey identifies a relation inside one database.
type RelKey struct {
	Schema string
	Name   string
}

func (k RelKey) String() string { return k.Schema + "." + k.Name }

// Relation is a table a candidate could be built on.
type Relation struct {
	RelKey
	// Kind is the relkind: 'r' ordinary, 'p' partitioned, 'm' materialized view.
	Kind string
	// Rows is reltuples, summed over the partitions for a partitioned root — a
	// root's own reltuples is zero, and judging it by that would exempt every
	// partitioned table from the size threshold.
	Rows  int64
	Pages int64
	// Root is the partitioned table this one belongs to, zero when it is not a
	// partition. An index belongs on the root, not on the partition a statement
	// happened to name.
	Root RelKey
	// Parent is the relation this one is a direct partition of, which equals Root
	// only in a single-level tree. The suggested DDL attaches an index level by
	// level, and the levels between a partition and its root are not derivable
	// from Root alone.
	Parent RelKey
}

// IsPartition reports whether the relation rolls up to a partitioned root.
func (r Relation) IsPartition() bool { return r.Root.Name != "" }

// Column carries what the column order of a candidate is decided by.
type Column struct {
	Name     string
	DataType string
	// BtreeIndexable is false for types with no default btree operator class —
	// arrays, json, and the like. A candidate over one of those is not an index.
	BtreeIndexable bool
	// StatsKnown is false when the column has no pg_stats row: never analyzed, or
	// not visible to this role. Then NDistinct and NullFrac mean nothing, and the
	// candidate has to say so rather than order its columns by zeros.
	StatsKnown bool
	NDistinct  float64
	NullFrac   float64
}

// Index is an existing index, as far as a btree prefix comparison cares.
type Index struct {
	Name   string
	Method string
	Unique bool
	// Primary and Valid matter to the duplicate check: an invalid index serves no
	// query, so a candidate that repeats it is not a duplicate.
	Primary bool
	Valid   bool
	// Partial and Expression indexes answer a narrower question than a plain index on
	// the same columns; a partial one still covers a candidate with the same predicate.
	Partial    bool
	Expression bool
	// NullPredicate holds the columns of an "a IS NULL" predicate, sorted; nil otherwise.
	NullPredicate []string
	// Columns are the key columns in order, without the INCLUDE tail — which
	// carries no ordering and so cannot serve a predicate.
	Columns []string
}

// Writes is the cost side of a candidate: every index has to be maintained by
// each of these, and the scan counters say how the table is read today.
type Writes struct {
	Inserted int64
	Updated  int64
	Deleted  int64
	SeqScans int64
	IdxScans int64
}

// Catalog is the state of the database the advisor reasons against.
type Catalog struct {
	Relations map[RelKey]Relation
	Columns   map[RelKey][]Column
	Indexes   map[RelKey][]Index
	Writes    map[RelKey]Writes
	// ByName resolves an unqualified name to the relations that carry it.
	// pg_stat_statements stores no search_path, so a bare "users" may be any
	// schema's; more than one entry means the caller must refuse to guess.
	ByName map[string][]RelKey
	// Truncated says a catalog query hit its row cap. It matters more than it
	// looks: an unread index makes a candidate that duplicates it look new.
	Truncated bool
}

func NewCatalog() Catalog {
	return Catalog{
		Relations: make(map[RelKey]Relation),
		Columns:   make(map[RelKey][]Column),
		Indexes:   make(map[RelKey][]Index),
		Writes:    make(map[RelKey]Writes),
		ByName:    make(map[string][]RelKey),
	}
}

// AddRelation records a relation and keeps ByName in step with it, so the two
// can never disagree about what exists.
func (c *Catalog) AddRelation(r Relation) {
	if _, exists := c.Relations[r.RelKey]; !exists {
		c.ByName[r.Name] = append(c.ByName[r.Name], r.RelKey)
	}

	c.Relations[r.RelKey] = r
}

func (c *Catalog) AddColumn(key RelKey, col Column) {
	c.Columns[key] = append(c.Columns[key], col)
}

func (c *Catalog) AddIndex(key RelKey, idx Index) {
	c.Indexes[key] = append(c.Indexes[key], idx)
}

// AddWrites folds one host's counters into the relation's cluster-wide total.
//
// Merging rather than replacing is what makes a multi-host read correct:
// pg_stat_user_tables is per-instance and is not replicated, so a table read
// entirely on a replica looks untouched on the primary.
func (c *Catalog) AddWrites(key RelKey, w Writes) {
	cur := c.Writes[key]

	cur.Inserted += w.Inserted
	cur.Updated += w.Updated
	cur.Deleted += w.Deleted
	cur.SeqScans += w.SeqScans
	cur.IdxScans += w.IdxScans

	c.Writes[key] = cur
}

// Forget removes every trace of a relation, which is what a row cap leaves the
// caller to do. A relation read in half is worse than one never read: the
// missing half of its index list makes a candidate duplicating an index that
// exists look new, and only dropping the whole relation avoids that.
func (c *Catalog) Forget(key RelKey) {
	delete(c.Relations, key)
	delete(c.Columns, key)
	delete(c.Indexes, key)
	delete(c.Writes, key)

	kept := slices.DeleteFunc(c.ByName[key.Name], func(k RelKey) bool { return k == key })
	if len(kept) == 0 {
		delete(c.ByName, key.Name)

		return
	}

	c.ByName[key.Name] = kept
}

// WorkloadEntry is one pg_stat_statements row, parsed. Several rows collapse into
// one entry by fingerprint later; the collector produces them one to one.
//
// The type is deliberately free of any pg_stat_statements specifics beyond these
// numbers: the same entries can be filled from stored snapshots when the workload
// stops coming from the live view.
type WorkloadEntry struct {
	QueryIDs []int64
	// QueryIDByHost keeps each queryid with the host it was actually read on.
	// QueryIDs and Hosts are two deduplicated lists after folding, and taking one
	// from each is a guess: a cluster whose hosts resolve the same statement to
	// different queryids is exactly where that guess names a pair that exists
	// nowhere, and a deep link built from it opens an empty report.
	QueryIDByHost map[string]int64
	Fingerprint   string
	Query         string // sanitized, safe to return to a client
	Calls         int64
	TotalTimeMs   float64
	Rows          int64
	Stmt          sqlparse.Statement
	// Hosts are the instances of the cluster this statement was read from. It is
	// a list because the same statement usually runs on several of them, and
	// which ones is the answer to "who would this index actually serve".
	Hosts []string
}

// Workload is everything the collector managed to read and parse.
type Workload struct {
	Entries []WorkloadEntry
	// NotParsed counts what did not become an entry, by reason code. An empty
	// candidate list next to a full NotParsed does not mean the schema is fine,
	// and the report has to be able to say so.
	NotParsed map[string]int
	// Collected is how many rows pg_stat_statements returned, parsed or not.
	Collected int
	// Available is false when pg_stat_statements could not be read at all — a
	// different statement from "read it, found nothing".
	Available bool
	// Hosts are the instances whose pg_stat_statements went into this workload.
	Hosts []string
	// NoStats are the instances that answered but carry no readable
	// pg_stat_statements. Their load is invisible here, which is not the same as
	// them being idle — on a replica serving all the reads it is the opposite.
	NoStats []string
	// Unreachable are the instances that could not be read. They are reported,
	// never dropped: pg_stat_statements is not replicated, so a statement absent
	// from the hosts that answered may be the whole load on the one that did not,
	// and the missing index for it would silently never be proposed.
	Unreachable []string
}

// CountNotParsed tallies one statement the collector could not use.
func (w *Workload) CountNotParsed(reason string) {
	if w.NotParsed == nil {
		w.NotParsed = make(map[string]int)
	}

	w.NotParsed[reason]++
}

// Merge folds one host's workload into the cluster-wide one.
//
// Entries are appended rather than combined here; folding them is collapse's job,
// which already does it by fingerprint and so cannot tell a second host apart from
// a second row on the same one. Available is an OR: one host with the extension
// makes the report possible, and the hosts without it are not a failure to report.
func (w *Workload) Merge(other Workload) {
	w.Entries = append(w.Entries, other.Entries...)
	w.Collected += other.Collected
	w.Available = w.Available || other.Available
	w.Hosts = append(w.Hosts, other.Hosts...)
	w.NoStats = append(w.NoStats, other.NoStats...)
	w.Unreachable = append(w.Unreachable, other.Unreachable...)

	for code, n := range other.NotParsed {
		if w.NotParsed == nil {
			w.NotParsed = make(map[string]int, len(other.NotParsed))
		}

		w.NotParsed[code] += n
	}
}
