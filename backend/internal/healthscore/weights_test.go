package healthscore

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/health"
)

type fakeWeights struct {
	recs    map[string]WeightsRecord
	readErr error
}

func (f *fakeWeights) GetHealthWeights(_ context.Context, cluster string) (*WeightsRecord, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}

	rec, ok := f.recs[cluster]
	if !ok {
		return nil, nil //nolint:nilnil
	}

	return &rec, nil
}

func (f *fakeWeights) UpsertHealthWeights(_ context.Context, cluster string, w health.Weights, user string) error {
	f.recs[cluster] = WeightsRecord{Weights: w, UpdatedAt: time.Unix(1, 0), UpdatedBy: user}

	return nil
}

func (f *fakeWeights) DeleteHealthWeights(_ context.Context, cluster string) error {
	delete(f.recs, cluster)

	return nil
}

func weightsScorer(st WeightsStore) *Scorer {
	s := newScorer(&config.Config{}, &fakeRepo{}, nil) //nolint:exhaustruct
	s.weights = st

	return s
}

func TestWeightsViewDefaultWithoutStore(t *testing.T) {
	t.Parallel()

	v, err := weightsScorer(nil).WeightsView(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, WeightsDefault, v.Source)
	assert.Equal(t, health.DefaultWeights(), v.Weights)
	assert.Nil(t, v.UpdatedAt)
}

func TestWeightsViewReadErrorFallsBackToDefault(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	s := weightsScorer(&fakeWeights{readErr: boom}) //nolint:exhaustruct

	v, err := s.WeightsView(context.Background(), "c1")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, health.DefaultWeights(), v.Weights)

	w, err := s.Weights(context.Background(), "c1")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, health.DefaultWeights(), w)
}

func TestSetWeightsNormalizesAndRecordsUser(t *testing.T) {
	t.Parallel()

	st := &fakeWeights{recs: map[string]WeightsRecord{}} //nolint:exhaustruct
	s := weightsScorer(st)

	in := health.Weights{Connections: 2, Locks: 2} //nolint:exhaustruct

	v, err := s.SetWeights(context.Background(), "c1", in, "alice")
	require.NoError(t, err)
	assert.Equal(t, WeightsOverride, v.Source)
	assert.InDelta(t, 1.0, v.Weights.Sum(), 1e-9)
	require.NotNil(t, v.UpdatedBy)
	assert.Equal(t, "alice", *v.UpdatedBy)

	w, err := s.Weights(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, v.Weights, w, "the score must use the same weights the view shows")
}

func TestSetWeightsRejectsInvalid(t *testing.T) {
	t.Parallel()

	for _, s := range []*Scorer{weightsScorer(nil), weightsScorer(&fakeWeights{recs: map[string]WeightsRecord{}})} { //nolint:exhaustruct
		_, err := s.SetWeights(context.Background(), "c1", health.Weights{Locks: math.NaN()}, "") //nolint:exhaustruct
		require.ErrorIs(t, err, ErrInvalidWeights)
	}
}

func TestSetWeightsWithoutStoreFails(t *testing.T) {
	t.Parallel()

	_, err := weightsScorer(nil).SetWeights(context.Background(), "c1", health.DefaultWeights(), "")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidWeights)
}

func TestResetWeights(t *testing.T) {
	t.Parallel()

	st := &fakeWeights{recs: map[string]WeightsRecord{ //nolint:exhaustruct
		"c1": {Weights: health.Weights{Locks: 1}, UpdatedBy: "bob"}, //nolint:exhaustruct
	}}

	v, err := weightsScorer(st).ResetWeights(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, WeightsDefault, v.Source)
	assert.Empty(t, st.recs)

	v, err = weightsScorer(nil).ResetWeights(context.Background(), "c1")
	require.NoError(t, err)
	assert.Equal(t, WeightsDefault, v.Source)
}
