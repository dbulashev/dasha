package autosnapshot

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

type insightsFakeStore struct {
	Store
	truncates int
}

func (f *insightsFakeStore) TruncateInsightsScans(context.Context) error {
	f.truncates++

	return nil
}

func TestInsightsRetentionRunsOncePerInterval(t *testing.T) {
	t.Parallel()

	store := &insightsFakeStore{Store: nil, truncates: 0}
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
