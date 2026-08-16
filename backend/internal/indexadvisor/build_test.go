package indexadvisor

import (
	"slices"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/internal/sqlparse"
)

// The fixtures run real SQL through the real parser rather than hand-built
// Statements. It costs a WASM module per test binary and buys the thing that
// matters: the two packages are checked against each other, not against what
// this file assumes the parser emits.
var testParser = sqlparse.New(sqlparse.Config{}) //nolint:gochecknoglobals // one WASM module per test binary

const (
	testSchema = "public"
	testRows   = 100_000
)

func entry(t *testing.T, id int64, sql string, timeMs float64) WorkloadEntry {
	t.Helper()

	return entryCalls(t, id, sql, timeMs, 100)
}

func entryCalls(t *testing.T, id int64, sql string, timeMs float64, calls int64) WorkloadEntry {
	t.Helper()

	stmt, err := testParser.Parse(sql)
	if err != nil {
		t.Fatalf("parse %q: %v", sql, err)
	}

	return WorkloadEntry{
		QueryIDs:    []int64{id},
		Fingerprint: stmt.Fingerprint,
		Query:       sql,
		Calls:       calls,
		TotalTimeMs: timeMs,
		Rows:        100,
		Stmt:        stmt,
	}
}

// entryOn is the same statement as it arrives from one host of the cluster: the
// text and the queryid are identical everywhere, only the counters and the host
// differ.
func entryOn(t *testing.T, host string, id int64, sql string, timeMs float64) WorkloadEntry {
	t.Helper()

	e := entry(t, id, sql, timeMs)
	e.Hosts = []string{host}

	return e
}

func workloadOf(entries ...WorkloadEntry) Workload {
	return Workload{
		Entries:   entries,
		NotParsed: nil,
		Collected: len(entries),
		Available: true,
	}
}

func col(name, dataType string, nDistinct float64) Column {
	return Column{
		Name:           name,
		DataType:       dataType,
		BtreeIndexable: dataType != "json",
		StatsKnown:     true,
		NDistinct:      nDistinct,
		NullFrac:       0,
	}
}

// ordersCatalog is one busy table with a primary key and nothing else indexed.
// n_distinct is set so the columns rank unambiguously: created_at (90 000
// distinct) > customer_id (5 000) > tenant_id (50) > status (4).
func ordersCatalog() Catalog {
	cat := NewCatalog()
	orders := RelKey{Schema: testSchema, Name: "orders"}

	cat.AddRelation(Relation{RelKey: orders, Kind: "r", Rows: testRows, Pages: 5000, Root: RelKey{}})

	for _, c := range []Column{
		col("id", "bigint", -1), // unique
		col("tenant_id", "integer", 50),
		col("customer_id", "integer", 5000),
		col("status", "character varying(32)", 4),
		col("created_at", "timestamp with time zone", -0.9),
		col("payload", "json", 0),
	} {
		cat.AddColumn(orders, c)
	}

	cat.AddIndex(orders, Index{
		Name: "orders_pkey", Method: "btree", Unique: true, Primary: true,
		Valid: true, Partial: false, Expression: false, Columns: []string{"id"},
	})

	cat.AddWrites(orders, Writes{
		Inserted: 1000, Updated: 100, Deleted: 0,
		SeqScans: 500, IdxScans: 5000,
	})

	return cat
}

func onlyCandidate(t *testing.T, rep Report) Candidate {
	t.Helper()

	if len(rep.Candidates) != 1 {
		t.Fatalf("want exactly one candidate, got %d: %+v", len(rep.Candidates), rep.Candidates)
	}

	return rep.Candidates[0]
}

func reasonCount(rep Report, code string) int {
	for _, n := range rep.NotParsed {
		if n.ReasonCode == code {
			return n.Count
		}
	}

	return 0
}

func hasWarning(c Candidate, code string) bool { return warningOf(c, code).Code != "" }

func warningOf(c Candidate, code string) Warning {
	for _, w := range c.Warnings {
		if w.Code == code {
			return w
		}
	}

	return Warning{Code: "", Params: nil}
}

