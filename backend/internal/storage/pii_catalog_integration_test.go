//go:build integration

package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/piicatalog"
	"github.com/dbulashev/dasha/internal/testinfra"
)

func newPIITestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolateEmptyPool(t)

	for _, ddl := range piiCatalogDDL {
		_, err := pool.Exec(t.Context(), ddl)
		require.NoError(t, err)
	}

	return &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}
}

func strp(s string) *string { return &s }

func snapOf(pairs ...piicatalog.Pair) piicatalog.Snapshot {
	return piicatalog.NewSnapshot(pairs, "h1", 170000, time.Now().UTC())
}

var piiKey = piicatalog.Key{Cluster: "c1", Database: "app"}

func countRows(t *testing.T, s *Storage, table string) int {
	t.Helper()

	var n int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))

	return n
}

// Criterion 3.
func TestPIIApply_SameSetIsNotAChange(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	snap := snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "fio", Comment: strp("ФИО")})

	changed, err := s.ApplyPIISnapshot(ctx, piiKey, snap)
	require.NoError(t, err)
	assert.True(t, changed)

	first, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)

	changed, err = s.ApplyPIISnapshot(ctx, piiKey, snapOf(snap.Pairs[1].Pair, snap.Pairs[0].Pair))
	require.NoError(t, err)
	assert.False(t, changed)

	second, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)

	assert.Equal(t, *first[piiKey].LastChangedAt, *second[piiKey].LastChangedAt)
	assert.True(t, second[piiKey].LastSuccessAt.After(*first[piiKey].LastSuccessAt))
}

// Criterion 4 (storage half): a new comment replaces the pair.
func TestPIIApply_CommentChange(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	_, err := s.ApplyPIISnapshot(ctx, piiKey, snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "snils"}))
	require.NoError(t, err)

	changed, err := s.ApplyPIISnapshot(ctx, piiKey,
		snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "snils", Comment: strp("СНИЛС")}))
	require.NoError(t, err)
	assert.True(t, changed)

	cat, err := s.PIICatalog(ctx, piiKey)
	require.NoError(t, err)
	assert.Equal(t, []piicatalog.Pair{{Name: "id"}, {Name: "snils", Comment: strp("СНИЛС")}}, cat.Pairs)

	st, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, st[piiKey].PairCount)
	assert.Equal(t, cat.SetHash, st[piiKey].SetHash)
}

func TestPIIApply_FailureKeepsCatalog(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	require.NoError(t, s.RecordPIIFailure(ctx, piiKey, time.Now().UTC(), "h1: timeout"))

	_, err := s.PIICatalog(ctx, piiKey)
	require.ErrorIs(t, err, piicatalog.ErrNotCollected)

	_, err = s.ApplyPIISnapshot(ctx, piiKey, snapOf(piicatalog.Pair{Name: "id"}))
	require.NoError(t, err)

	st, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.Nil(t, st[piiKey].LastError)

	require.NoError(t, s.RecordPIIFailure(ctx, piiKey, time.Now().UTC(), "h1: timeout"))

	cat, err := s.PIICatalog(ctx, piiKey)
	require.NoError(t, err, "a failure keeps the last known catalog")
	assert.Len(t, cat.Pairs, 1)
}

func TestPIIApply_EmptyDatabaseIsCollected(t *testing.T) {
	s := newPIITestStorage(t)

	_, err := s.ApplyPIISnapshot(t.Context(), piiKey, snapOf())
	require.NoError(t, err)

	cat, err := s.PIICatalog(t.Context(), piiKey)
	require.NoError(t, err)
	assert.Empty(t, cat.Pairs)
}

func TestPIICatalog_ByteOrderAndLongComment(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()
	long := strings.Repeat("комментарий ", 1000)

	_, err := s.PIICatalog(ctx, piiKey)
	require.ErrorIs(t, err, piicatalog.ErrNotFound)

	_, err = s.ApplyPIISnapshot(ctx, piiKey, snapOf(
		piicatalog.Pair{Name: "a"}, piicatalog.Pair{Name: "B"}, piicatalog.Pair{Name: "_x", Comment: strp("")},
		piicatalog.Pair{Name: "a", Comment: &long},
	))
	require.NoError(t, err)

	cat, err := s.PIICatalog(ctx, piiKey)
	require.NoError(t, err)
	assert.Equal(t, []piicatalog.Pair{
		{Name: "B"}, {Name: "_x", Comment: strp("")}, {Name: "a"}, {Name: "a", Comment: &long},
	}, cat.Pairs)
}

func TestPIIRetention(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	live := piiKey
	failing := piicatalog.Key{Cluster: "c1", Database: "failing"}
	gone := piicatalog.Key{Cluster: "c1", Database: "gone"}

	for _, k := range []piicatalog.Key{live, failing, gone} {
		_, err := s.ApplyPIISnapshot(ctx, k, snapOf(piicatalog.Pair{Name: "id"}))
		require.NoError(t, err)
	}

	old := time.Now().UTC().AddDate(0, 0, -100)

	_, err := s.pool.Exec(ctx, `
		UPDATE pii_catalog_status SET last_success_at = $1::timestamptz,
		       last_attempt_at = CASE WHEN database = 'gone' THEN $1::timestamptz ELSE now() END`, old)
	require.NoError(t, err)

	orphans, err := s.PIIRetention(ctx, time.Now().UTC().AddDate(0, 0, -90))
	require.NoError(t, err)
	assert.EqualValues(t, 1, orphans)

	st, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.NotContains(t, st, gone)
	assert.Contains(t, st, live)
	assert.Contains(t, st, failing, "a database still being attempted is not an orphan")
	assert.Equal(t, 2, countRows(t, s, "pii_catalog_pair"))
}
