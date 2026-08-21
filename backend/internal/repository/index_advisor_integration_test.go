//go:build integration

package repository

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/indexadvisor"
	"github.com/dbulashev/dasha/internal/sqlparse"
	"github.com/dbulashev/dasha/internal/testinfra"
)

// seedIndexAdvisorSchema plants the shapes the catalog queries have to get right:
// a varchar column (btree-indexable only through a binary-coercible cast), a json
// column (no btree operator class at all), a partial and an expression index
// (neither covers a plain candidate), and a partitioned table whose rows live in
// its partitions.
func seedIndexAdvisorSchema(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	stmts := []string{
		`CREATE SCHEMA advisor_test`,
		`CREATE TABLE advisor_test.orders (
		     id         bigserial PRIMARY KEY,
		     tenant_id  int NOT NULL,
		     customer_id int NOT NULL,
		     status     varchar(32) NOT NULL,
		     payload    json,
		     processed_at timestamptz,
		     created_at timestamptz NOT NULL DEFAULT now()
		 )`,
		`CREATE INDEX orders_tenant_created_idx ON advisor_test.orders (tenant_id, created_at)`,
		`CREATE INDEX orders_open_idx ON advisor_test.orders (status) WHERE status = 'open'`,
		`CREATE INDEX orders_pending_idx ON advisor_test.orders (tenant_id) WHERE processed_at IS NULL`,
		`CREATE INDEX orders_lower_status_idx ON advisor_test.orders (lower(status))`,
		`INSERT INTO advisor_test.orders (tenant_id, customer_id, status, processed_at)
		     SELECT g % 7, g % 500, CASE WHEN g % 3 = 0 THEN 'open' ELSE 'done' END,
		            CASE WHEN g % 20 = 0 THEN NULL ELSE now() END
		     FROM generate_series(1, 2000) g`,

		`CREATE TABLE advisor_test.events (id bigint, tenant_id int, at date NOT NULL)
		     PARTITION BY RANGE (at)`,
		`CREATE TABLE advisor_test.events_01 PARTITION OF advisor_test.events
		     FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`,
		`CREATE TABLE advisor_test.events_02 PARTITION OF advisor_test.events
		     FOR VALUES FROM ('2026-02-01') TO ('2026-03-01')`,
		`INSERT INTO advisor_test.events (id, tenant_id, at)
		     SELECT g, g % 5, DATE '2026-01-01' + (g % 40) FROM generate_series(1, 400) g`,

		// A temporary table must stay out of every answer: it belongs to one
		// session and cannot be indexed on anyone's advice.
		`CREATE TEMP TABLE advisor_scratch (id int, val text)`,

		`ANALYZE advisor_test.orders`,
		`ANALYZE advisor_test.events`,
	}

	for _, stmt := range stmts {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err, stmt)
	}
}

func findIndex(indexes []indexadvisor.Index, name string) (indexadvisor.Index, bool) {
	for _, idx := range indexes {
		if idx.Name == name {
			return idx, true
		}
	}

	return indexadvisor.Index{}, false
}

func findColumn(columns []indexadvisor.Column, name string) (indexadvisor.Column, bool) {
	for _, col := range columns {
		if col.Name == name {
			return col, true
		}
	}

	return indexadvisor.Column{}, false
}

