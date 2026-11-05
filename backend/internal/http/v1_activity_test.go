package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/activity"
)

func TestActivitySampleToAPI(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 12, 0, 10, 0, time.UTC)
	started := at.Add(-time.Minute)

	prev := activity.Sample{
		At: at.Add(-time.Second),
		Sessions: []activity.Session{
			{PID: 1, BackendType: "client backend", State: "active", QueryStart: started, Key: "q:9007199254740993"},
			{PID: 5, BackendType: "client backend", State: "active", QueryStart: started, Key: "q:1"},
			{PID: 3, BackendType: "autovacuum worker", WaitType: "IO", QueryStart: started, Key: ""},
		},
	}

	smp := activity.Sample{
		At: at,
		Sessions: []activity.Session{
			{PID: 1, BackendType: "client backend", State: "active", QueryID: 9007199254740993,
				QueryStart: started, Text: "old", Key: "q:9007199254740993", KeyKind: activity.KeyKindQueryID},
			{PID: 2, BackendType: "client backend", State: "active", WaitType: "Lock", WaitEvent: "relation",
				QueryStart: at.Add(-time.Millisecond), Text: "new", Key: "f:x", KeyKind: activity.KeyKindFingerprint},
			{PID: 5, BackendType: "client backend", State: "active", QueryID: 2,
				QueryStart: started, Text: "BEGIN; UPDATE a; UPDATE b; COMMIT", Key: "q:2", KeyKind: activity.KeyKindQueryID},
			{PID: 3, BackendType: "autovacuum worker", WaitType: "IO", QueryStart: started, Text: "autovacuum: VACUUM t"},
			{PID: 4, BackendType: "client backend", State: "disabled"},
		},
	}

	out := activitySampleToAPI(smp, activity.Access{CanReadAllStats: true}, false, &prev)

	assert.Equal(t, at, out.SampledAt)
	assert.True(t, out.CanReadAllStats)
	require.Len(t, out.Sessions, 3)

	old, fresh, next := out.Sessions[0], out.Sessions[1], out.Sessions[2]

	assert.Nil(t, old.Text, "the same statement was in the previous sample: the client has the text")
	assert.Equal(t, "9007199254740993", old.QueryId)
	assert.Equal(t, serverhttp.ActivityClass(activity.ClassCPU), old.Class)
	assert.Nil(t, old.XactStart)

	require.NotNil(t, fresh.Text)
	assert.Equal(t, "new", *fresh.Text)
	assert.Equal(t, serverhttp.ActivityClass(activity.ClassLock), fresh.Class)

	require.NotNil(t, next.Text, "same query_start, next statement of the string: a key the client has no text for")

	withBg := activitySampleToAPI(smp, activity.Access{}, true, &prev)
	require.Len(t, withBg.Sessions, 4)
	assert.Nil(t, withBg.Sessions[3].Text, "the previous answer with background processes sent this text")
	assert.Nil(t, withBg.Sessions[0].Text)
	assert.NotNil(t, withBg.Sessions[2].Text)

	for _, s := range activitySampleToAPI(smp, activity.Access{}, true, nil).Sessions {
		assert.NotNil(t, s.Text, "an unknown text_since sends every text")
	}
}

func TestCollectActivity(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	calls := 0

	get := func(context.Context) (activity.Sample, activity.Access, error) {
		calls++

		switch calls {
		case 2:
			return activity.Sample{}, activity.Access{}, context.DeadlineExceeded
		case 4:
			// The cache answered twice with the same sample.
			return activity.Sample{At: t0.Add(3 * time.Second)}, activity.Access{CanReadAllStats: true}, nil
		}

		return activity.Sample{At: t0.Add(time.Duration(calls) * time.Second)}, activity.Access{CanReadAllStats: true}, nil
	}

	col, err := collectActivity(t.Context(), get, 5, time.Millisecond, time.Minute)
	require.NoError(t, err)

	assert.Equal(t, 5, calls)
	assert.Equal(t, 1, col.failed)
	require.ErrorIs(t, col.lastErr, context.DeadlineExceeded)
	assert.Len(t, col.samples, 3, "the repeated sample is counted once")
	assert.True(t, col.access.CanReadAllStats)
}

func TestCollectActivityStopsOnHardErrors(t *testing.T) {
	t.Parallel()

	get := func(context.Context) (activity.Sample, activity.Access, error) {
		return activity.Sample{}, activity.Access{}, activity.ErrUnsupportedVersion
	}

	_, err := collectActivity(t.Context(), get, 5, time.Millisecond, time.Minute)
	require.ErrorIs(t, err, activity.ErrUnsupportedVersion)
}

func TestCollectActivityStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	calls := 0

	get := func(context.Context) (activity.Sample, activity.Access, error) {
		calls++
		cancel()

		return activity.Sample{At: time.Now()}, activity.Access{}, nil
	}

	_, err := collectActivity(ctx, get, 60, time.Hour, 2*time.Hour)
	require.True(t, errors.Is(err, context.Canceled))
	assert.Equal(t, 1, calls)
}

func TestProfileParamsError(t *testing.T) {
	t.Parallel()

	lock := serverhttp.ActivityClass(activity.ClassLock)
	bogus := serverhttp.ActivityClass("Nap")

	assert.Empty(t, profileParamsError(15, 1, 10, &lock))
	assert.NotEmpty(t, profileParamsError(4, 1, 10, nil))
	assert.NotEmpty(t, profileParamsError(61, 1, 10, nil))
	assert.NotEmpty(t, profileParamsError(15, 11, 10, nil))
	assert.NotEmpty(t, profileParamsError(5, 10, 10, nil))
	assert.NotEmpty(t, profileParamsError(15, 1, 51, nil))
	assert.NotEmpty(t, profileParamsError(15, 1, 10, &bogus))
}

func TestActivityProfileToAPI(t *testing.T) {
	t.Parallel()

	p := activity.Profile{
		Samples: 3,
		AAS:     2,
		ByClass: []activity.Share{{Value: activity.ClassCPU, AAS: 2, SharePct: 100}},
		TopQueries: []activity.QueryShare{
			{Key: "q:9007199254740993", KeyKind: activity.KeyKindQueryID, QueryID: 9007199254740993, AAS: 2, SharePct: 100},
		},
	}

	out := activityProfileToAPI(p, activity.Access{QueryIDAvailable: true}, 1)

	assert.Equal(t, 1, out.FailedSamples)
	assert.True(t, out.QueryIdAvailable)
	require.Len(t, out.TopQueries, 1)
	assert.Equal(t, "9007199254740993", out.TopQueries[0].QueryId)
	assert.Nil(t, out.TopQueries[0].Text)
	assert.NotNil(t, out.ByDatabase, "empty lists serialize as []")
	assert.NotNil(t, out.ByBackendType)
}

func TestActivityProfileToAPIRounds(t *testing.T) {
	t.Parallel()

	p := activity.Profile{
		Samples: 45,
		AAS:     12.155555555555555,
		ByClass: []activity.Share{{Value: activity.ClassTimeout, AAS: 10.533333333333333, SharePct: 86.654478976234}},
		TopQueries: []activity.QueryShare{{
			Key: "q:1", KeyKind: activity.KeyKindQueryID, QueryID: 1, AAS: 7.111111111111111, SharePct: 58.50091407678245,
			ByClass: map[string]float64{activity.ClassCPU: 3.4375, activity.ClassTimeout: 96.5625},
		}},
		ByBackend: []activity.Share{{Value: "checkpointer", AAS: 1, SharePct: 5.1020408163265305}},
	}

	out := activityProfileToAPI(p, activity.Access{}, 0)

	assert.InDelta(t, 12.16, out.Aas, 1e-9)
	assert.InDelta(t, 86.7, out.ByClass[0].SharePct, 1e-9)
	assert.InDelta(t, 7.11, out.TopQueries[0].Aas, 1e-9)
	assert.InDelta(t, 58.5, out.TopQueries[0].SharePct, 1e-9)
	assert.InDelta(t, 3.4, out.TopQueries[0].ByClass[activity.ClassCPU], 1e-9)
	assert.InDelta(t, 5.1, out.ByBackendType[0].SharePct, 1e-9)
}

func TestCollectActivityEndsWhenBudgetElapses(t *testing.T) {
	t.Parallel()

	calls := 0

	get := func(ctx context.Context) (activity.Sample, activity.Access, error) {
		calls++
		if calls == 1 {
			return activity.Sample{At: time.Now()}, activity.Access{}, nil
		}

		<-ctx.Done()

		return activity.Sample{}, activity.Access{}, ctx.Err()
	}

	col, err := collectActivity(t.Context(), get, 60, time.Millisecond, 50*time.Millisecond)
	require.NoError(t, err)
	assert.Len(t, col.samples, 1)
	assert.Equal(t, 2, calls)
}
