package metrics

import (
	"context"
	"testing"
	"time"
)

// valueClient returns a fixed value for any non-empty expression.
type valueClient struct{ v float64 }

func (c valueClient) QueryInstant(_ context.Context, expr string, _ time.Time) ([]Sample, error) {
	if expr == "" {
		return nil, nil
	}

	return []Sample{{Value: c.v, Labels: map[string]string{}}}, nil
}

func (c valueClient) QueryRange(_ context.Context, expr string, _ Range) ([]Series, error) {
	if expr == "" {
		return nil, nil
	}

	return []Series{{Points: []SeriesPoint{{Time: time.Unix(100, 0), Value: c.v}}}}, nil
}

func newTestCollector(t *testing.T, v float64) *Collector {
	t.Helper()

	m, err := NewMatcher(testConfig(), nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	return NewCollector(m, NewQueryCatalog(), valueClient{v: v}, "5m", "")
}

func TestCollector_InstantCollectsCataloguedSignals(t *testing.T) {
	co := newTestCollector(t, 42)

	sig, err := co.Instant(context.Background(), "prod-mdb", "rc1a-abc.mdb.yandexcloud.net", time.Now(),
		SigXactsLeftWrap, SigIOReadTime)
	if err != nil {
		t.Fatalf("Instant: %v", err)
	}

	// xacts_left is catalogued for pgscv (core) -> present.
	if v, ok := sig.Get(SigXactsLeftWrap); !ok || v != 42 {
		t.Errorf("SigXactsLeftWrap: want present/42, got v=%v ok=%v", v, ok)
	}

	// io_read_time has no catalog template -> absent (graceful).
	if _, ok := sig.Get(SigIOReadTime); ok {
		t.Errorf("SigIOReadTime should be absent (uncatalogued), got present")
	}
}

func TestCollector_RangeAlignsByTimestamp(t *testing.T) {
	co := newTestCollector(t, 7)

	out, err := co.Range(context.Background(), "prod-mdb", "rc1a-abc.mdb.yandexcloud.net", Range{
		Start: time.Unix(100, 0), End: time.Unix(100, 0), Step: time.Minute,
	}, SigXactsLeftWrap)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 aligned point, got %d", len(out))
	}

	if v, ok := out[0].Get(SigXactsLeftWrap); !ok || v != 7 {
		t.Errorf("aligned signal: want 7, got v=%v ok=%v", v, ok)
	}
}

func TestCollector_UnmappedTarget(t *testing.T) {
	co := newTestCollector(t, 1)

	if _, err := co.Instant(context.Background(), "nope", "nope", time.Now()); err == nil {
		t.Fatal("expected error for unmapped target")
	}
}