func TestIndexAdvisorCatalog_ReadsSeededSchema(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	seedIndexAdvisorSchema(ctx, t, pool)

	cat, err := p.collectIndexAdvisorClusterCatalog(ctx, oneHostCluster(pool, vNum))
	require.NoError(t, err)
	assert.False(t, cat.Truncated, "the seeded schema is far below the row cap")

	orders := indexadvisor.RelKey{Schema: "advisor_test", Name: "orders"}
	events := indexadvisor.RelKey{Schema: "advisor_test", Name: "events"}
	events01 := indexadvisor.RelKey{Schema: "advisor_test", Name: "events_01"}

	t.Run("ordinary table", func(t *testing.T) {
		rel, ok := cat.Relations[orders]
		require.True(t, ok, "seeded table must be in the catalog")
		assert.Equal(t, "r", rel.Kind)
		assert.False(t, rel.IsPartition())
		assert.Greater(t, rel.Rows, int64(1500), "reltuples after ANALYZE")
	})

	t.Run("partitioned root carries the rows of its partitions", func(t *testing.T) {
		root, ok := cat.Relations[events]
		require.True(t, ok)
		assert.Equal(t, "p", root.Kind)
		// The root's own reltuples is zero, and ANALYZE on the root sets it to the
		// total — so summing every level would double-count. 400 rows were seeded.
		assert.Equal(t, int64(400), root.Rows)

		part, ok := cat.Relations[events01]
		require.True(t, ok)
		assert.Equal(t, events, part.Root, "a partition must point at its root")
		// The DDL attaches an index level by level, and only Parent says which
		// level that is once a tree is deeper than one.
		assert.Equal(t, events, part.Parent, "a partition must point at the level above it")
	})

	t.Run("temporary tables are invisible", func(t *testing.T) {
		for key := range cat.Relations {
			assert.NotEqual(t, "advisor_scratch", key.Name)
		}
	})

	// pg_stat_statements stores no search_path, so an unqualified name is only
	// usable when exactly one schema answers to it. The fixture template already
	// owns a public.orders, which makes the seeded advisor_test.orders the
	// ambiguous case for free — and ambiguity is what a candidate must be refused
	// over rather than guessed through.
	t.Run("a name two schemas share stays ambiguous", func(t *testing.T) {
		keys := cat.ByName["orders"]
		assert.Contains(t, keys, orders)
		// The count is not the point and would follow whatever the fixture grows
		// next; more than one schema answering to the name is.
		assert.Greater(t, len(keys), 1, "public.orders from the fixture and the seeded advisor_test.orders")
	})

	t.Run("a name only one schema holds resolves", func(t *testing.T) {
		keys := cat.ByName["events_01"]
		require.Len(t, keys, 1)
		assert.Equal(t, events01, keys[0])
	})

	t.Run("btree indexability follows the operator class, not the type name", func(t *testing.T) {
		cols := cat.Columns[orders]
		require.NotEmpty(t, cols)

		status, ok := findColumn(cols, "status")
		require.True(t, ok)
		assert.True(t, status.BtreeIndexable,
			"varchar reaches text_ops through a binary-coercible cast and is indexable")
		assert.True(t, status.StatsKnown, "the table was analyzed")
		assert.NotZero(t, status.NDistinct)

		payload, ok := findColumn(cols, "payload")
		require.True(t, ok)
		assert.False(t, payload.BtreeIndexable, "json has no default btree operator class")
	})

	t.Run("columns appear once per table", func(t *testing.T) {
		seen := make(map[string]int)
		for _, col := range cat.Columns[orders] {
			seen[col.Name]++
		}

		for name, n := range seen {
			assert.Equal(t, 1, n, "column %s was collected %d times", name, n)
		}
	})

	t.Run("index key columns keep their order", func(t *testing.T) {
		idx, ok := findIndex(cat.Indexes[orders], "orders_tenant_created_idx")
		require.True(t, ok)
		assert.Equal(t, []string{"tenant_id", "created_at"}, idx.Columns)
		assert.Equal(t, "btree", idx.Method)
		assert.False(t, idx.Unique)
		assert.True(t, idx.Valid)
		assert.False(t, idx.Partial)
		assert.False(t, idx.Expression)
	})

	t.Run("primary key is reported as unique", func(t *testing.T) {
		idx, ok := findIndex(cat.Indexes[orders], "orders_pkey")
		require.True(t, ok)
		assert.True(t, idx.Unique)
		assert.True(t, idx.Primary)
		assert.Equal(t, []string{"id"}, idx.Columns)
	})

	t.Run("partial and expression indexes are flagged", func(t *testing.T) {
		partial, ok := findIndex(cat.Indexes[orders], "orders_open_idx")
		require.True(t, ok)
		assert.True(t, partial.Partial, "a partial index must not count as covering")

		expr, ok := findIndex(cat.Indexes[orders], "orders_lower_status_idx")
		require.True(t, ok)
		assert.True(t, expr.Expression, "an expression index must not count as covering")
	})

	// The shape the predicate parser expects has to be checked against a real server.
	t.Run("an IS NULL predicate is read column by column", func(t *testing.T) {
		pending, ok := findIndex(cat.Indexes[orders], "orders_pending_idx")
		require.True(t, ok)
		assert.True(t, pending.Partial)
		assert.Equal(t, []string{"processed_at"}, pending.NullPredicate)

		open, ok := findIndex(cat.Indexes[orders], "orders_open_idx")
		require.True(t, ok)
		assert.Empty(t, open.NullPredicate, "a comparison cannot be compared with a candidate's predicate")
	})

	t.Run("write activity is collected", func(t *testing.T) {
		// Table counters reach pg_stat_user_tables asynchronously — a backend
		// flushes its pending statistics about a second after the transaction, and
		// the backend holding them is not necessarily the one this pool answers
		// on. So they are re-read until they land rather than once.
		var w indexadvisor.Writes

		require.Eventually(t, func() bool {
			fresh := indexadvisor.NewCatalog()
			if err := p.readIndexAdvisorWrites(ctx, pool, vNum, &fresh); err != nil {
				return false
			}

			w = fresh.Writes[orders]

			return w.Inserted > 0
		}, 10*time.Second, 250*time.Millisecond, "insert counters must reach pg_stat_user_tables")

		assert.Positive(t, w.SeqScans+w.IdxScans, "the seeded reads must be counted too")
	})
}

