//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/schemalint"
	"github.com/dbulashev/dasha/internal/testinfra"
)

// seedSchemaLintDefects plants one instance of every defect the stage-1 checks
// look for. Returns the server version so version-gated assertions can adapt.
func seedSchemaLintDefects(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vNum int) {
	t.Helper()

	stmts := []string{
		`CREATE SCHEMA lint_test`,
		// serial → the sequence maxes out at int4, and setval puts it at the edge.
		`CREATE TABLE lint_test.orders (id serial PRIMARY KEY, val text)`,
		`SELECT setval('lint_test.orders_id_seq', 2147483000)`,
		// A unique index but no primary key, and the unique column is nullable —
		// the case where "you already have a unique index" is wrong advice.
		`CREATE TABLE lint_test.no_pk (id int UNIQUE, val text)`,
		// Neither a primary key nor any unique index.
		`CREATE TABLE lint_test.no_key (val text)`,
		`CREATE UNLOGGED TABLE lint_test.scratch (id int PRIMARY KEY)`,
		// A partitioned table without a key: one defect, three partitions.
		`CREATE TABLE lint_test.events (id int, at date) PARTITION BY RANGE (at)`,
		`CREATE TABLE lint_test.events_01 PARTITION OF lint_test.events FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`,
		`CREATE TABLE lint_test.events_02 PARTITION OF lint_test.events FOR VALUES FROM ('2026-02-01') TO ('2026-03-01')`,
		`CREATE TABLE lint_test.events_03 PARTITION OF lint_test.events FOR VALUES FROM ('2026-03-01') TO ('2026-04-01')`,
		`GRANT CREATE ON SCHEMA lint_test TO PUBLIC`,
		// A UUID kept as text, and a table on neither side of any foreign key.
		`CREATE TABLE lint_test.accounts (id int PRIMARY KEY, external_id varchar(36))`,
		// Every column dropped — the relation stays in the catalog with none left.
		`CREATE TABLE lint_test.hollow (gone int)`,
		`ALTER TABLE lint_test.hollow DROP COLUMN gone`,
		// A reserved keyword as a name, and a name that cannot be written unquoted.
		`CREATE TABLE lint_test."order" (id int PRIMARY KEY)`,
		`CREATE TABLE lint_test."my table" (id int PRIMARY KEY)`,
		// A constraint nobody validated against the existing rows.
		`CREATE TABLE lint_test.parents (id int PRIMARY KEY)`,
		`CREATE TABLE lint_test.children (id int PRIMARY KEY, parent_id int)`,
		`ALTER TABLE lint_test.children ADD CONSTRAINT fk_parent
		     FOREIGN KEY (parent_id) REFERENCES lint_test.parents(id) NOT VALID`,
		// A btree over an array column — useless for containment queries.
		`CREATE TABLE lint_test.tagged (id int PRIMARY KEY, tags text[])`,
		`CREATE INDEX tagged_tags_idx ON lint_test.tagged (tags)`,
	}

	// UNLOGGED sequences only exist from PG 15 on.
	if vNum >= 150000 {
		stmts = append(stmts, `CREATE UNLOGGED SEQUENCE lint_test.scratch_seq`)
	}

	for _, stmt := range stmts {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err, stmt)
	}
}

func schemaLintReport(t *testing.T, ctx context.Context, p *PgxPool, pool *pgxpool.Pool, vNum int) schemalint.Report {
	t.Helper()

	return schemalint.BuildReport(p.collectSchemaLint(ctx, pool, vNum), p.schemaLintConfig)
}

func findFinding(report schemalint.Report, code, schema, object string) (schemalint.Finding, bool) {
	for _, f := range report.Findings {
		if f.Code == code && f.Schema == schema && f.Object == object {
			return f, true
		}
	}

	return schemalint.Finding{}, false
}