func TestBuildOrdersEqualityColumnsBySelectivity(t *testing.T) {
	w := workloadOf(entry(t, 1,
		`SELECT id FROM orders WHERE tenant_id = $1 AND customer_id = $2`, 1000))

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct
	cand := onlyCandidate(t, rep)

	want := []string{"customer_id", "tenant_id"}
	if !slices.Equal(cand.Columns, want) {
		t.Errorf("columns = %v, want %v — the more selective column belongs first", cand.Columns, want)
	}

	if cand.PlannerChecked {
		t.Error("step 1 never asks the planner; the flag must stay false")
	}

	if !strings.Contains(cand.DDL, "CREATE INDEX CONCURRENTLY") {
		t.Errorf("DDL = %q, want a CONCURRENTLY build for an ordinary table", cand.DDL)
	}

	if !strings.Contains(cand.DDL, `"public"."orders" ("customer_id", "tenant_id")`) {
		t.Errorf("DDL = %q, want quoted identifiers in key order", cand.DDL)
	}

	if cand.WeightPct != 100 {
		t.Errorf("WeightPct = %v, want 100 — it is the only statement", cand.WeightPct)
	}
}

// Without statistics the order is the one the statement wrote, and the candidate
// has to admit it rather than present a guess as a decision.
func TestBuildKeepsStatementOrderWithoutStats(t *testing.T) {
	cat := ordersCatalog()
	orders := RelKey{Schema: testSchema, Name: "orders"}

	for i, c := range cat.Columns[orders] {
		if c.Name == "customer_id" {
			cat.Columns[orders][i].StatsKnown = false
		}
	}

	w := workloadOf(entry(t, 1,
		`SELECT id FROM orders WHERE tenant_id = $1 AND customer_id = $2`, 1000))

	cand := onlyCandidate(t, Build(w, cat, Config{})) //nolint:exhaustruct

	want := []string{"tenant_id", "customer_id"}
	if !slices.Equal(cand.Columns, want) {
		t.Errorf("columns = %v, want statement order %v", cand.Columns, want)
	}

	if !hasWarning(cand, WarnStatsMissing) {
		t.Error("a candidate ordered without statistics must carry stats_missing")
	}
}

func TestBuildSkipsWhatAnExistingIndexAlreadyCovers(t *testing.T) {
	w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE id = $1`, 1000))

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 0 {
		t.Errorf("the primary key already serves this: %+v", rep.Candidates)
	}

	if got := reasonCount(rep, ReasonAlreadyIndexed); got != 1 {
		t.Errorf("already_indexed = %d, want 1 — an empty list must say why", got)
	}
}

// A partial index answers a narrower question, so it cannot stand in for a plain
// one. Missing this is how an advisor goes quiet on a table that needs an index.
func TestBuildPartialAndInvalidIndexesDoNotCover(t *testing.T) {
	cases := []struct {
		name  string
		index Index
	}{
		{
			name: "partial",
			index: Index{
				Name: "orders_open_idx", Method: "btree", Valid: true, Partial: true,
				Columns: []string{"status"}, Unique: false, Primary: false, Expression: false,
			},
		},
		{
			name: "invalid",
			index: Index{
				Name: "orders_status_idx", Method: "btree", Valid: false, Partial: false,
				Columns: []string{"status"}, Unique: false, Primary: false, Expression: false,
			},
		},
		{
			name: "not a btree",
			index: Index{
				Name: "orders_status_hash", Method: "hash", Valid: true, Partial: false,
				Columns: []string{"status"}, Unique: false, Primary: false, Expression: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := ordersCatalog()
			cat.AddIndex(RelKey{Schema: testSchema, Name: "orders"}, tc.index)

			w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE status = $1`, 1000))

			cand := onlyCandidate(t, Build(w, cat, Config{})) //nolint:exhaustruct
			if !slices.Equal(cand.Columns, []string{"status"}) {
				t.Errorf("columns = %v, want [status]", cand.Columns)
			}
		})
	}
}