// The whole of step 1 against a real server: read the workload, read the catalog,
// build the candidates, then run the DDL a candidate proposes and watch it stop
// being a candidate. That last part is the acceptance criterion no unit test can
// give — the suggested statement has to be valid SQL, and the duplicate check has
// to recognize the index it just asked for.
func TestIndexAdvisor_EndToEndAgainstRealSchema(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	seedIndexAdvisorSchema(ctx, t, pool)

	statements := []string{
		// Wants an index; customer_id (500 distinct) is more selective than
		// tenant_id (7), so it must come first in the key.
		`SELECT id FROM advisor_test.orders WHERE tenant_id = 3 AND customer_id = 42`,
		// Already served by the primary key.
		`SELECT status FROM advisor_test.orders WHERE id = 7`,
		// Already served by orders_tenant_created_idx.
		`SELECT id FROM advisor_test.orders WHERE tenant_id = 3 AND created_at > now()`,
		// IS NULL belongs in the predicate of a partial candidate, not in the key.
		`SELECT id FROM advisor_test.orders WHERE processed_at IS NULL AND customer_id = 42`,
	}

	for range 3 {
		for _, stmt := range statements {
			_, err := pool.Exec(ctx, stmt)
			require.NoError(t, err, stmt)
		}
	}

	// The seeded table holds 2000 rows, well under the production threshold — the
	// point here is the pipeline, not the size heuristic.
	cfg := indexadvisor.Config{MinTableRows: 100} //nolint:exhaustruct

	report := buildIndexAdvisorReport(ctx, t, p, pool, vNum, cfg)

	cand, ok := findCandidate(report, "advisor_test", "orders", []string{"customer_id", "tenant_id"})
	require.True(t, ok, "the two-column filter must yield a candidate: %+v", report.Candidates)

	assert.False(t, cand.PlannerChecked)
	assert.Positive(t, cand.WeightPct)
	assert.NotEmpty(t, cand.Covered, "a candidate must name the statements behind it")

	t.Run("an is null filter becomes a partial candidate", func(t *testing.T) {
		partial, ok := findCandidate(report, "advisor_test", "orders", []string{"customer_id"})
		require.True(t, ok, "the IS NULL statement must yield a partial candidate: %+v", report.Candidates)

		assert.Equal(t, `"processed_at" IS NULL`, partial.Predicate)
		assert.Contains(t, partial.DDL, `WHERE "processed_at" IS NULL`)
	})

	t.Run("statements an index already serves produce none", func(t *testing.T) {
		for _, c := range report.Candidates {
			assert.NotEqual(t, []string{"id"}, c.Columns, "the primary key already serves this")
			assert.NotEqual(t, []string{"tenant_id", "created_at"}, c.Columns,
				"orders_tenant_created_idx already serves this")
		}

		var alreadyIndexed int

		for _, n := range report.NotParsed {
			if n.ReasonCode == indexadvisor.ReasonAlreadyIndexed {
				alreadyIndexed = n.Count
			}
		}

		assert.GreaterOrEqual(t, alreadyIndexed, 2, "both covered statements must say so")
	})

	t.Run("the suggested ddl runs and settles the candidate", func(t *testing.T) {
		_, err := pool.Exec(ctx, cand.DDL)
		require.NoError(t, err, "the suggested statement must be valid SQL: %s", cand.DDL)

		after := buildIndexAdvisorReport(ctx, t, p, pool, vNum, cfg)

		_, still := findCandidate(after, "advisor_test", "orders", []string{"customer_id", "tenant_id"})
		assert.False(t, still, "the index now exists; proposing it again would be a duplicate")
	})
}