func TestSchemaLint_DetectsSeededDefects(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	seedSchemaLintDefects(t, ctx, pool, vNum)

	report := schemaLintReport(t, ctx, p, pool, vNum)

	for _, s := range report.Skipped {
		assert.NotEqual(t, schemalint.SkipError, s.Reason,
			"check %s failed to run: %s", s.Code, s.Detail)
	}

	t.Run("sequence at the int4 edge is an error", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeSequenceExhaustion, "lint_test", "orders_id_seq")
		require.True(t, ok, "sequence near its ceiling must be reported")
		assert.Equal(t, schemalint.LevelError, f.Level)
		require.NotNil(t, f.Params.UsedPct)
		assert.Greater(t, *f.Params.UsedPct, 99.0)
		assert.Equal(t, "lint_test.orders.id", f.Params.OwnedBy)
		assert.Equal(t, "integer", f.Params.OwnedColumnType)
	})

	t.Run("table with a nullable unique index lacks a primary key", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeNoPrimaryKey, "lint_test", "no_pk")
		require.True(t, ok)
		assert.Equal(t, schemalint.LevelError, f.Level)
		assert.True(t, f.Params.UniqueNullable,
			"a nullable unique index is not a replica identity and must be flagged")
	})

	t.Run("table without any unique key", func(t *testing.T) {
		_, ok := findFinding(report, schemalint.CodeNoUniqueKey, "lint_test", "no_key")
		assert.True(t, ok)
	})

	t.Run("partitions collapse into the root table", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeNoUniqueKey, "lint_test", "events")
		require.True(t, ok, "the finding must sit on the root, not on the partitions")
		assert.Equal(t, 3, f.Params.Partitions)

		for _, part := range []string{"events_01", "events_02", "events_03"} {
			_, found := findFinding(report, schemalint.CodeNoUniqueKey, "lint_test", part)
			assert.False(t, found, "partition %s must not produce a row of its own", part)
		}
	})

	t.Run("unlogged relation", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeUnloggedRelation, "lint_test", "scratch")
		require.True(t, ok)
		assert.Equal(t, schemalint.LevelWarning, f.Level)
	})

	t.Run("unlogged sequence", func(t *testing.T) {
		if vNum < 150000 {
			t.Skip("unlogged sequences require PG 15")
		}

		_, ok := findFinding(report, schemalint.CodeUnloggedSequence, "lint_test", "scratch_seq")
		assert.True(t, ok)
	})

	t.Run("public create privilege, severity by version", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodePublicCreatePrivilege, "lint_test", "lint_test")
		require.True(t, ok, "a schema PUBLIC may create in must be reported")

		want := schemalint.LevelError
		if vNum < 150000 {
			want = schemalint.LevelWarning
		}

		assert.Equal(t, want, f.Level)
		assert.NotEmpty(t, f.Params.Owner)
	})

	t.Run("uuid kept as text is found on its column", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeUUIDInNonUUIDType, "lint_test", "accounts")
		require.True(t, ok)
		assert.Equal(t, "external_id", f.Params.Column)
		assert.Contains(t, f.Params.ColumnType, "36")
		assert.Equal(t, schemalint.LevelNotice, f.Level)
	})

	t.Run("relation with every column dropped", func(t *testing.T) {
		_, ok := findFinding(report, schemalint.CodeRelationWithoutColumns, "lint_test", "hollow")
		assert.True(t, ok)
	})

	t.Run("relation_without_fk stays silent until enabled", func(t *testing.T) {
		_, ok := findFinding(report, schemalint.CodeRelationWithoutFk, "lint_test", "accounts")
		require.False(t, ok, "the check is off by default")

		optedIn := NewTestPgxPool(pool, zap.NewNop())
		optedIn.schemaLintConfig = schemalint.Config{EnabledChecks: []string{schemalint.CodeRelationWithoutFk}}

		enabled := schemalint.BuildReport(optedIn.collectSchemaLint(ctx, pool, vNum), optedIn.schemaLintConfig)

		_, ok = findFinding(enabled, schemalint.CodeRelationWithoutFk, "lint_test", "accounts")
		assert.True(t, ok, "opting in must surface tables outside any foreign key")
	})

	t.Run("names that break tooling", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeReservedWordInName, "lint_test", "order")
		require.True(t, ok, "a reserved keyword as a table name must be reported")
		assert.Equal(t, schemalint.LevelWarning, f.Level)

		_, ok = findFinding(report, schemalint.CodeUnsafeCharsInName, "lint_test", "my table")
		assert.True(t, ok, "a name with a space must be reported")
	})

	t.Run("unvalidated constraint reaches the report", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeInvalidConstraint, "lint_test", "children")
		require.True(t, ok, "a NOT VALID constraint must reach the schema report")
		assert.Equal(t, "fk_parent", f.Params.Constraint)
		assert.Contains(t, f.Params.ReferencedBy, "parents")
	})

	t.Run("checks borrowed from other pages carry their schema", func(t *testing.T) {
		f, ok := findFinding(report, schemalint.CodeBtreeOnArray, "lint_test", "tagged")
		require.True(t, ok, "a btree over an array column must reach the schema report")
		assert.Equal(t, "tagged_tags_idx", f.Params.Index)
		assert.Equal(t, schemalint.ObjectTypeIndex, f.ObjectType)
	})

	t.Run("summary counts every finding", func(t *testing.T) {
		counted := report.Summary[schemalint.LevelError] +
			report.Summary[schemalint.LevelWarning] +
			report.Summary[schemalint.LevelNotice]
		assert.Equal(t, len(report.Findings), counted)
	})
}

