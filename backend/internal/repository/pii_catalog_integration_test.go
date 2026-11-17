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

type pairKey struct {
	name    string
	comment string
	null    bool
}

func columnCatalog(t *testing.T, p *PgxPool, pool *pgxpool.Pool, exclude []string) map[pairKey]int {
	t.Helper()

	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	qStr, err := query.Get(vNum, enums.QueryPiiCatalogColumns, nil)
	require.NoError(t, err)

	pairs, err := scanColumnCatalog(ctx, pool, qStr, exclude)
	require.NoError(t, err)

	return pairSet(pairs)
}

func pairSet(pairs []piicatalog.Pair) map[pairKey]int {
	out := map[pairKey]int{}

	for _, pr := range pairs {
		k := pairKey{name: pr.Name, null: pr.Comment == nil}
		if pr.Comment != nil {
			k.comment = *pr.Comment
		}

		out[k]++
	}

	return out
}

func nullPair(name string) pairKey           { return pairKey{name: name, null: true} }
func commented(name, comment string) pairKey { return pairKey{name: name, comment: comment} }

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

	want := map[pairKey]int{
		nullPair("id"):                     1,
		commented("fio", "ФИО"):            1,
		commented("snils", "СНИЛС"):        1,
		commented("snils", "СНИЛС 2024"):   1,
		nullPair("d"):                      1,
		nullPair("inn"):                    1,
		commented("inn", "ИНН наследника"): 1,
		nullPair("extra_col"):              1,
		nullPair("mv_col"):                 1,
		nullPair("tmpx_col"):               1,
		nullPair("keep"):                   1,
	}
	assert.Equal(t, want, got)

	all := columnCatalog(t, p, pool, []string{})
	assert.Contains(t, all, nullPair("archived_col"), "an empty exclude list excludes nothing")
	assert.Contains(t, all, nullPair("tmp_col"))
}

// Criterion 5: partitions add no pairs and no noticeable cost.
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
	pairs, err := scanColumnCatalog(ctx, pool, qStr, []string{})
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Equal(t, map[pairKey]int{nullPair("id"): 1, commented("fio", "ФИО"): 1, nullPair("n"): 1}, pairSet(pairs))
	assert.Less(t, elapsed, 5*time.Second)

	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+qStr, []string{})
	require.NoError(t, err)

	defer rows.Close()

	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		t.Log(line)
	}

	require.NoError(t, rows.Err())
	t.Logf("catalog query took %s", elapsed)
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
