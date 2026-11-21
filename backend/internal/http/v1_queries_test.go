package http

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/querystats"
	"github.com/dbulashev/dasha/internal/repository"
)

type statsSnapshots map[uuid.UUID]querystats.Snapshot

func (f statsSnapshots) GetSnapshotReport(_ context.Context, id uuid.UUID) (querystats.Snapshot, error) {
	s, ok := f[id]
	if !ok {
		return querystats.Snapshot{}, querystats.ErrSnapshotNotFound //nolint:exhaustruct
	}

	return s, nil
}

type statsLive struct {
	reports []dto.QueryReport
	err     error
}

func (f statsLive) GetQueriesReport(context.Context, string, string, string, []string, *int64) ([]dto.QueryReport, error) {
	return f.reports, f.err
}

func statsHandlers(snaps statsSnapshots, live statsLive) *Handlers {
	return &Handlers{queryStats: querystats.NewService(snaps, live)} //nolint:exhaustruct
}

func compareReq(a uuid.UUID) serverhttp.GetQueriesCompareRequestObject {
	return serverhttp.GetQueriesCompareRequestObject{
		Params: serverhttp.GetQueriesCompareParams{ //nolint:exhaustruct
			ClusterName: "c1", Instance: "h1", Database: "app", SnapshotA: a,
		},
	}
}

func TestQueryStatsWithoutStorage(t *testing.T) {
	t.Parallel()

	h := &Handlers{} //nolint:exhaustruct

	resp, err := h.GetQueriesCompare(context.Background(), compareReq(uuid.New()))
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetQueriesCompare501Response{}, resp)

	sResp, err := h.GetSnapshot(context.Background(), serverhttp.GetSnapshotRequestObject{Id: uuid.New()}) //nolint:exhaustruct
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetSnapshot501Response{}, sResp)
}

func TestGetQueriesCompareErrors(t *testing.T) {
	t.Parallel()

	legacy, scoped := uuid.New(), uuid.New()
	snaps := statsSnapshots{
		legacy: {Attributed: false}, //nolint:exhaustruct
		scoped: {Attributed: true},  //nolint:exhaustruct
	}

	resp, err := statsHandlers(snaps, statsLive{}).GetQueriesCompare(context.Background(), compareReq(uuid.New())) //nolint:exhaustruct
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetQueriesCompare404Response{}, resp)

	resp, err = statsHandlers(snaps, statsLive{}).GetQueriesCompare(context.Background(), compareReq(legacy)) //nolint:exhaustruct
	require.NoError(t, err)
	require.IsType(t, serverhttp.GetQueriesCompare409JSONResponse{}, resp)
	assert.Equal(t, msgIncomparableSnapshots, resp.(serverhttp.GetQueriesCompare409JSONResponse).Message)

	live := statsLive{err: repository.ErrNotFound} //nolint:exhaustruct
	resp, err = statsHandlers(snaps, live).GetQueriesCompare(context.Background(), compareReq(scoped))
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetQueriesCompare404Response{}, resp)
}

func TestGetQueriesCompareMapsPairs(t *testing.T) {
	t.Parallel()

	a := uuid.New()
	snaps := statsSnapshots{a: {Attributed: true, Reports: []dto.QueryReport{
		{QueryID: 7, Datname: "app", Query: "select 1"}, //nolint:exhaustruct
	}}}
	live := statsLive{reports: []dto.QueryReport{ //nolint:exhaustruct
		{QueryID: 8, Datname: "app", Query: "select 2"}, //nolint:exhaustruct
	}}

	resp, err := statsHandlers(snaps, live).GetQueriesCompare(context.Background(), compareReq(a))
	require.NoError(t, err)

	items, ok := resp.(serverhttp.GetQueriesCompare200JSONResponse)
	require.True(t, ok, "got %T", resp)
	require.Len(t, items, 2)

	byID := map[string]serverhttp.QueryCompareItem{}
	for _, it := range items {
		byID[it.QueryID] = it
	}

	assert.NotNil(t, byID["7"].Left)
	assert.Nil(t, byID["7"].Right)
	assert.Nil(t, byID["8"].Left)
	assert.NotNil(t, byID["8"].Right)
	assert.Equal(t, "app", *byID["8"].Datname)
	assert.NotEmpty(t, byID["8"].Query)
}

func TestGetSnapshotNotFound(t *testing.T) {
	t.Parallel()

	resp, err := statsHandlers(statsSnapshots{}, statsLive{}).GetSnapshot( //nolint:exhaustruct
		context.Background(), serverhttp.GetSnapshotRequestObject{Id: uuid.New()}) //nolint:exhaustruct
	require.NoError(t, err)
	assert.IsType(t, serverhttp.GetSnapshot404Response{}, resp)
}
