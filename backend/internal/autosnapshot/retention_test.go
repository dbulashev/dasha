package autosnapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

type insightsFakeStore struct {
	Store
	truncates int
	err       error
}

func (f *insightsFakeStore) TruncateInsightsScans(context.Context) error {
	f.truncates++

	return f.err
}

func TestInsightsRetentionRunsOncePerInterval(t *testing.T) {
	t.Parallel()

	store := &insightsFakeStore{Store: nil, truncates: 0, err: nil}
	d := &Daemon{ //nolint:exhaustruct
		store:  store,
		logger: zap.NewNop(),
		hosts:  map[hostKey]*hostState{},
	}

	ctx := t.Context()

	d.maybeRunInsightsRetention(ctx)
	d.maybeRunInsightsRetention(ctx)

	if store.truncates != 1 {
		t.Fatalf("truncated %d times, want once on start", store.truncates)
	}

	d.mu.Lock()
	d.lastInsightsRetention = time.Now().UTC().Add(-retentionInterval - time.Minute)
	d.mu.Unlock()

	d.maybeRunInsightsRetention(ctx)

	if store.truncates != 2 {
		t.Fatalf("truncated %d times, want one more after the interval", store.truncates)
	}
}

func TestInsightsRetentionRetriesSoonerAfterAFailure(t *testing.T) {
	t.Parallel()

	store := &insightsFakeStore{Store: nil, truncates: 0, err: errors.New("truncate denied")}
	d := &Daemon{ //nolint:exhaustruct
		store:  store,
		logger: zap.NewNop(),
		hosts:  map[hostKey]*hostState{},
	}

	d.maybeRunInsightsRetention(t.Context())

	d.mu.Lock()
	next := d.lastInsightsRetention.Add(retentionInterval)
	d.mu.Unlock()

	if wait := time.Until(next); wait > insightsRetentionRetry || wait < insightsRetentionRetry-time.Minute {
		t.Errorf("next run in %s, want about %s", wait, insightsRetentionRetry)
	}

	d.maybeRunInsightsRetention(t.Context())

	if store.truncates != 1 {
		t.Fatalf("truncated %d times, want no retry before %s", store.truncates, insightsRetentionRetry)
	}
}
