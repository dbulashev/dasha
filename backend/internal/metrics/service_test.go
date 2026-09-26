package metrics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestService(t *testing.T, client DatasourceClient) *Service {
	t.Helper()

	cfg := testConfig().WithDefaults()

	m, err := NewMatcher(cfg, nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	return newService(cfg, m, client, nil)
}

var testTarget = TargetRef{Cluster: "prod-mdb", Instance: "rc1a-abc.mdb.yandexcloud.net"}

func latencyKey() []batchKey {
	return []batchKey{{Target: testTarget, Signal: SigLatencyMs}}
}

func rangeCount(st *QueryStats) int {
	return st.Counts().Range
}

func TestService_CurrentRawIsOneInstantAndOneRange(t *testing.T) {
	s := newTestService(t, &exprClient{value: constValue(1)})

	st := &QueryStats{}

	_, sig, err := s.CurrentRaw(WithQueryStats(context.Background(), st), testTarget.Cluster, testTarget.Instance)
	if err != nil {
		t.Fatalf("CurrentRaw: %v", err)
	}

	if len(sig.Have) == 0 {
		t.Fatal("no signals collected")
	}

	if c := st.Counts(); c.Instant != 1 || c.Range != 1 {
		t.Errorf("want 1 instant + 1 range request, got %+v", c)
	}
}

func TestService_BaselineCachedWithinTTL(t *testing.T) {
	s := newTestService(t, &exprClient{value: constValue(1)})

	st := &QueryStats{}
	ctx := WithQueryStats(context.Background(), st)

	s.baselines(ctx, latencyKey())
	s.baselines(ctx, latencyKey())

	if n := rangeCount(st); n != 1 {
		t.Errorf("want 1 range request within cache_ttl, got %d", n)
	}
}

func TestService_FailedBaselineCachedForAMinute(t *testing.T) {
	s := newTestService(t, &exprClient{
		value: constValue(1),
		fail:  func(string) error { return errors.New("vm down") },
	})

	st := &QueryStats{}
	ctx := WithQueryStats(context.Background(), st)

	s.baselines(ctx, latencyKey())
	s.baselines(ctx, latencyKey())

	if n := rangeCount(st); n != 1 {
		t.Errorf("a failed refresh must be cached, got %d range requests", n)
	}

	e := s.baseCache[latencyKey()[0]]
	if left := time.Until(e.expires); left <= 0 || left > time.Minute {
		t.Errorf("failed entry must expire within a minute, expires in %v", left)
	}
}

func TestService_CancelledRefreshCachesNothing(t *testing.T) {
	s := newTestService(t, &exprClient{value: constValue(1)})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.baselines(ctx, latencyKey())

	if len(s.baseCache) != 0 || len(s.baseFlight) != 0 {
		t.Errorf("want empty cache and no flights, got %d entries, %d flights", len(s.baseCache), len(s.baseFlight))
	}
}

// gateClient blocks range requests until release is closed.
type gateClient struct {
	*exprClient
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gateClient) QueryRange(ctx context.Context, q string, r Range) ([]Series, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release

	return g.exprClient.QueryRange(ctx, q, r)
}

func TestService_ConcurrentRefreshesCollapse(t *testing.T) {
	g := &gateClient{
		exprClient: &exprClient{value: constValue(1)},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	s := newTestService(t, g)

	st := &QueryStats{}
	ctx := WithQueryStats(context.Background(), st)

	var wg sync.WaitGroup

	wg.Go(func() { s.baselines(ctx, latencyKey()) })

	<-g.started

	wg.Go(func() { s.baselines(ctx, latencyKey()) })

	close(g.release)
	wg.Wait()

	if n := rangeCount(st); n != 1 {
		t.Errorf("want one range request for concurrent refreshes, got %d", n)
	}
}
