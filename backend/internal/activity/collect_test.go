package activity

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/pkg/errs"
)

func TestCollect(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	calls := 0

	get := func(context.Context) (Sample, Access, error) {
		calls++

		switch calls {
		case 2:
			return Sample{}, Access{}, context.DeadlineExceeded
		case 4:
			// The cache answered twice with the same sample.
			return Sample{At: t0.Add(3 * time.Second)}, Access{CanReadAllStats: true}, nil
		}

		return Sample{At: t0.Add(time.Duration(calls) * time.Second)}, Access{CanReadAllStats: true}, nil
	}

	col, err := collect(t.Context(), get, 5, time.Millisecond, time.Minute)
	require.NoError(t, err)

	assert.Equal(t, 5, calls)
	assert.Equal(t, 1, col.Failed)
	require.ErrorIs(t, col.LastErr, context.DeadlineExceeded)
	assert.Len(t, col.Samples, 3, "the repeated sample is counted once")
	assert.True(t, col.Access.CanReadAllStats)
}

func TestCollectStopsOnHardErrors(t *testing.T) {
	t.Parallel()

	for _, hard := range []error{ErrUnsupportedVersion, errs.ErrNotFound} {
		calls := 0

		get := func(context.Context) (Sample, Access, error) {
			calls++

			return Sample{}, Access{}, hard
		}

		_, err := collect(t.Context(), get, 5, time.Millisecond, time.Minute)
		require.ErrorIs(t, err, hard)
		assert.Equal(t, 1, calls)
	}
}

func TestCollectStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	calls := 0

	get := func(context.Context) (Sample, Access, error) {
		calls++
		cancel()

		return Sample{At: time.Now()}, Access{}, nil
	}

	_, err := collect(ctx, get, 60, time.Hour, 2*time.Hour)
	require.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 1, calls)
}

func TestCollectEndsWhenBudgetElapses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const budget = 50 * time.Millisecond

		start := time.Now()
		calls := 0

		get := func(ctx context.Context) (Sample, Access, error) {
			calls++
			if calls == 1 {
				return Sample{At: time.Now()}, Access{}, nil
			}

			<-ctx.Done()

			return Sample{}, Access{}, ctx.Err()
		}

		col, err := collect(t.Context(), get, 60, time.Millisecond, budget)
		require.NoError(t, err)
		assert.Len(t, col.Samples, 1, "samples taken before the budget elapsed are kept")
		assert.Equal(t, 2, calls)
		assert.Equal(t, budget, time.Since(start))
	})
}

func TestSamplerCollect(t *testing.T) {
	t.Parallel()

	src := &fakeSource{}
	s, clk := newTestSampler(src)
	src.onSample = func() { clk.advance(time.Second) }

	col, err := s.Collect(t.Context(), "c", "h", 3, time.Millisecond, time.Minute)
	require.NoError(t, err)
	assert.Len(t, col.Samples, 3)
	assert.Zero(t, col.Failed)
}
