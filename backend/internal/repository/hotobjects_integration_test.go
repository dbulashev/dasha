//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/testinfra"
)

// TestHotSampleRollup exercises the hash-partition rollup baked into the
// hot/sample_* queries: the recursive CTE and the part_sig churn signature — the
// piece the demo lab confirms by hand but nothing else covers.
func TestHotSampleRollup(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	// Pure hash (4 leaves), range→hash (2 months × 2 hash subparts) and a plain
	// table — the three rollup cases in one fixture.
	_, err := pool.Exec(ctx, `
		CREATE TABLE hot_hash_test (id int, k int, PRIMARY KEY (k, id)) PARTITION BY HASH (k);
		CREATE TABLE hot_hash_test_p0 PARTITION OF hot_hash_test FOR VALUES WITH (MODULUS 4, REMAINDER 0);
		CREATE TABLE hot_hash_test_p1 PARTITION OF hot_hash_test FOR VALUES WITH (MODULUS 4, REMAINDER 1);
		CREATE TABLE hot_hash_test_p2 PARTITION OF hot_hash_test FOR VALUES WITH (MODULUS 4, REMAINDER 2);
		CREATE TABLE hot_hash_test_p3 PARTITION OF hot_hash_test FOR VALUES WITH (MODULUS 4, REMAINDER 3);
		INSERT INTO hot_hash_test SELECT g, g FROM generate_series(1, 200) g;

		CREATE TABLE hot_rh_test (id int, k int, d date, PRIMARY KEY (d, k, id)) PARTITION BY RANGE (d);
		CREATE TABLE hot_rh_test_2026_01 PARTITION OF hot_rh_test
			FOR VALUES FROM ('2026-01-01') TO ('2026-02-01') PARTITION BY HASH (k);
		CREATE TABLE hot_rh_test_2026_01_p0 PARTITION OF hot_rh_test_2026_01 FOR VALUES WITH (MODULUS 2, REMAINDER 0);
		CREATE TABLE hot_rh_test_2026_01_p1 PARTITION OF hot_rh_test_2026_01 FOR VALUES WITH (MODULUS 2, REMAINDER 1);
		CREATE TABLE hot_rh_test_2026_02 PARTITION OF hot_rh_test
			FOR VALUES FROM ('2026-02-01') TO ('2026-03-01') PARTITION BY HASH (k);
		CREATE TABLE hot_rh_test_2026_02_p0 PARTITION OF hot_rh_test_2026_02 FOR VALUES WITH (MODULUS 2, REMAINDER 0);
		CREATE TABLE hot_rh_test_2026_02_p1 PARTITION OF hot_rh_test_2026_02 FOR VALUES WITH (MODULUS 2, REMAINDER 1);
		INSERT INTO hot_rh_test SELECT g, g, '2026-01-15'::date FROM generate_series(1, 60) g;
		INSERT INTO hot_rh_test SELECT g, g, '2026-02-15'::date FROM generate_series(1, 40) g;

		CREATE TABLE hot_plain_test (id int PRIMARY KEY);
		INSERT INTO hot_plain_test SELECT generate_series(1, 10);
	`)
	require.NoError(t, err)

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	tableQ, err := query.Get(vNum, enums.QueryHotSampleTables, nil)
	require.NoError(t, err)

	readTables := func() (map[string]hotobjects.AnchorRow, error) {
		rows, _, _, err := scanHotSample(ctx, pool, tableQ, hotobjects.KindTable, nil, nil)
		if err != nil {
			return nil, err
		}

		m := make(map[string]hotobjects.AnchorRow, len(rows))
		for _, r := range rows {
			m[r.Object] = r
		}

		return m, nil
	}

	tables, err := readTables()
	require.NoError(t, err)

	// Hash leaves (pure and subpartitioned) fold into their parent — none appear.
	for _, leaf := range []string{
		"hot_hash_test_p0", "hot_hash_test_p3",
		"hot_rh_test_2026_01_p0", "hot_rh_test_2026_02_p1",
	} {
		assert.NotContains(t, tables, leaf, "hash leaf must fold into its rollup target")
	}

	hash, ok := tables["hot_hash_test"]
	require.True(t, ok, "hash partitions roll up to the parent")
	assert.NotEmpty(t, hash.PartSig, "rolled-up target carries a partition signature")

	// Range levels stay per-leaf; the hash subparts collapse into their month.
	assert.Contains(t, tables, "hot_rh_test_2026_01", "range partition kept as its own target")
	assert.Contains(t, tables, "hot_rh_test_2026_02", "second range partition kept")
	assert.Contains(t, tables, "hot_plain_test", "plain table present")

	// Indexes roll up symmetrically: the partitioned PK index is one target, its
	// per-partition children never appear.
	indexQ, err := query.Get(vNum, enums.QueryHotSampleIndexes, nil)
	require.NoError(t, err)

	indexRows, _, _, err := scanHotSample(ctx, pool, indexQ, hotobjects.KindIndex, nil, nil)
	require.NoError(t, err)

	indexes := make(map[string]hotobjects.AnchorRow, len(indexRows))
	for _, r := range indexRows {
		indexes[r.Object] = r
	}

	pk, ok := indexes["hot_hash_test_pkey"]
	require.True(t, ok, "partitioned PK index rolled up to the parent index")
	assert.Equal(t, "hot_hash_test", pk.TableName, "rolled-up index reports the parent table")
	assert.NotContains(t, indexes, "hot_hash_test_p0_pkey", "child index folded into the parent")

	// Summation: the parent's insert counter sums all four hash leaves (200), once
	// the stats subsystem has flushed (Eventually covers PG14's async collector).
	require.Eventually(t, func() bool {
		m, err := readTables()
		return err == nil && m["hot_hash_test"].Counters["n_tup_ins"] == 200
	}, 5*time.Second, 100*time.Millisecond, "hash parent sums its leaves' n_tup_ins")

	// Churn guard: changing the partition set changes part_sig, so BuildSnapshot
	// skips the interval instead of silently under-counting a dropped partition.
	_, err = pool.Exec(ctx, `DROP TABLE hot_hash_test_p3`)
	require.NoError(t, err)

	after, err := readTables()
	require.NoError(t, err)
	assert.NotEqual(t, hash.PartSig, after["hot_hash_test"].PartSig,
		"part_sig changes when a partition is dropped")
}
