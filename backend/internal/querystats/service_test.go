package querystats

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/dto"
)

type fakeSnapshots map[uuid.UUID]Snapshot

func (f fakeSnapshots) GetSnapshotReport(_ context.Context, id uuid.UUID) (Snapshot, error) {
	s, ok := f[id]
	if !ok {
		return Snapshot{}, ErrSnapshotNotFound //nolint:exhaustruct
	}

	return s, nil
}

type fakeLive struct {
	reports []dto.QueryReport
	err     error
	calls   int
	exclude []string
}

func (f *fakeLive) GetQueriesReport(_ context.Context, _, _, _ string, excludeUsers []string, _ *int64) ([]dto.QueryReport, error) {
	f.calls++
	f.exclude = excludeUsers

	return f.reports, f.err
}

func TestCompareRefusesMixedGenerationsBeforeLiveRead(t *testing.T) {
	t.Parallel()

	a := uuid.New()
	live := &fakeLive{} //nolint:exhaustruct
	svc := NewService(fakeSnapshots{a: {Reports: nil, Attributed: false}}, live)

	_, err := svc.Compare(context.Background(), CompareRequest{SnapshotA: a, Database: "app"}) //nolint:exhaustruct
	require.ErrorIs(t, err, ErrIncomparable)
	assert.Zero(t, live.calls, "live statistics must not be read on a refusal")
}

func TestCompareMissingSnapshot(t *testing.T) {
	t.Parallel()

	a, b := uuid.New(), uuid.New()
	svc := NewService(fakeSnapshots{a: {Attributed: true}}, &fakeLive{}) //nolint:exhaustruct

	_, err := svc.Compare(context.Background(), CompareRequest{SnapshotA: uuid.New()}) //nolint:exhaustruct
	require.ErrorIs(t, err, ErrSnapshotNotFound)

	_, err = svc.Compare(context.Background(), CompareRequest{SnapshotA: a, SnapshotB: &b}) //nolint:exhaustruct
	require.ErrorIs(t, err, ErrSnapshotNotFound)
}

func TestCompareLiveErrorPassesThrough(t *testing.T) {
	t.Parallel()

	a := uuid.New()
	svc := NewService(fakeSnapshots{a: {Attributed: true}}, &fakeLive{err: dto.ErrNotFound}) //nolint:exhaustruct

	_, err := svc.Compare(context.Background(), CompareRequest{SnapshotA: a}) //nolint:exhaustruct
	require.ErrorIs(t, err, dto.ErrNotFound)
}

func TestCompareWithLiveExcludesUsersOnBothSides(t *testing.T) {
	t.Parallel()

	a := uuid.New()
	snaps := fakeSnapshots{a: {Attributed: true, Reports: []dto.QueryReport{
		{QueryID: 1, Datname: "app", Usernames: []string{"app"}},       //nolint:exhaustruct
		{QueryID: 2, Datname: "app", Usernames: []string{"reporting"}}, //nolint:exhaustruct
	}}}
	live := &fakeLive{reports: []dto.QueryReport{{QueryID: 1, Datname: "app"}}} //nolint:exhaustruct

	pairs, err := NewService(snaps, live).Compare(context.Background(), CompareRequest{ //nolint:exhaustruct
		SnapshotA: a, Database: "app", ExcludeUsers: []string{"reporting"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"reporting"}, live.exclude)
	require.Len(t, pairs, 1)
	assert.Equal(t, Key{QueryID: 1, Datname: "app"}, pairs[0].Key)
	assert.NotNil(t, pairs[0].Left)
	assert.NotNil(t, pairs[0].Right)
}

func TestCompareTwoSnapshotsTakesKeyAndTextFromPresentSide(t *testing.T) {
	t.Parallel()

	a, b := uuid.New(), uuid.New()
	snaps := fakeSnapshots{
		a: {Attributed: true, Reports: []dto.QueryReport{
			{QueryID: 1, Datname: "app", Query: "select 1"}, //nolint:exhaustruct
		}},
		b: {Attributed: true, Reports: []dto.QueryReport{
			{QueryID: 1, Datname: "app", Query: "select 1 -- b"}, //nolint:exhaustruct
			{QueryID: 2, Datname: "app", Query: "select 2"},      //nolint:exhaustruct
			{QueryID: 3, Datname: "other", Query: "select 3"},    //nolint:exhaustruct
		}},
	}
	live := &fakeLive{} //nolint:exhaustruct

	pairs, err := NewService(snaps, live).Compare(context.Background(), CompareRequest{ //nolint:exhaustruct
		SnapshotA: a, SnapshotB: &b, Database: "app",
	})
	require.NoError(t, err)
	assert.Zero(t, live.calls)

	byID := make(map[int64]Pair, len(pairs))
	for _, p := range pairs {
		byID[p.Key.QueryID] = p
	}

	require.Len(t, byID, 2, "database scope must drop the other database")
	assert.Equal(t, "select 1", byID[1].Query, "the left text wins")
	assert.Nil(t, byID[2].Left)
	assert.Equal(t, Key{QueryID: 2, Datname: "app"}, byID[2].Key)
	assert.Equal(t, "select 2", byID[2].Query)
}

func TestCompareLegacyPairIsInstanceWide(t *testing.T) {
	t.Parallel()

	a, b := uuid.New(), uuid.New()
	snaps := fakeSnapshots{
		a: {Reports: []dto.QueryReport{{QueryID: 1}}}, //nolint:exhaustruct
		b: {Reports: []dto.QueryReport{{QueryID: 1}}}, //nolint:exhaustruct
	}

	pairs, err := NewService(snaps, &fakeLive{}).Compare(context.Background(), CompareRequest{ //nolint:exhaustruct
		SnapshotA: a, SnapshotB: &b, Database: "app",
	})
	require.NoError(t, err)
	require.Len(t, pairs, 1, "rows without a database must survive a named database")
}

func TestSnapshotScope(t *testing.T) {
	t.Parallel()

	legacy, scoped := uuid.New(), uuid.New()
	rows := []dto.QueryReport{
		{QueryID: 1, Datname: "app"},   //nolint:exhaustruct
		{QueryID: 2, Datname: "other"}, //nolint:exhaustruct
	}
	svc := NewService(fakeSnapshots{
		legacy: {Reports: []dto.QueryReport{{QueryID: 1}}}, //nolint:exhaustruct
		scoped: {Reports: rows, Attributed: true},
	}, &fakeLive{}) //nolint:exhaustruct

	got, err := svc.Snapshot(context.Background(), scoped, nil, "app")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, int64(1), got[0].QueryID)

	got, err = svc.Snapshot(context.Background(), legacy, nil, "app")
	require.NoError(t, err)
	assert.Len(t, got, 1, "a legacy snapshot stays instance-wide")

	_, err = svc.Snapshot(context.Background(), uuid.New(), nil, "app")
	require.ErrorIs(t, err, ErrSnapshotNotFound)
}
