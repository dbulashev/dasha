//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/piicatalog"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/testinfra"
)

type colKey struct {
	table   string // schema.table
	name    string
	comment string
	null    bool
}

func columnCatalog(t *testing.T, p *PgxPool, pool *pgxpool.Pool, exclude []string) map[colKey]int {
	t.Helper()

	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	qStr, err := query.Get(vNum, enums.QueryPiiCatalogColumns, nil)
	require.NoError(t, err)

	cols, err := scanColumnCatalog(ctx, pool, qStr, exclude)
	require.NoError(t, err)

	return colSet(cols)
}

func colSet(cols []piicatalog.Column) map[colKey]int {
	out := map[colKey]int{}

	for _, pr := range cols {
		k := colKey{table: pr.Schema + "." + pr.Table, name: pr.Name, null: pr.Comment == nil}
		if pr.Comment != nil {
			k.comment = *pr.Comment
		}

		out[k]++
	}

	return out
}

func nullCol(table, name string) colKey { return colKey{table: table, name: name, null: true} }

func commented(table, name, comment string) colKey {
	return colKey{table: table, name: name, comment: comment}
}

// Criteria 4–8: sources, inheritance, partitions, extensions, exclude_schemas.
func TestColumnCatalog_Sources(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolateEmptyPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())

	_, err := pool.Exec(t.Context(), `
		CREATE TABLE t_plain (id int, fio text);
		COMMENT ON COLUMN t_plain.fio IS 'ФИО';

		CREATE TABLE t_part (id int, snils text, d date) PARTITION BY RANGE (d);
		COMMENT ON COLUMN t_part.snils IS 'СНИЛС';
		CREATE TABLE t_part_2024 PARTITION OF t_part FOR VALUES FROM ('2024-01-01') TO ('2025-01-01');
		CREATE TABLE t_part_2025 PARTITION OF t_part FOR VALUES FROM ('2025-01-01') TO ('2026-01-01');
		COMMENT ON COLUMN t_part_2024.snils IS 'СНИЛС 2024';
		COMMENT ON COLUMN t_part_2025.snils IS 'СНИЛС';
		CREATE TABLE t_attached (id int, snils text, d date);
		ALTER TABLE t_part ATTACH PARTITION t_attached FOR VALUES FROM ('2023-01-01') TO ('2024-01-01');

		CREATE TABLE t_parent (id int, inn text);
		CREATE TABLE t_child (extra_col text) INHERITS (t_parent);
		COMMENT ON COLUMN t_child.inn IS 'ИНН наследника';

		CREATE MATERIALIZED VIEW mv AS SELECT 1 AS mv_col;
		CREATE VIEW v AS SELECT 1 AS view_col;

		CREATE EXTENSION file_fdw;
		CREATE SERVER fs FOREIGN DATA WRAPPER file_fdw;
		CREATE FOREIGN TABLE ft (ft_col text) SERVER fs OPTIONS (filename '/dev/null');
		CREATE TABLE ext_owned (ext_col int);
		ALTER EXTENSION file_fdw ADD TABLE ext_owned;

		CREATE SCHEMA archive_2023;
		CREATE TABLE archive_2023.a (archived_col int);
		CREATE SCHEMA tmp_x;
		CREATE TABLE tmp_x.a (tmp_col int);
		CREATE SCHEMA tmpx;
		CREATE TABLE tmpx.a (tmpx_col int);

		CREATE TABLE t_dropped (keep int, gone int);
		ALTER TABLE t_dropped DROP COLUMN gone;
	`)
	require.NoError(t, err)

	got := columnCatalog(t, p, pool, []string{"archive%", `tmp\_%`})

	// Partitions and children appear only with a comment of their own, even
	// one equal to the parent's.
	want := map[colKey]int{
		nullCol("public.t_plain", "id"):                        1,
		commented("public.t_plain", "fio", "ФИО"):              1,
		nullCol("public.t_part", "id"):                         1,
		commented("public.t_part", "snils", "СНИЛС"):           1,
		nullCol("public.t_part", "d"):                          1,
		commented("public.t_part_2024", "snils", "СНИЛС 2024"): 1,
		commented("public.t_part_2025", "snils", "СНИЛС"):      1,
		nullCol("public.t_parent", "id"):                       1,
		nullCol("public.t_parent", "inn"):                      1,
		commented("public.t_child", "inn", "ИНН наследника"):   1,
		nullCol("public.t_child", "extra_col"):                 1,
		nullCol("public.mv", "mv_col"):                         1,
		nullCol("tmpx.a", "tmpx_col"):                          1,
		nullCol("public.t_dropped", "keep"):                    1,
	}
	assert.Equal(t, want, got)

	all := columnCatalog(t, p, pool, []string{})
	assert.Contains(t, all, nullCol("archive_2023.a", "archived_col"), "an empty exclude list excludes nothing")
	assert.Contains(t, all, nullCol("tmp_x.a", "tmp_col"))
}

// Criterion 5: partitions without own comments add no rows and no noticeable cost.
func TestColumnCatalog_ManyPartitions(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolateEmptyPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		CREATE TABLE big (id int, fio text, n int) PARTITION BY RANGE (n);
		COMMENT ON COLUMN big.fio IS 'ФИО';
		DO $$
		BEGIN
		    FOR i IN 0..999 LOOP
		        EXECUTE format('CREATE TABLE big_%s PARTITION OF big FOR VALUES FROM (%s) TO (%s)', i, i, i + 1);
		    END LOOP;
		END $$;
	`)
	require.NoError(t, err)

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	qStr, err := query.Get(vNum, enums.QueryPiiCatalogColumns, nil)
	require.NoError(t, err)

	start := time.Now()
	cols, err := scanColumnCatalog(ctx, pool, qStr, []string{})
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Equal(t, map[colKey]int{
		nullCol("public.big", "id"):           1,
		commented("public.big", "fio", "ФИО"): 1,
		nullCol("public.big", "n"):            1,
	}, colSet(cols))
	assert.Less(t, elapsed, 5*time.Second)
}

func TestWithTxSettings_StatementTimeout(t *testing.T) {
	t.Parallel()

	pool := testinfra.IsolateEmptyPool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	var before string
	require.NoError(t, pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&before))

	err := p.withTxSettings(ctx, pool, []string{"SET LOCAL statement_timeout = '50ms'"}, func(q querier) error {
		var v any

		return q.QueryRow(ctx, "SELECT pg_sleep(2)").Scan(&v)
	})

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "57014", pgErr.Code)

	// SET LOCAL must not leak into the pooled session.
	var timeout string
	require.NoError(t, pool.QueryRow(context.WithoutCancel(ctx), "SHOW statement_timeout").Scan(&timeout))
	assert.Equal(t, before, timeout)
}
