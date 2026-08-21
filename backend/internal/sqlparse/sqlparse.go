// Package sqlparse turns the normalized query text of pg_stat_statements into a
// structural description a caller can reason about: which tables the statement
// touches and which columns it filters, joins, sorts and groups by.
//
// It is the only place in the codebase that imports a PostgreSQL parser. That is
// deliberate: the parser is a WASM build of libpg_query, and confining it behind
// the Parser interface keeps the decision replaceable and the domain packages
// free of it.
//
// The package never connects to a database and never executes SQL. Names are
// reported exactly as the statement wrote them — resolving them to catalog
// objects needs a search_path pg_stat_statements does not store, so that job
// belongs to the caller that holds the catalog.
package sqlparse

// Kind is what the statement does. It decides whether the statement can yield
// index candidates at all: a SELECT is read work, an INSERT is only write cost.
type Kind string

const (
	KindSelect  Kind = "select"
	KindInsert  Kind = "insert"
	KindUpdate  Kind = "update"
	KindDelete  Kind = "delete"
	KindMerge   Kind = "merge"
	KindUtility Kind = "utility"
	KindOther   Kind = "other"
)

// Role is the part a column played in the statement. The distinction drives
// column order inside a composite index: equality columns belong in the prefix,
// a range column can only be last among the useful ones.
type Role string

const (
	RoleEquality Role = "equality"
	RoleRange    Role = "range"
	RoleJoin     Role = "join"
	RoleOrder    Role = "order"
	RoleGroup    Role = "group"
	// RoleIsNull is apart from equality: its selectivity is null_frac, not 1/n_distinct.
	RoleIsNull Role = "is_null"
)

// Ref is a table as the statement wrote it, before catalog resolution. Schema is
// empty for an unqualified name; the caller resolves it, because the parser has
// no search_path to resolve it with.
type Ref struct {
	Schema string
	Name   string
	Alias  string
}

// Usage is one column occurrence together with the role it played.
//
// Ref is empty when the column was written unqualified in a statement with more
// than one table in scope: the parser refuses to guess an owner, and the caller
// resolves the column through the catalog (or drops it as ambiguous).
type Usage struct {
	Ref    Ref
	Column string
	Role   Role
	Desc   bool // ORDER BY direction; meaningful only for RoleOrder
	Seq    int  // position within ORDER BY / GROUP BY
}

// Statement is everything an index advisor needs from one query text.
type Statement struct {
	Kind        Kind
	Fingerprint string
	Tables      []Ref
	Usages      []Usage
	Written     []Ref    // targets of INSERT/UPDATE/DELETE/MERGE
	Unsupported []string // reason codes for what was recognized but deliberately skipped
}

// Reason codes. They travel to the UI and to MCP as codes, never as prose: the
// wording lives in the frontend i18n bundle, the same split schemalint uses.
const (
	// ReasonEmpty: the text was blank after trimming.
	ReasonEmpty = "empty"
	// ReasonTooLong: the text exceeded MaxQueryBytes and was not parsed.
	ReasonTooLong = "too_long"
	// ReasonTruncated: pg_stat_statements clipped the text, so the tail is missing.
	ReasonTruncated = "truncated"
	// ReasonInsufficientPrivilege: the text was hidden by pg_stat_statements.
	ReasonInsufficientPrivilege = "insufficient_privilege"
	// ReasonParseError: libpg_query rejected the text.
	ReasonParseError = "parse_error"
	// ReasonExpressionPredicate: the predicate applies a function or a cast to the
	// column, so a plain btree index on that column would not be used.
	ReasonExpressionPredicate = "expression_predicate"
	// ReasonOrPredicate: an OR branch was skipped — its columns only help when
	// every branch is indexed, which this step does not evaluate.
	ReasonOrPredicate = "or_predicate"
)

// Parser turns query text into a Statement. Implementations are safe for
// concurrent use.
type Parser interface {
	Parse(sql string) (Statement, error)
	Fingerprint(sql string) (string, error)
}
