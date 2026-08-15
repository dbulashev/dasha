// Package indexadvisor decides which indexes a database is missing, from the
// workload its pg_stat_statements holds and the catalog it already has.
//
// It performs no I/O: the repository reads the rows, this package reasons about
// them — the same split schemalint and indexadvice use, which is what makes the
// decisions testable without a database.
package indexadvisor

import "github.com/dbulashev/dasha/internal/sqlparse"

// RelKey identifies a relation inside one database.
type RelKey struct {
	Schema string
	Name   string
}

func (k RelKey) String() string { return k.Schema + "." + k.Name }

// Relation is a table a candidate could be built on.
type Relation struct {
	RelKey
	// Kind is the relkind: 'r' for an ordinary table, 'p' for a partitioned one.
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
	// Partial and Expression indexes never count as covering a candidate: they
	// answer a narrower question than a plain index on the same columns.
	Partial    bool
	Expression bool
	// Columns are the key columns in order, without the INCLUDE tail — which
	// carries no ordering and so cannot serve a predicate.
	Columns []string
}

// Writes is the cost side of a candidate: every index has to be maintained by
// each of these, and SeqScans/IdxScans/LiveTuples are what the indexes/missing
// signal reads, so a candidate can say when it is talking about the same table.
type Writes struct {
	Inserted   int64
	Updated    int64
	Deleted    int64
	SeqScans   int64
	IdxScans   int64
	LiveTuples int64
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

func (c *Catalog) SetWrites(key RelKey, w Writes) {
	c.Writes[key] = w
}

// WorkloadEntry is one pg_stat_statements row, parsed. Several rows collapse into
// one entry by fingerprint later; the collector produces them one to one.
//
// The type is deliberately free of any pg_stat_statements specifics beyond these
// numbers: the same entries can be filled from stored snapshots when the workload
// stops coming from the live view.
type WorkloadEntry struct {
	QueryIDs    []int64
	Fingerprint string
	Query       string // sanitized, safe to return to a client
	Calls       int64
	TotalTimeMs float64
	Rows        int64
	Stmt        sqlparse.Statement
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
}

// CountNotParsed tallies one statement the collector could not use.
func (w *Workload) CountNotParsed(reason string) {
	if w.NotParsed == nil {
		w.NotParsed = make(map[string]int)
	}

	w.NotParsed[reason]++
}
