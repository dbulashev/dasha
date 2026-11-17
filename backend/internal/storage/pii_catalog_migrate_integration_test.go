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

	// Incompressible, so TOAST cannot hide an index on the comment.
	raw := make([]byte, 5*1024)
	_, err := rand.Read(raw)
	require.NoError(t, err)

	insert := `INSERT INTO pii_catalog_pair (cluster_name, database, pair_hash, column_name, column_comment)
	    VALUES ('c1', 'db1', '\x01', 'fio', $1)`

	_, err = pool.Exec(ctx, insert, hex.EncodeToString(raw))
	require.NoError(t, err, "a comment over the btree key limit (~2704 bytes) must fit")

	_, err = pool.Exec(ctx, insert, "x")
	require.Error(t, err, "the same pair_hash twice in one database")
}
