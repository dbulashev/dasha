//go:build integration

package storage

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestPIICatalogDDL(t *testing.T) {
	pool := testinfra.IsolateEmptyPool(t)
	ctx := t.Context()

	// Twice: `dasha migrate` runs on every rollout.
	for range 2 {
		for _, ddl := range piiCatalogDDL {
			_, err := pool.Exec(ctx, ddl)
			require.NoError(t, err, ddl)
		}
	}

	insert := `INSERT INTO pii_catalog_pair
	    (cluster_name, database, pair_hash, column_name, column_comment, added_version, added_at)
	    VALUES ('c1', 'db1', '\x01', 'snils', $1, 1, now())`

	_, err := pool.Exec(ctx, insert, nil)
	require.NoError(t, err)

	// One current row per (cluster, database, pair_hash)...
	_, err = pool.Exec(ctx, insert, nil)
	require.Error(t, err, "duplicate current pair must violate uq_pii_catalog_pair_current")

	// ...but a removed row does not block re-adding the same pair.
	_, err = pool.Exec(ctx, `UPDATE pii_catalog_pair SET removed_version = 2, removed_at = now()`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, insert, nil)
	require.NoError(t, err)

	// A comment past the btree key limit is fine: only its hash is indexed.
	long := make([]byte, 10*1024)
	for i := range long {
		long[i] = 'x'
	}

	_, err = pool.Exec(ctx, `INSERT INTO pii_catalog_pair
	    (cluster_name, database, pair_hash, column_name, column_comment, added_version, added_at)
	    VALUES ('c1', 'db1', '\x02', 'fio', $1, 3, now())`, string(long))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO pii_catalog_access_log (operation, decision, remote_ip)
	    VALUES ('getPiiCatalogStatus', 'denied', '10.0.0.1')`)
	require.NoError(t, err)
}
