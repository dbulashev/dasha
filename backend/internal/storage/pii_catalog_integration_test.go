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

type pairRow struct {
	Name           string
	Comment        *string
	AddedVersion   int64
	RemovedVersion *int64
}

func piiPairs(t *testing.T, s *Storage, k piicatalog.Key) []pairRow {
	t.Helper()

	rows, err := s.pool.Query(t.Context(), `
		SELECT column_name, column_comment, added_version, removed_version
		FROM pii_catalog_pair WHERE cluster_name = $1 AND database = $2
		ORDER BY id`, k.Cluster, k.Database)
	require.NoError(t, err)

	defer rows.Close()

	var out []pairRow

	for rows.Next() {
		var r pairRow
		require.NoError(t, rows.Scan(&r.Name, &r.Comment, &r.AddedVersion, &r.RemovedVersion))
		out = append(out, r)
	}

	require.NoError(t, rows.Err())

	return out
}

func countRows(t *testing.T, s *Storage, table string) int {
	t.Helper()

	var n int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))

	return n
}

// Criterion 3.
func TestPIIApply_SameSetNoNewVersion(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	snap := snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "fio", Comment: strp("ФИО")})

	res, err := s.ApplyPIISnapshot(ctx, piiKey, snap)
	require.NoError(t, err)
	require.NotZero(t, res.VersionID)
	assert.Equal(t, 2, res.Added)

	first, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)

	res, err = s.ApplyPIISnapshot(ctx, piiKey, snapOf(snap.Pairs[1].Pair, snap.Pairs[0].Pair))
	require.NoError(t, err)
	assert.Zero(t, res.VersionID)

	second, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, countRows(t, s, "pii_catalog_version"))
	assert.Equal(t, *first[piiKey].CurrentVersionID, *second[piiKey].CurrentVersionID)
	assert.Equal(t, *first[piiKey].LastChangedAt, *second[piiKey].LastChangedAt)
	assert.True(t, second[piiKey].LastSuccessAt.After(*first[piiKey].LastSuccessAt))
}

// Criterion 4 (storage half): a new comment is removed + added in one version.
func TestPIIApply_CommentChange(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	v1, err := s.ApplyPIISnapshot(ctx, piiKey, snapOf(piicatalog.Pair{Name: "id"}))
	require.NoError(t, err)

	v2, err := s.ApplyPIISnapshot(ctx, piiKey, snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "snils"}))
	require.NoError(t, err)
	assert.Equal(t, 1, v2.Added)

	v3, err := s.ApplyPIISnapshot(ctx, piiKey,
		snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "snils", Comment: strp("СНИЛС")}))
	require.NoError(t, err)
	assert.Equal(t, 1, v3.Added)
	assert.Equal(t, 1, v3.Removed)

	assert.Equal(t, []pairRow{
		{Name: "id", AddedVersion: v1.VersionID},
		{Name: "snils", AddedVersion: v2.VersionID, RemovedVersion: &v3.VersionID},
		{Name: "snils", Comment: strp("СНИЛС"), AddedVersion: v3.VersionID},
	}, piiPairs(t, s, piiKey))

	st, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, v3.VersionID, *st[piiKey].CurrentVersionID)
	assert.Equal(t, 2, st[piiKey].PairCount)
	assert.Nil(t, st[piiKey].LastError)
}

func TestPIIApply_EmptyDatabaseGetsVersion(t *testing.T) {
	s := newPIITestStorage(t)

	res, err := s.ApplyPIISnapshot(t.Context(), piiKey, snapOf())
	require.NoError(t, err)
	assert.NotZero(t, res.VersionID)
}

func TestPIIApply_FailureThenSuccessClearsError(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	require.NoError(t, s.RecordPIIFailure(ctx, piiKey, time.Now().UTC(), "h1: timeout"))

	st, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "h1: timeout", *st[piiKey].LastError)
	assert.Nil(t, st[piiKey].CurrentVersionID)

	_, err = s.ApplyPIISnapshot(ctx, piiKey, snapOf(piicatalog.Pair{Name: "id"}))
	require.NoError(t, err)

	st, err = s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.Nil(t, st[piiKey].LastError)

	require.NoError(t, s.RecordPIIFailure(ctx, piiKey, time.Now().UTC(), "h1: timeout"))

	st, err = s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.NotNil(t, st[piiKey].CurrentVersionID, "a failure keeps the last known catalog")
}

func TestPIIApply_LongComment(t *testing.T) {
	s := newPIITestStorage(t)
	long := strings.Repeat("комментарий ", 1000)

	_, err := s.ApplyPIISnapshot(t.Context(), piiKey, snapOf(piicatalog.Pair{Name: "fio", Comment: &long}))
	require.NoError(t, err)

	rows := piiPairs(t, s, piiKey)
	require.Len(t, rows, 1)
	assert.Equal(t, long, *rows[0].Comment)
}

func TestPIIRetention(t *testing.T) {
	s := newPIITestStorage(t)
	ctx := t.Context()

	live := piiKey
	failing := piicatalog.Key{Cluster: "c1", Database: "failing"}
	gone := piicatalog.Key{Cluster: "c1", Database: "gone"}

	for _, k := range []piicatalog.Key{live, failing, gone} {
		_, err := s.ApplyPIISnapshot(ctx, k, snapOf(piicatalog.Pair{Name: "id"}, piicatalog.Pair{Name: "old"}))
		require.NoError(t, err)
	}

	cur, err := s.ApplyPIISnapshot(ctx, live, snapOf(piicatalog.Pair{Name: "id"}))
	require.NoError(t, err)

	_, err = s.pool.Exec(ctx, `INSERT INTO pii_catalog_access_log (operation, decision) VALUES ('op', 'allowed')`)
	require.NoError(t, err)

	old := time.Now().UTC().AddDate(0, 0, -100)

	for _, q := range []string{
		`UPDATE pii_catalog_pair SET added_at = $1::timestamptz,
		        removed_at = CASE WHEN removed_at IS NULL THEN NULL ELSE $1::timestamptz END`,
		`UPDATE pii_catalog_version SET created_at = $1::timestamptz`,
		`UPDATE pii_catalog_access_log SET at = $1::timestamptz`,
		`UPDATE pii_catalog_status SET last_success_at = $1::timestamptz, last_changed_at = $1::timestamptz,
		        last_attempt_at = CASE WHEN database = 'gone' THEN $1::timestamptz ELSE now() END`,
	} {
		_, err = s.pool.Exec(ctx, q, old)
		require.NoError(t, err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -90)

	stats, err := s.PIIRetention(ctx, cutoff, cutoff)
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.OrphanDatabases)
	assert.EqualValues(t, 1, stats.AccessLog)

	st, err := s.ListPIIStatus(ctx)
	require.NoError(t, err)
	assert.NotContains(t, st, gone)
	require.Contains(t, st, live)
	require.Contains(t, st, failing, "a database still being attempted is not an orphan")

	assert.Equal(t, []pairRow{{Name: "id", AddedVersion: piiPairs(t, s, live)[0].AddedVersion}}, piiPairs(t, s, live),
		"only the removed pair is gone")
	assert.Len(t, piiPairs(t, s, failing), 2)
	assert.Empty(t, piiPairs(t, s, gone))

	var versions []int64

	rows, err := s.pool.Query(ctx, `SELECT id FROM pii_catalog_version WHERE database = 'app'`)
	require.NoError(t, err)

	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		versions = append(versions, id)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{cur.VersionID}, versions, "the current version survives the cutoff")
	assert.Equal(t, 0, countRows(t, s, "pii_catalog_access_log"))
}
