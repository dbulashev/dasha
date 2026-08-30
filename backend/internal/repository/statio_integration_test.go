//go:build integration

package repository

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/statio"
	"github.com/dbulashev/dasha/internal/testinfra"
)

// TestStatioSnapshotTemplate runs the pg_stat_io template against the live
// server: it is the only check that the per-version column sets (op_bytes
// before PG 18, byte counters from 18 on) actually exist where the override
// boundary says they do.
func TestStatioSnapshotTemplate(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	if vNum < statio.MinVersionNum {
		t.Skip("pg_stat_io requires PG16+")
	}

	qStr, err := query.Get(vNum, enums.QueryStatioSnapshot, nil)
	require.NoError(t, err)

	rows, err := pool.Query(ctx, qStr)
	require.NoError(t, err)
	defer rows.Close()

	snap := statio.Snapshot{VersionNum: vNum}

	for rows.Next() {
		var (
			r        statio.Row
			reset    pgtype.Timestamptz
			opBytes  pgtype.Int4
			counters []byte
		)

		require.NoError(t, rows.Scan(&r.BackendType, &r.Object, &r.Context, &reset,
			&snap.TrackIOTiming, &snap.TrackWALIOTiming, &opBytes, &counters))

		if opBytes.Valid {
			v := int(opBytes.Int32)
			snap.OpBytes = &v
		}

		require.NoError(t, json.Unmarshal(counters, &r.Values))

		snap.Rows = append(snap.Rows, r)
	}

	require.NoError(t, rows.Err())
	require.NotEmpty(t, snap.Rows, "pg_stat_io always reports the full matrix")

	// Rows are never filtered: the shape of a snapshot must not depend on load,
	// or a row would appear mid-series and lose its baseline.
	byKey := map[statio.Key]statio.Row{}
	for _, r := range snap.Rows {
		byKey[r.Key] = r
		assert.NotEmpty(t, r.BackendType)
		assert.NotEmpty(t, r.Object)
		assert.NotEmpty(t, r.Context)
	}

	assert.Len(t, byKey, len(snap.Rows), "every backend_type × object × context appears once")

	if vNum < 180000 {
		require.NotNil(t, snap.OpBytes, "PG 16/17 report op_bytes")
		assert.Positive(t, *snap.OpBytes)

		for _, r := range snap.Normalized().Rows {
			if reads, ok := r.Values["reads"]; ok {
				assert.EqualValues(t, reads*int64(*snap.OpBytes), r.Values["read_bytes"])
			}
		}

		return
	}

	assert.Nil(t, snap.OpBytes, "PG 18+ dropped op_bytes for per-operation byte counters")

	var sawByteCounter bool

	for _, r := range snap.Rows {
		if _, ok := r.Values["read_bytes"]; ok {
			sawByteCounter = true
			break
		}
	}

	assert.True(t, sawByteCounter, "PG 18+ reports read_bytes directly")
}