// The DDL of a partitioned table is a script rather than a statement: PostgreSQL
// rejects CREATE INDEX CONCURRENTLY on the root, so the advisor proposes an
// ON ONLY root index, a concurrent build per partition and an ATTACH each. Here
// it runs the way psql sends it — statement by statement — and the duplicate
// check answers whether the root index came out valid: an invalid one covers
// nothing, and the candidate would come back.
func TestIndexAdvisor_PartitionedDDLRunsAndSettlesTheCandidate(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	seedIndexAdvisorSchema(ctx, t, pool)

	for range 3 {
		_, err := pool.Exec(ctx, `SELECT id FROM advisor_test.events WHERE tenant_id = 3`)
		require.NoError(t, err)
	}

	cfg := indexadvisor.Config{MinTableRows: 100} //nolint:exhaustruct

	report := buildIndexAdvisorReport(ctx, t, p, pool, vNum, cfg)

	cand, ok := findCandidate(report, "advisor_test", "events", []string{"tenant_id"})
	require.True(t, ok, "the partitioned root must yield a candidate: %+v", report.Candidates)

	require.Contains(t, cand.DDL, "ON ONLY", "the root index has to be built invalid")
	require.Contains(t, cand.DDL, "ATTACH PARTITION", "every partition has to be attached")

	for _, stmt := range ddlStatements(cand.DDL) {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err, "the suggested script must run statement by statement: %s", stmt)
	}

	after := buildIndexAdvisorReport(ctx, t, p, pool, vNum, cfg)

	_, still := findCandidate(after, "advisor_test", "events", []string{"tenant_id"})
	assert.False(t, still, "the root index turns valid with the last ATTACH, so it now covers the candidate")
}

// ddlStatements splits the script the way psql sends it. CREATE INDEX
// CONCURRENTLY cannot run inside a transaction block, and a multi-statement Exec
// is one — so the statements have to go one at a time.
func ddlStatements(ddl string) []string {
	var out []string

	for _, line := range strings.Split(ddl, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}

		out = append(out, line)
	}

	return out
}

// testAdvisorHost names the single instance a test container is, so the host
// attribution the report carries can be asserted on.
const testAdvisorHost = "test-host"

// oneHostCluster is the cluster a test container amounts to: the cluster-wide
// readers take a list of hosts, and here the list has one entry.
func oneHostCluster(pool *pgxpool.Pool, vNum int) []indexAdvisorHost {
	return []indexAdvisorHost{{host: testAdvisorHost, pool: pool, vNum: vNum}}
}