func TestSchemaLint_SystemSchemasAreNeverReported(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	report := schemaLintReport(t, ctx, p, pool, vNum)

	for _, f := range report.Findings {
		assert.False(t, strings.HasPrefix(f.Schema, "pg_"),
			"system schema %s leaked into the report", f.Schema)
		assert.NotEqual(t, "information_schema", f.Schema)
	}
}

// TestSchemaLint_UnreadableSequenceBecomesSkip is the privilege case that
// matters most: a role that cannot read last_value would see every sequence as
// untouched, i.e. healthy. The report must say the check did not run instead.
func TestSchemaLint_UnreadableSequenceBecomesSkip(t *testing.T) {
	t.Parallel()
	adminPool := testinfra.IsolatePool(t)
	ctx := t.Context()

	admin := NewTestPgxPool(adminPool, zap.NewNop())

	vNum, err := admin.getServerVersionNum(ctx, adminPool)
	require.NoError(t, err)

	seedSchemaLintDefects(t, ctx, adminPool, vNum)

	const role = "lint_watcher"

	for _, stmt := range []string{
		fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD 'secret'`, role),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA lint_test TO %s`, role),
		// The role gets no privilege on the sequences, and neither does PUBLIC —
		// so pg_sequences hides last_value from it.
		`REVOKE ALL ON ALL SEQUENCES IN SCHEMA lint_test FROM PUBLIC`,
	} {
		_, err := adminPool.Exec(ctx, stmt)
		require.NoError(t, err, stmt)
	}

	t.Cleanup(func() {
		cleanCtx := context.Background()
		_, _ = adminPool.Exec(cleanCtx, fmt.Sprintf(`DROP OWNED BY %s`, role))
		_, _ = adminPool.Exec(cleanCtx, fmt.Sprintf(`DROP ROLE IF EXISTS %s`, role))
	})

	var dbName string
	require.NoError(t, adminPool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName))

	// Reuse the admin pool's endpoint; only the credentials differ.
	conn := adminPool.Config().ConnConfig
	dsn := fmt.Sprintf("postgres://%s:secret@%s:%d/%s?sslmode=disable", role, conn.Host, conn.Port, dbName)

	rolePool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect as the unprivileged role")

	defer rolePool.Close()

	watcher := NewTestPgxPool(rolePool, zap.NewNop())
	report := schemaLintReport(t, ctx, watcher, rolePool, vNum)

	var skipped bool

	for _, s := range report.Skipped {
		if s.Code == schemalint.CodeSequenceExhaustion && s.Reason == schemalint.SkipInsufficientPrivileges {
			skipped = true
		}
	}

	assert.True(t, skipped,
		"sequences it cannot read must be reported as a skip, not silently omitted")

	for _, f := range report.Findings {
		assert.NotEqual(t, schemalint.CodeSequenceExhaustion, f.Code,
			"an unreadable sequence must not produce a finding either way")
	}
}
