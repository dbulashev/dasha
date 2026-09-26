package healthscore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/repository"
)

const readDelay = 100 * time.Millisecond

type fakeRepo struct {
	repository.Repository

	delay    time.Duration
	snap     *dto.HealthScoreMetrics
	snapErr  error
	seqWorst float64
	seqKnown bool

	snapCalls atomic.Int32
	seqCalls  atomic.Int32
}

func (f *fakeRepo) GetHealthScoreMetrics(_ context.Context, _, _, _ string) (*dto.HealthScoreMetrics, error) {
	f.snapCalls.Add(1)
	time.Sleep(f.delay)

	return f.snap, f.snapErr
}

func (f *fakeRepo) GetSequenceHeadroom(_ context.Context, _, _, _ string) (float64, bool, error) {
	f.seqCalls.Add(1)
	time.Sleep(f.delay)

	return f.seqWorst, f.seqKnown, nil
}

func (f *fakeRepo) Clusters(context.Context) ([]dto.ClusterInfo, error) {
	return nil, nil
}

type fakeMetrics struct {
	delay time.Duration
	sig   metrics.Signals
	err   error
	calls atomic.Int32
}

func (f *fakeMetrics) Enabled() bool { return true }

func (f *fakeMetrics) CurrentRaw(context.Context, string, string) (health.RawMetrics, metrics.Signals, error) {
	f.calls.Add(1)
	time.Sleep(f.delay)

	if f.err != nil {
		return health.RawMetrics{}, metrics.Signals{}, f.err
	}

	return f.sig.ToRawMetrics(), f.sig, nil
}

var target = metrics.TargetRef{Cluster: "c1", Instance: "h1"}

func newFakes() (*fakeRepo, *fakeMetrics) {
	sig := metrics.NewSignals(time.Now())
	sig.Set(metrics.SigTotalConns, 5)
	sig.Set(metrics.SigMaxConns, 200)

	return &fakeRepo{delay: readDelay, snap: snapshotFixture()},
		&fakeMetrics{delay: readDelay, sig: sig}
}

func TestScore_ReadsRunConcurrently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		s := newScorer(&config.Config{}, repo, ms)

		start := time.Now()

		got, err := s.Score(t.Context(), target, nil)
		if err != nil {
			t.Fatal(err)
		}

		if elapsed := time.Since(start); elapsed != readDelay {
			t.Errorf("elapsed %v, want %v (the slowest read, not the sum)", elapsed, readDelay)
		}

		if got.Source != SourceMetrics {
			t.Errorf("source %q, want metrics", got.Source)
		}
	})
}

func TestScore_MetricsErrorFallsBackToSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		ms.err = errors.New("vm down")
		s := newScorer(&config.Config{}, repo, ms)

		got, err := s.Score(t.Context(), target, nil)
		if err != nil {
			t.Fatal(err)
		}

		if got.Source != SourceSnapshot {
			t.Errorf("source %q, want snapshot", got.Source)
		}

		if got.MetricsDegraded {
			t.Error("snapshot score flagged as metrics-degraded")
		}

		if n := repo.snapCalls.Load(); n != 1 {
			t.Errorf("snapshot read %d times, want 1", n)
		}
	})
}

func TestScore_PreReplacesDatasourceRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		s := newScorer(&config.Config{}, repo, ms)

		pre := &metrics.RawResult{Raw: ms.sig.ToRawMetrics(), Signals: ms.sig}

		got, err := s.Score(t.Context(), target, pre)
		if err != nil {
			t.Fatal(err)
		}

		if n := ms.calls.Load(); n != 0 {
			t.Errorf("datasource read %d times with pre given", n)
		}

		if got.Source != SourceMetrics {
			t.Errorf("source %q, want metrics", got.Source)
		}
	})
}