func buildIndexAdvisorReport(
	ctx context.Context,
	t *testing.T,
	p *PgxPool,
	pool *pgxpool.Pool,
	vNum int,
	cfg indexadvisor.Config,
) indexadvisor.Report {
	t.Helper()

	w, err := p.collectIndexAdvisorWorkload(ctx, pool, testAdvisorHost, vNum, nil)
	require.NoError(t, err)

	cat, err := p.collectIndexAdvisorClusterCatalog(ctx, oneHostCluster(pool, vNum))
	require.NoError(t, err)

	return indexadvisor.Build(w, cat, cfg)
}

func findCandidate(
	rep indexadvisor.Report,
	schema, table string,
	columns []string,
) (indexadvisor.Candidate, bool) {
	for _, c := range rep.Candidates {
		if c.Schema == schema && c.Table == table && slices.Equal(c.Columns, columns) {
			return c, true
		}
	}

	return indexadvisor.Candidate{}, false
}

func TestIndexAdvisorWorkload_ParsesLiveStatements(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	seedIndexAdvisorSchema(ctx, t, pool)

	// Distinctive enough to find among the fixture's own statements.
	workloadStmts := []string{
		`SELECT id FROM advisor_test.orders WHERE tenant_id = 3 AND status = 'open'`,
		`SELECT count(*) FROM advisor_test.orders WHERE customer_id = 42`,
		`UPDATE advisor_test.orders SET status = 'done' WHERE id = 1`,
	}

	for range 3 {
		for _, stmt := range workloadStmts {
			_, err := pool.Exec(ctx, stmt)
			require.NoError(t, err, stmt)
		}
	}

	w, err := p.collectIndexAdvisorWorkload(ctx, pool, testAdvisorHost, vNum, nil)
	require.NoError(t, err)
	require.True(t, w.Available, "pg_stat_statements is installed in the fixture")
	assert.Positive(t, w.Collected)
	require.NotEmpty(t, w.Entries)
	assert.Equal(t, []string{testAdvisorHost}, w.Hosts, "the workload must name the host it came from")

	t.Run("statements of this database are parsed into usages", func(t *testing.T) {
		var found *indexadvisor.WorkloadEntry

		for i, e := range w.Entries {
			for _, u := range e.Stmt.Usages {
				if u.Ref.Name == "orders" && u.Column == "tenant_id" && u.Role == sqlparse.RoleEquality {
					found = &w.Entries[i]
				}
			}
		}

		require.NotNil(t, found, "the seeded SELECT must produce a tenant_id equality usage")
		assert.Equal(t, sqlparse.KindSelect, found.Stmt.Kind)
		assert.Equal(t, "advisor_test", found.Stmt.Tables[0].Schema)
		assert.NotEmpty(t, found.Fingerprint)
		assert.Positive(t, found.Calls)
		assert.NotEmpty(t, found.QueryIDs)
		// Attribution is per entry, not per read: entries from several hosts are
		// merged into one workload, and afterwards only this says where each ran.
		assert.Equal(t, []string{testAdvisorHost}, found.Hosts)
	})

	t.Run("update reports its write target", func(t *testing.T) {
		var found bool

		for _, e := range w.Entries {
			if e.Stmt.Kind != sqlparse.KindUpdate {
				continue
			}

			for _, ref := range e.Stmt.Written {
				if ref.Name == "orders" {
					found = true
				}
			}
		}

		assert.True(t, found, "the seeded UPDATE must be reported as a write on orders")
	})

	t.Run("every entry carries the numbers ranking needs", func(t *testing.T) {
		for _, e := range w.Entries {
			assert.NotEmpty(t, e.Query, "query text must survive sanitization")
			assert.GreaterOrEqual(t, e.TotalTimeMs, 0.0)
		}
	})
}