func TestBuildMergesPrefixCandidates(t *testing.T) {
	w := workloadOf(
		entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 400),
		entry(t, 2, `SELECT * FROM orders WHERE tenant_id = $1 AND created_at > $2`, 600),
	)

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct
	cand := onlyCandidate(t, rep)

	want := []string{"tenant_id", "created_at"}
	if !slices.Equal(cand.Columns, want) {
		t.Errorf("columns = %v, want %v — the shorter candidate folds into the longer", cand.Columns, want)
	}

	if len(cand.Covered) != 2 {
		t.Fatalf("covered %d statements, want both", len(cand.Covered))
	}

	if cand.WeightPct != 100 {
		t.Errorf("WeightPct = %v, want the sum of both weights", cand.WeightPct)
	}

	if rep.Summary.CoveredTimePct != 100 {
		t.Errorf("CoveredTimePct = %v, want 100", rep.Summary.CoveredTimePct)
	}
}

// After a range column the index is no longer ordered, so ORDER BY columns can
// only be appended when there is no range predicate to begin with.
func TestBuildPlacesRangeAndOrderingColumns(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "order by follows equality",
			sql:  `SELECT * FROM orders WHERE tenant_id = $1 ORDER BY created_at DESC`,
			want: []string{"tenant_id", "created_at"},
		},
		{
			name: "range wins over ordering",
			sql:  `SELECT * FROM orders WHERE tenant_id = $1 AND customer_id > $2 ORDER BY created_at DESC`,
			want: []string{"tenant_id", "customer_id"},
		},
		{
			name: "group by orders the key like order by",
			sql:  `SELECT status, count(*) FROM orders WHERE tenant_id = $1 GROUP BY status`,
			want: []string{"tenant_id", "status"},
		},
		{
			name: "mixed sort directions are not served by an ascending key",
			sql:  `SELECT * FROM orders WHERE tenant_id = $1 ORDER BY customer_id ASC, created_at DESC`,
			want: []string{"tenant_id"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := workloadOf(entry(t, 1, tc.sql, 1000))

			cand := onlyCandidate(t, Build(w, ordersCatalog(), Config{})) //nolint:exhaustruct
			if !slices.Equal(cand.Columns, tc.want) {
				t.Errorf("columns = %v, want %v", cand.Columns, tc.want)
			}
		})
	}
}

func TestBuildCapsIndexWidth(t *testing.T) {
	w := workloadOf(entry(t, 1,
		`SELECT * FROM orders
		 WHERE tenant_id = $1 AND customer_id = $2 AND status = $3 AND created_at = $4`, 1000))

	cand := onlyCandidate(t, Build(w, ordersCatalog(), Config{})) //nolint:exhaustruct

	if len(cand.Columns) != DefaultMaxIndexColumns {
		t.Errorf("columns = %v, want %d of them", cand.Columns, DefaultMaxIndexColumns)
	}

	if !hasWarning(cand, WarnWideIndex) {
		t.Error("a truncated key must carry wide_index")
	}
}

func TestBuildSkipsSmallTables(t *testing.T) {
	cat := ordersCatalog()
	orders := RelKey{Schema: testSchema, Name: "orders"}

	rel := cat.Relations[orders]
	rel.Rows = 100
	cat.Relations[orders] = rel

	w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000))

	rep := Build(w, cat, Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 0 {
		t.Errorf("a table of 100 rows needs no index: %+v", rep.Candidates)
	}

	if got := reasonCount(rep, ReasonTableTooSmall); got != 1 {
		t.Errorf("table_too_small = %d, want 1", got)
	}
}