func TestScore_EmptySignalsAreDegraded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, _ := newFakes()
		s := newScorer(&config.Config{}, repo, nil)

		empty := metrics.NewSignals(time.Now())

		got, err := s.Score(t.Context(), target, &metrics.RawResult{Raw: empty.ToRawMetrics(), Signals: empty})
		if err != nil {
			t.Fatal(err)
		}

		if got.Source != SourceMetrics || !got.MetricsDegraded {
			t.Errorf("got source %q degraded %v, want metrics degraded", got.Source, got.MetricsDegraded)
		}
	})
}

func TestScore_NotFound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		repo.snapErr = fmt.Errorf("GetHealthScoreMetrics | %w", repository.ErrNotFound)
		s := newScorer(&config.Config{}, repo, ms)

		_, err := s.Score(t.Context(), target, nil)
		if !IsNotFound(err) {
			t.Fatalf("err %v, want not found", err)
		}
	})
}

func TestScore_CardAndRecommendationsShareOneRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		s := newScorer(&config.Config{}, repo, ms)

		var wg sync.WaitGroup

		wg.Go(func() {
			if _, err := s.Score(t.Context(), target, nil); err != nil {
				t.Error(err)
			}
		})

		wg.Go(func() {
			if _, err := s.Recommendations(t.Context(), target, ""); err != nil {
				t.Error(err)
			}
		})

		wg.Wait()

		if n := repo.snapCalls.Load(); n != 1 {
			t.Errorf("snapshot read %d times, want 1", n)
		}

		if n := ms.calls.Load(); n != 1 {
			t.Errorf("datasource read %d times, want 1", n)
		}

		time.Sleep(inputsHold + time.Second)

		if _, err := s.Score(t.Context(), target, nil); err != nil {
			t.Fatal(err)
		}

		if n := repo.snapCalls.Load(); n != 2 {
			t.Errorf("snapshot read %d times after the hold expired, want 2", n)
		}
	})
}

func TestScore_FirstCallerCancelDoesNotBreakSecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		s := newScorer(&config.Config{}, repo, ms)

		ctx, cancel := context.WithCancel(t.Context())

		var wg sync.WaitGroup

		wg.Go(func() {
			if _, err := s.Score(ctx, target, nil); !errors.Is(err, context.Canceled) {
				t.Errorf("first caller err %v, want canceled", err)
			}
		})

		synctest.Wait()

		wg.Go(func() {
			if _, err := s.Score(t.Context(), target, nil); err != nil {
				t.Errorf("second caller: %v", err)
			}
		})

		synctest.Wait()
		cancel()
		wg.Wait()

		if n := repo.snapCalls.Load(); n != 1 {
			t.Errorf("snapshot read %d times, want 1", n)
		}
	})
}

func TestRecommendations_DrillDownSkipsDatasource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo, ms := newFakes()
		s := newScorer(&config.Config{}, repo, ms)

		if _, err := s.Recommendations(t.Context(), target, "app_db"); err != nil {
			t.Fatal(err)
		}

		if n := ms.calls.Load(); n != 0 {
			t.Errorf("datasource read %d times for a database drill-down", n)
		}
	})
}

func TestCompose_SequenceHeadroom(t *testing.T) {
	s := newScorer(&config.Config{}, &fakeRepo{}, nil)

	t.Run("primary takes the headroom", func(t *testing.T) {
		raw, _, _, err := s.compose(inputs{snap: &dto.HealthScoreMetrics{}, seqWorst: 0.9, seqKnown: true}, false)
		if err != nil {
			t.Fatal(err)
		}

		if raw.SequenceExhaustionMax != 0.9 {
			t.Errorf("SequenceExhaustionMax %v, want 0.9", raw.SequenceExhaustionMax)
		}
	})

	t.Run("standby drops it", func(t *testing.T) {
		raw, _, _, err := s.compose(inputs{snap: &dto.HealthScoreMetrics{InRecovery: true}, seqWorst: 0.9, seqKnown: true}, false)
		if err != nil {
			t.Fatal(err)
		}

		if raw.SequenceExhaustionMax != 0 {
			t.Errorf("SequenceExhaustionMax %v on a standby, want 0", raw.SequenceExhaustionMax)
		}
	})
}
