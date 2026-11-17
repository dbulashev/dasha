//go:build integration

package storage

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestPIICatalogDDL(t *testing.T) {
	pool := testinfra.IsolateEmptyPool(t)
	ctx := t.Context()

	// Twice: migrate runs on every rollout.
	s := &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}
	for range 2 {
		require.NoError(t, s.migrate(ctx, zap.NewNop()))
	}

	insert := `INSERT INTO pii_catalog_pair
	    (cluster_name, database, pair_hash, column_name, column_comment, added_version, added_at)
	    VALUES ('c1', 'db1', '\x01', 'snils', $1, 1, now())`

	_, err := pool.Exec(ctx, insert, nil)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, insert, nil)
	require.Error(t, err, "duplicate current pair must violate uq_pii_catalog_pair_current")

	_, err = pool.Exec(ctx, `UPDATE pii_catalog_pair SET removed_version = 2, removed_at = now()`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, insert, nil)
	require.NoError(t, err)

	// Incompressible, so TOAST cannot hide an index on the comment.
	raw := make([]byte, 5*1024)
	_, err = rand.Read(raw)
	require.NoError(t, err)

	long := hex.EncodeToString(raw)

	_, err = pool.Exec(ctx, `INSERT INTO pii_catalog_pair
	    (cluster_name, database, pair_hash, column_name, column_comment, added_version, added_at)
	    VALUES ('c1', 'db1', '\x02', 'fio', $1, 3, now())`, long)
	require.NoError(t, err)

	// btree key limit is ~2704 bytes.
	var stored int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT pg_column_size(column_comment) FROM pii_catalog_pair WHERE pair_hash = '\x02'`).Scan(&stored))
	require.Greater(t, stored, 2704, "comment was compressed under the btree limit")

	_, err = pool.Exec(ctx, `INSERT INTO pii_catalog_access_log (operation, decision, remote_ip)
	    VALUES ('getPiiCatalogStatus', 'denied', '10.0.0.1')`)
	require.NoError(t, err)
}