func TestBuildRollsPartitionUpToItsRoot(t *testing.T) {
	cat := ordersCatalog()
	root := RelKey{Schema: testSchema, Name: "events"}
	part := RelKey{Schema: testSchema, Name: "events_01"}

	cat.AddRelation(Relation{RelKey: root, Kind: "p", Rows: testRows, Pages: 0, Root: RelKey{}, Parent: RelKey{}})
	cat.AddRelation(Relation{RelKey: part, Kind: "r", Rows: testRows / 2, Pages: 100, Root: root, Parent: root})

	for _, key := range []RelKey{root, part} {
		cat.AddColumn(key, col("tenant_id", "integer", 50))
		cat.AddColumn(key, col("at", "date", 400))
	}

	w := workloadOf(entry(t, 1, `SELECT * FROM events_01 WHERE tenant_id = $1`, 1000))

	cand := onlyCandidate(t, Build(w, cat, Config{})) //nolint:exhaustruct

	if cand.Table != "events" {
		t.Errorf("candidate on %q, want the partitioned root", cand.Table)
	}

	if !hasWarning(cand, WarnPartitionRoot) {
		t.Error("a candidate on a partitioned root must carry partition_root")
	}

	// PostgreSQL rejects CREATE INDEX CONCURRENTLY on a partitioned table, so the
	// root index is built invalid and the partitions are attached to it — the
	// shape of the script is ddl_test's subject, here it only has to be that one.
	if !strings.Contains(cand.DDL, `CREATE INDEX "events_tenant_id_idx" ON ONLY "public"."events"`) {
		t.Errorf("DDL = %q, want an ON ONLY root index", cand.DDL)
	}

	if !strings.Contains(cand.DDL, `ATTACH PARTITION "public"."events_01_tenant_id_idx"`) {
		t.Errorf("DDL = %q, want the partition attached", cand.DDL)
	}

	if got := warningOf(cand, WarnPartitionRoot).Params[ParamPartitions]; got != 1 {
		t.Errorf("partition_root counts %v partitions, want 1", got)
	}
}

// pgpro_stats keys its view by planid as well, so one statement arrives as
// several rows. They must weigh once.
func TestBuildCollapsesRowsSharingAFingerprint(t *testing.T) {
	first := entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 600)
	second := entry(t, 2, `SELECT * FROM orders WHERE tenant_id = $1`, 400)

	rep := Build(workloadOf(first, second), ordersCatalog(), Config{}) //nolint:exhaustruct

	if rep.Summary.CollapsedGroups != 1 {
		t.Errorf("CollapsedGroups = %d, want 1", rep.Summary.CollapsedGroups)
	}

	cand := onlyCandidate(t, rep)
	if len(cand.Covered) != 1 {
		t.Fatalf("covered = %d entries, want one collapsed unit", len(cand.Covered))
	}

	if got := cand.Covered[0].QueryIDs; !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("QueryIDs = %v, want both rows behind the unit", got)
	}

	if cand.Covered[0].Calls != 200 {
		t.Errorf("Calls = %d, want the sum of both rows", cand.Covered[0].Calls)
	}
}

func TestBuildRefusesAmbiguousNames(t *testing.T) {
	cat := ordersCatalog()
	other := RelKey{Schema: "billing", Name: "orders"}

	cat.AddRelation(Relation{RelKey: other, Kind: "r", Rows: testRows, Pages: 10, Root: RelKey{}})
	cat.AddColumn(other, col("tenant_id", "integer", 50))

	w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000))

	rep := Build(w, cat, Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 0 {
		t.Errorf("two schemas answer to orders; guessing one is worse than refusing: %+v", rep.Candidates)
	}

	if got := reasonCount(rep, ReasonAmbiguousName); got != 1 {
		t.Errorf("ambiguous_name = %d, want 1", got)
	}
}

// A bare column in a join is the parser's open question; the catalog answers it
// when exactly one of the tables has that column.
func TestBuildResolvesBareColumnsThroughTheCatalog(t *testing.T) {
	cat := ordersCatalog()
	customers := RelKey{Schema: testSchema, Name: "customers"}

	cat.AddRelation(Relation{RelKey: customers, Kind: "r", Rows: testRows, Pages: 100, Root: RelKey{}})
	cat.AddColumn(customers, col("id", "integer", -1))
	cat.AddColumn(customers, col("country", "text", 200))

	w := workloadOf(entry(t, 1,
		`SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id WHERE country = $1`, 1000))

	rep := Build(w, cat, Config{}) //nolint:exhaustruct

	var found bool

	for _, cand := range rep.Candidates {
		if cand.Table == "customers" && slices.Contains(cand.Columns, "country") {
			found = true
		}
	}

	if !found {
		t.Errorf("country belongs to customers alone and must be attributed to it: %+v", rep.Candidates)
	}
}

