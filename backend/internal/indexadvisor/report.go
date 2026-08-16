package indexadvisor

import (
	"sort"
	"strings"
)

// Warning codes. Every one of them is a reason a candidate might be a bad idea
// despite ranking well — the prose lives in the frontend i18n bundle and in the
// MCP tool description, never here.
const (
	// WarnWriteHeavy: the table is written far more than it is read, so a new
	// index may cost more than the reads it saves.
	WarnWriteHeavy = "write_heavy"
	// WarnLowWeight: the covered statements are a marginal share of the load.
	WarnLowWeight = "low_weight"
	// WarnPartitionRoot: the candidate is on a partitioned table, where the DDL
	// cannot use CONCURRENTLY and every partition pays for the index.
	WarnPartitionRoot = "partition_root"
	// WarnStatsMissing: no pg_stats row for the columns, so their order in the
	// key is the order the statement wrote them, not the selective one.
	WarnStatsMissing = "stats_missing"
	// WarnWideIndex: the statement asked for more columns than the key may hold.
	WarnWideIndex = "wide_index"
	// WarnMatview: a plain REFRESH rebuilds every index on a materialized view,
	// while REFRESH CONCURRENTLY needs a unique one to exist at all.
	WarnMatview = "matview"
)

// Params keys. They are passed to i18n as-is, so they are part of the contract
// with the locale files.
const (
	ParamWriteCalls = "write_calls"
	ParamReadCalls  = "read_calls"
	ParamWeightPct  = "weight_pct"
	ParamColumns    = "columns"
	ParamRequested  = "requested"
)

// Reasons a statement contributed no candidate. The collector's own codes
// (truncated, parse_error, ...) come from sqlparse and share the tally.
const (
	// ReasonUnknownRelation: the statement names a table the catalog does not have.
	ReasonUnknownRelation = "unknown_relation"
	// ReasonSystemRelation: a system catalog or a monitoring view, not an
	// application table. Dasha's own polling lands here.
	ReasonSystemRelation = "system_relation"
	// ReasonAmbiguousName: an unqualified name several schemas answer to. Refusing
	// beats guessing — pg_stat_statements does not record a search_path.
	ReasonAmbiguousName = "ambiguous_name"
	// ReasonAmbiguousColumn: a bare column more than one table in the statement has.
	ReasonAmbiguousColumn = "ambiguous_column"
	// ReasonUnknownColumn: a bare column none of the statement's tables has.
	ReasonUnknownColumn = "unknown_column"
	// ReasonUnsupportedType: every candidate column is of a type with no btree
	// operator class.
	ReasonUnsupportedType = "unsupported_type"
	// ReasonTableTooSmall: below min_table_rows, where a sequential scan is fine.
	ReasonTableTooSmall = "table_too_small"
	// ReasonAlreadyIndexed: an existing index already covers what the statement
	// filters on. The healthy outcome, and the one that explains an empty list.
	ReasonAlreadyIndexed = "already_indexed"
	// ReasonNoIndexablePredicate: parsed, resolved, and simply has nothing an
	// index would help with.
	ReasonNoIndexablePredicate = "no_indexable_predicate"
)

// Warning is a caveat on a candidate: a code plus the numbers its phrasing quotes.
type Warning struct {
	Code   string
	Params map[string]float64
}

// CoveredQuery is one statement a candidate would serve. It carries enough for
// the user to check the recommendation against the query itself.
type CoveredQuery struct {
	QueryIDs    []int64
	Fingerprint string
	Query       string // sanitized
	WeightPct   float64
	Calls       int64
	// Hosts are the instances the statement was actually seen on. A candidate
	// whose statements run only on the replicas is still worth creating — the
	// index is physically replicated — but the user has to be able to see that
	// the primary is not where the win lands.
	Hosts []string
}

// Candidate is one proposed index.
type Candidate struct {
	Schema  string
	Table   string
	Columns []string
	// DDL is a suggestion for the user to run. Dasha never executes DDL.
	DDL string
	// WeightPct is the share of the analyzed execution time spent in the
	// statements this candidate covers. It is not a predicted gain: nothing here
	// asked the planner whether the index would even be used.
	WeightPct float64
	Covered   []CoveredQuery
	TableRows int64
	Writes    Writes
	Warnings  []Warning
	// PlannerChecked stays false through step 1 and is the flag the UI and MCP
	// must not hide: the whole report is a heuristic until the planner sees it.
	PlannerChecked bool
}

// NotParsed is one reason some of the workload produced nothing, with its count.
type NotParsed struct {
	ReasonCode string
	Count      int
}

// Summary is what the section shows above the table, so that an empty candidate
// list cannot be read as "everything is fine".
type Summary struct {
	PgssAvailable   bool
	AnalyzedQueries int
	CollapsedGroups int
	NotParsedCount  int
	// CoveredTimePct is the share of analyzed time the emitted candidates touch.
	CoveredTimePct float64
	// CatalogTruncated says the catalog was read only in part, which can make a
	// candidate that duplicates an unread index look new.
	CatalogTruncated bool
	// Hosts are the instances whose workload this report was built from, sorted.
	Hosts []string
	// HostsWithoutStats are the instances that answered but have no readable
	// pg_stat_statements, so nothing they run reached this report.
	HostsWithoutStats []string
}

// Report is the whole answer for one database, over every host of the cluster.
type Report struct {
	Candidates []Candidate
	NotParsed  []NotParsed
	Summary    Summary
	// UnreachableHosts are the instances that could not be read. They matter to
	// how the report should be read: pg_stat_statements is per-instance, so an
	// unread host is workload the advisor never saw, and the candidate list is
	// incomplete by exactly that much rather than merely shorter.
	UnreachableHosts []string
	DurationMs       int64
}

// ddlFor renders the statement the user is invited to run.
//
// CONCURRENTLY is the default because the alternative locks the table against
// writes for the duration of the build. It is dropped for a partitioned table:
// PostgreSQL rejects CREATE INDEX CONCURRENTLY there outright, and a DDL that
// cannot run is worse than a slower one — the partition_root warning is what
// explains the difference.
func ddlFor(key RelKey, columns []string, partitioned bool) string {
	quoted := make([]string, 0, len(columns))
	for _, c := range columns {
		quoted = append(quoted, quoteIdent(c))
	}

	concurrently := " CONCURRENTLY"
	if partitioned {
		concurrently = ""
	}

	return "CREATE INDEX" + concurrently + " ON " +
		quoteIdent(key.Schema) + "." + quoteIdent(key.Name) +
		" (" + strings.Join(quoted, ", ") + ");"
}

// quoteIdent always quotes. An unquoted identifier would be folded to lower case
// and would break on a keyword, and this string is handed to a human to run
// against production — pg_dump quotes everything for the same reason.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// notParsedList turns the tally into a stable, sorted list: most frequent first,
// then by code, so the same report never renders in two different orders.
func notParsedList(counts map[string]int) []NotParsed {
	if len(counts) == 0 {
		return nil
	}

	out := make([]NotParsed, 0, len(counts))
	for code, n := range counts {
		out = append(out, NotParsed{ReasonCode: code, Count: n})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}

		return out[i].ReasonCode < out[j].ReasonCode
	})

	return out
}
