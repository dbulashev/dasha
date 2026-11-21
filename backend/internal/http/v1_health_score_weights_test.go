package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/healthscore"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

type weightsStore struct {
	rec     *healthscore.WeightsRecord
	readErr error
}

func (f *weightsStore) GetHealthWeights(context.Context, string) (*healthscore.WeightsRecord, error) {
	return f.rec, f.readErr
}

func (f *weightsStore) UpsertHealthWeights(_ context.Context, _ string, w health.Weights, user string) error {
	f.rec = &healthscore.WeightsRecord{Weights: w, UpdatedAt: time.Unix(1, 0), UpdatedBy: user}

	return nil
}

func (f *weightsStore) DeleteHealthWeights(context.Context, string) error {
	f.rec = nil

	return nil
}

func (*weightsStore) GetColdTables(context.Context, string, string, time.Time) (hotobjects.ColdSets, error) {
	return hotobjects.ColdSets{}, nil //nolint:exhaustruct
}

func weightsHandlers(st healthscore.Store) *Handlers {
	return &Handlers{scorer: healthscore.NewScorer(&config.Config{}, nil, st, nil)} //nolint:exhaustruct
}

func weightsReq(w serverhttp.HealthScoreWeightsUpdate) serverhttp.PutHealthScoreWeightsRequestObject {
	return serverhttp.PutHealthScoreWeightsRequestObject{
		Params: serverhttp.PutHealthScoreWeightsParams{ClusterName: "c1"},
		Body:   &w,
	}
}

func TestPutHealthScoreWeightsInvalid(t *testing.T) {
	t.Parallel()

	for _, st := range []healthscore.Store{nil, &weightsStore{}} { //nolint:exhaustruct
		resp, err := weightsHandlers(st).PutHealthScoreWeights(context.Background(),
			weightsReq(serverhttp.HealthScoreWeightsUpdate{Locks: -1})) //nolint:exhaustruct
		require.NoError(t, err)
		assert.IsType(t, serverhttp.PutHealthScoreWeights400Response{}, resp)
	}
}

func TestPutHealthScoreWeightsWithoutStorage(t *testing.T) {
	t.Parallel()

	_, err := weightsHandlers(nil).PutHealthScoreWeights(context.Background(),
		weightsReq(serverhttp.HealthScoreWeightsUpdate{Locks: 1})) //nolint:exhaustruct
	require.Error(t, err)
}

func TestPutHealthScoreWeightsReturnsOverride(t *testing.T) {
	t.Parallel()

	resp, err := weightsHandlers(&weightsStore{}).PutHealthScoreWeights(context.Background(), //nolint:exhaustruct
		weightsReq(serverhttp.HealthScoreWeightsUpdate{Locks: 3, Connections: 1})) //nolint:exhaustruct
	require.NoError(t, err)

	got, ok := resp.(serverhttp.PutHealthScoreWeights200JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, serverhttp.Override, got.Source)
	assert.InDelta(t, 0.75, got.Locks, 1e-9)
	assert.NotNil(t, got.UpdatedAt)
	assert.Nil(t, got.UpdatedBy, "no user in context, nothing to record")
}

func TestGetHealthScoreWeightsDefault(t *testing.T) {
	t.Parallel()

	resp, err := weightsHandlers(nil).GetHealthScoreWeights(context.Background(),
		serverhttp.GetHealthScoreWeightsRequestObject{Params: serverhttp.GetHealthScoreWeightsParams{ClusterName: "c1"}})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.GetHealthScoreWeights200JSONResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, serverhttp.Default, got.Source)
	assert.InDelta(t, health.DefaultWeights().Locks, got.Locks, 1e-9)
	assert.Nil(t, got.UpdatedAt)
}

func TestGetHealthScoreWeightsReadError(t *testing.T) {
	t.Parallel()

	_, err := weightsHandlers(&weightsStore{readErr: errors.New("boom")}).GetHealthScoreWeights( //nolint:exhaustruct
		context.Background(),
		serverhttp.GetHealthScoreWeightsRequestObject{Params: serverhttp.GetHealthScoreWeightsParams{ClusterName: "c1"}})
	require.Error(t, err)
}