func TestBuildRefusesColumnsTwoTablesShare(t *testing.T) {
	cat := ordersCatalog()
	archive := RelKey{Schema: testSchema, Name: "orders_archive"}

	cat.AddRelation(Relation{RelKey: archive, Kind: "r", Rows: testRows, Pages: 100, Root: RelKey{}})
	cat.AddColumn(archive, col("tenant_id", "integer", 50))
	cat.AddColumn(archive, col("id", "bigint", -1))

	w := workloadOf(entry(t, 1,
		`SELECT * FROM orders o JOIN orders_archive a ON a.id = o.id WHERE tenant_id = $1`, 1000))

	rep := Build(w, cat, Config{}) //nolint:exhaustruct

	// The statement still yields a candidate from its join columns, so it is not
	// counted as unproductive — the invariant under test is only that the column
	// nobody can attribute never reaches a key.
	for _, cand := range rep.Candidates {
		if slices.Contains(cand.Columns, "tenant_id") {
			t.Errorf("both tables have tenant_id; attributing it is a guess: %+v", cand)
		}
	}
}

// When ambiguity is the only thing standing between a statement and a candidate,
// it is what the report has to say about it.
func TestBuildReportsAmbiguousColumnWhenNothingElseSurvives(t *testing.T) {
	cat := ordersCatalog()
	archive := RelKey{Schema: testSchema, Name: "orders_archive"}

	cat.AddRelation(Relation{RelKey: archive, Kind: "r", Rows: testRows, Pages: 100, Root: RelKey{}})
	cat.AddColumn(archive, col("tenant_id", "integer", 50))

	w := workloadOf(entry(t, 1,
		`SELECT * FROM orders, orders_archive WHERE tenant_id = $1`, 1000))

	rep := Build(w, cat, Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 0 {
		t.Fatalf("nothing here can be attributed: %+v", rep.Candidates)
	}

	if got := reasonCount(rep, ReasonAmbiguousColumn); got != 1 {
		t.Errorf("ambiguous_column = %d, want 1", got)
	}
}

func TestBuildSkipsColumnsWithoutBtreeSupport(t *testing.T) {
	w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE payload = $1`, 1000))

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 0 {
		t.Errorf("json has no btree operator class: %+v", rep.Candidates)
	}

	if got := reasonCount(rep, ReasonUnsupportedType); got != 1 {
		t.Errorf("unsupported_type = %d, want 1", got)
	}
}

func TestBuildWarnsOnWriteHeavyTables(t *testing.T) {
	w := workloadOf(
		entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000),
		entryCalls(t, 2, `UPDATE orders SET status = $1 WHERE id = $2`, 500, 20_000),
	)

	cand := onlyCandidate(t, Build(w, ordersCatalog(), Config{})) //nolint:exhaustruct
	if !hasWarning(cand, WarnWriteHeavy) {
		t.Error("20 000 updates against 100 reads must warn")
	}
}

// The lifetime counters carry the bulk load that filled the table; the workload
// writes it once. That is not a write-heavy table.
func TestBuildDoesNotCallASeededTableWriteHeavy(t *testing.T) {
	cat := ordersCatalog()

	cat.AddWrites(RelKey{Schema: testSchema, Name: "orders"}, Writes{
		Inserted: 5_000_000, Updated: 0, Deleted: 0,
		SeqScans: 10, IdxScans: 100,
	})

	w := workloadOf(
		entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000),
		entryCalls(t, 2, `INSERT INTO orders (tenant_id, status) SELECT $1, $2`, 5000, 1),
	)

	cand := onlyCandidate(t, Build(w, cat, Config{})) //nolint:exhaustruct
	if hasWarning(cand, WarnWriteHeavy) {
		t.Error("one bulk load is not a write-heavy table")
	}
}

// A materialized view takes indexes like a table, but REFRESH rebuilds them, and
// the workload cannot show that: REFRESH is a utility statement.
func TestBuildWarnsOnMaterializedViews(t *testing.T) {
	cat := ordersCatalog()
	orders := RelKey{Schema: testSchema, Name: "orders"}

	rel := cat.Relations[orders]
	rel.Kind = matviewKind
	cat.AddRelation(rel)

	w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000))

	cand := onlyCandidate(t, Build(w, cat, Config{})) //nolint:exhaustruct
	if !hasWarning(cand, WarnMatview) {
		t.Error("an index on a matview is rebuilt by every plain REFRESH")
	}
}

func TestBuildSeparatesCatalogQueriesFromMissingTables(t *testing.T) {
	w := workloadOf(
		entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 100),
		entry(t, 2, `SELECT count(*) FROM pg_stat_activity WHERE state = $1`, 9900),
	)

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct

	if got := reasonCount(rep, ReasonSystemRelation); got != 1 {
		t.Errorf("system_relation = %d, want 1", got)
	}

	if got := reasonCount(rep, ReasonUnknownRelation); got != 0 {
		t.Errorf("unknown_relation = %d, want 0: a catalog is not a missing table", got)
	}
}

// Monitoring left in the denominator would shrink the one application statement
// here to 1%.
func TestBuildKeepsMonitoringOutOfTheWeights(t *testing.T) {
	w := workloadOf(
		entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 100),
		entry(t, 2, `SELECT count(*) FROM pg_stat_activity WHERE state = $1`, 9900),
	)

	cand := onlyCandidate(t, Build(w, ordersCatalog(), Config{})) //nolint:exhaustruct
	if cand.WeightPct < 99.9 {
		t.Errorf("weight = %.2f, want 100", cand.WeightPct)
	}
}

// FR-4.8: what the collector could not parse stays in the report, or an empty
// candidate list would read as a clean bill of health.
func TestBuildKeepsCollectorFailures(t *testing.T) {
	w := workloadOf(entry(t, 1, `SELECT * FROM orders WHERE tenant_id = $1`, 1000))
	w.NotParsed = map[string]int{sqlparse.ReasonTruncated: 3, sqlparse.ReasonParseError: 1}
	w.Collected = 5

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct

	if got := reasonCount(rep, sqlparse.ReasonTruncated); got != 3 {
		t.Errorf("truncated = %d, want 3", got)
	}

	if rep.Summary.NotParsedCount != 4 {
		t.Errorf("NotParsedCount = %d, want 4", rep.Summary.NotParsedCount)
	}

	// Most frequent first, so the section can lead with the dominant cause.
	if len(rep.NotParsed) == 0 || rep.NotParsed[0].ReasonCode != sqlparse.ReasonTruncated {
		t.Errorf("NotParsed = %+v, want the largest count first", rep.NotParsed)
	}
}

func TestBuildIgnoresInsertsAndUtilityStatements(t *testing.T) {
	w := workloadOf(
		entry(t, 1, `INSERT INTO orders (tenant_id, customer_id) VALUES ($1, $2)`, 500),
		entry(t, 2, `SET search_path TO $1`, 10),
	)

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 0 {
		t.Errorf("neither statement is a reason to index: %+v", rep.Candidates)
	}

	if rep.Summary.NotParsedCount != 0 {
		t.Errorf("NotParsedCount = %d — a write is workload, not a failure", rep.Summary.NotParsedCount)
	}
}

func TestBuildRanksByWeightAndCaps(t *testing.T) {
	cat := ordersCatalog()
	other := RelKey{Schema: testSchema, Name: "shipments"}

	cat.AddRelation(Relation{RelKey: other, Kind: "r", Rows: testRows, Pages: 100, Root: RelKey{}})
	cat.AddColumn(other, col("carrier_id", "integer", 30))

	w := workloadOf(
		entry(t, 1, `SELECT * FROM shipments WHERE carrier_id = $1`, 200),
		entry(t, 2, `SELECT * FROM orders WHERE tenant_id = $1`, 800),
	)

	rep := Build(w, cat, Config{}) //nolint:exhaustruct

	if len(rep.Candidates) != 2 {
		t.Fatalf("want a candidate per table, got %d", len(rep.Candidates))
	}

	if rep.Candidates[0].Table != "orders" {
		t.Errorf("first candidate is %q, want the heavier table first", rep.Candidates[0].Table)
	}

	if rep.Candidates[0].WeightPct != 80 {
		t.Errorf("WeightPct = %v, want 80", rep.Candidates[0].WeightPct)
	}

	// A fifth of the load is not marginal, so nothing should apologise for it.
	if hasWarning(rep.Candidates[1], WarnLowWeight) {
		t.Errorf("low_weight on a candidate carrying %v%% of the load", rep.Candidates[1].WeightPct)
	}

	capped := Build(w, cat, Config{MaxCandidates: 1}) //nolint:exhaustruct
	if len(capped.Candidates) != 1 {
		t.Errorf("max_candidates=1 must cut the list to one, got %d", len(capped.Candidates))
	}
}

// A statement running on three hosts is one statement with three sources, and the
// candidate for it has to be weighed by what the cluster spends on it. Reading one
// host would rank it a third as heavy, which is how the advisor comes to recommend
// the wrong index first.
func TestBuildSumsOneStatementAcrossHosts(t *testing.T) {
	const sql = `SELECT id FROM orders WHERE tenant_id = $1 AND customer_id = $2`

	w := workloadOf(
		entryOn(t, "replica-1", 42, sql, 600),
		entryOn(t, "primary", 42, sql, 300),
		entryOn(t, "replica-2", 42, sql, 100),
	)
	w.Hosts = []string{"replica-1", "primary", "replica-2"}

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct
	cand := onlyCandidate(t, rep)

	if len(cand.Covered) != 1 {
		t.Fatalf("Covered = %d entries, want 1 — the same statement on three hosts is one unit of work", len(cand.Covered))
	}

	covered := cand.Covered[0]

	want := []string{"primary", "replica-1", "replica-2"}
	if !slices.Equal(covered.Hosts, want) {
		t.Errorf("Hosts = %v, want %v — every host the statement was seen on, sorted", covered.Hosts, want)
	}

	if !slices.Equal(covered.QueryIDs, []int64{42}) {
		t.Errorf("QueryIDs = %v, want [42] — queryid is derived from the parse tree and repeats on every host", covered.QueryIDs)
	}

	if covered.Calls != 300 {
		t.Errorf("Calls = %d, want 300 — the calls of all three hosts", covered.Calls)
	}

	if cand.WeightPct != 100 {
		t.Errorf("WeightPct = %v, want 100 — it is the only statement in the cluster", cand.WeightPct)
	}

	if !slices.Equal(rep.Summary.Hosts, want) {
		t.Errorf("Summary.Hosts = %v, want %v", rep.Summary.Hosts, want)
	}
}

// The fuller text wins the merge: track_activity_query_size is per-host, so the
// same statement can arrive clipped on one host and whole on another.
func TestBuildKeepsTheFullestStatementText(t *testing.T) {
	const sql = `SELECT id FROM orders WHERE tenant_id = $1 AND customer_id = $2`

	clipped := entryOn(t, "replica-1", 42, sql, 100)
	clipped.Query = "SELECT id FROM orders WHERE tena"

	w := workloadOf(clipped, entryOn(t, "primary", 42, sql, 100))

	cand := onlyCandidate(t, Build(w, ordersCatalog(), Config{})) //nolint:exhaustruct
	if got := cand.Covered[0].Query; got != sql {
		t.Errorf("Query = %q, want the unclipped text %q", got, sql)
	}
}

// An unread host is workload the advisor never saw, and the report has to say so:
// the candidate list is incomplete by exactly that much, not merely shorter.
func TestBuildReportsUnreachableHosts(t *testing.T) {
	w := workloadOf(entryOn(t, "primary", 1,
		`SELECT id FROM orders WHERE tenant_id = $1 AND customer_id = $2`, 1000))
	w.Hosts = []string{"primary"}
	w.Unreachable = []string{"replica-2", "replica-1"}
	w.NoStats = []string{"replica-3"}

	rep := Build(w, ordersCatalog(), Config{}) //nolint:exhaustruct

	if want := []string{"replica-1", "replica-2"}; !slices.Equal(rep.UnreachableHosts, want) {
		t.Errorf("UnreachableHosts = %v, want %v sorted", rep.UnreachableHosts, want)
	}

	// A host that answers but keeps no statistics is neither analyzed nor
	// unreachable, and collapsing it into either would misstate what happened.
	if want := []string{"replica-3"}; !slices.Equal(rep.Summary.HostsWithoutStats, want) {
		t.Errorf("HostsWithoutStats = %v, want %v", rep.Summary.HostsWithoutStats, want)
	}
}
