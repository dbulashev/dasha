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
)

type fakeClusters struct {
	clusters []dto.ClusterInfo
}

func (f fakeClusters) Clusters(context.Context) ([]dto.ClusterInfo, error) {
	return f.clusters, nil
}

func fleetOf(clusters, hostsPer int) fakeClusters {
	var out fakeClusters

	for c := range clusters {
		ci := dto.ClusterInfo{Name: config.ClusterName(fmt.Sprintf("c%03d", c))}
		for h := range hostsPer {
			ci.Instances = append(ci.Instances, dto.Instance{HostName: config.Host(fmt.Sprintf("h%d", h))})
		}

		out.clusters = append(out.clusters, ci)
	}

	return out
}

type fakeFleetScorer struct {
	delay  time.Duration
	scores map[metrics.TargetRef]float64 // missing = 90
}

func (f *fakeFleetScorer) Score(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult) (InstanceScore, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return InstanceScore{}, ctx.Err()
		}
	}

	score, ok := f.scores[t]
	if !ok {
		score = 90
	}

	src := SourceSnapshot
	if pre != nil && pre.Err == nil {
		src = SourceMetrics
	}

	return InstanceScore{Result: health.Result{Score: score}, Source: src}, nil
}

func (f *fakeFleetScorer) Weights(context.Context, string) (health.Weights, error) {
	return health.DefaultWeights(), nil
}

type fakeFleetMetrics struct {
	sigs     map[metrics.TargetRef]metrics.Signals // missing = healthy
	errFor   func(metrics.TargetRef) error
	instant  atomic.Int32
	rawCalls atomic.Int32
}

func healthySignals() metrics.Signals {
	sig := metrics.NewSignals(time.Now())
	sig.Set(metrics.SigTotalConns, 5)
	sig.Set(metrics.SigMaxConns, 200)
	sig.Set(metrics.SigCacheHitRatio, 99.9)

	return sig
}

func (f *fakeFleetMetrics) Enabled() bool { return true }

func (f *fakeFleetMetrics) InstantMany(_ context.Context, targets []metrics.TargetRef, _ ...metrics.SignalKind) (map[metrics.TargetRef]metrics.Signals, map[metrics.TargetRef]error) {
	f.instant.Add(1)

	return f.read(targets)
}

func (f *fakeFleetMetrics) read(targets []metrics.TargetRef) (map[metrics.TargetRef]metrics.Signals, map[metrics.TargetRef]error) {
	out := make(map[metrics.TargetRef]metrics.Signals)
	errs := make(map[metrics.TargetRef]error)

	for _, t := range targets {
		if f.errFor != nil {
			if err := f.errFor(t); err != nil {
				errs[t] = err

				continue
			}
		}

		if s, ok := f.sigs[t]; ok {
			out[t] = s
		} else {
			out[t] = healthySignals()
		}
	}

	return out, errs
}

func (f *fakeFleetMetrics) CurrentRawMany(_ context.Context, targets []metrics.TargetRef) map[metrics.TargetRef]metrics.RawResult {
	f.rawCalls.Add(1)

	sigs, errs := f.read(targets)
	out := make(map[metrics.TargetRef]metrics.RawResult, len(targets))

	for _, t := range targets {
		if err := errs[t]; err != nil {
			out[t] = metrics.RawResult{Err: err}

			continue
		}

		out[t] = metrics.RawResult{Raw: sigs[t].ToRawMetrics(), Signals: sigs[t]}
	}

	return out
}

func fleetCfg(margin int) config.FleetConfig {
	return config.FleetConfig{CandidateMargin: &margin}.WithDefaults()
}

func wraparoundSignals() metrics.Signals {
	sig := healthySignals()
	sig.Set(metrics.SigXactsLeftWrap, 100_000_000)

	return sig
}

func TestFleet_FloorInstanceAlwaysListed(t *testing.T) {
	for _, n := range []int{10, 200, 1000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			cl := fleetOf(n, 1)
			bad := metrics.TargetRef{Cluster: fmt.Sprintf("c%03d", n-1), Instance: "h0"}

			ms := &fakeFleetMetrics{sigs: map[metrics.TargetRef]metrics.Signals{bad: wraparoundSignals()}}
			sc := &fakeFleetScorer{scores: map[metrics.TargetRef]float64{bad: 30}}
			f := newFleet(fleetCfg(0), nil, sc, ms, cl, nil)

			res, err := f.Worst(t.Context(), FleetRequest{Limit: 5})
			if err != nil {
				t.Fatal(err)
			}

			if len(res.Items) == 0 || res.Items[0].Target != bad {
				t.Fatalf("worst %+v, want %v first", res.Items, bad)
			}

			if res.Candidates > config.DefaultFleetLimit+1 {
				t.Errorf("candidates %d, want at most the computed limit plus the floor", res.Candidates)
			}

			if res.InstancesTotal != n {
				t.Errorf("instances_total %d, want %d", res.InstancesTotal, n)
			}
		})
	}
}

func TestFleet_BudgetExceeded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := fleetCfg(0)
		cfg.SnapshotConcurrency = 1
		cfg.Budget = 10 * time.Second
		cfg.InstanceTimeout = 5 * time.Second

		sc := &fakeFleetScorer{delay: 3 * time.Second}
		f := newFleet(cfg, nil, sc, nil, fleetOf(10, 1), nil)

		res, err := f.Worst(t.Context(), FleetRequest{})
		if err != nil {
			t.Fatal(err)
		}

		if res.InstancesScored != 3 || res.Uncomputed != 7 || !res.Incomplete {
			t.Fatalf("scored %d uncomputed %d incomplete %v, want 3/7/true", res.InstancesScored, res.Uncomputed, res.Incomplete)
		}

		for i, it := range res.Items {
			if scored := it.Score != nil; scored != (i < 3) {
				t.Errorf("row %d scored=%v: unscored rows must trail", i, scored)
			}

			if it.Score == nil && (it.Source != SourceNone || it.Err != errBudgetExceeded.Error()) {
				t.Errorf("row %d: source %q err %q", i, it.Source, it.Err)
			}
		}
	})
}

func TestFleet_MetricsUnavailableFallsBackToSnapshot(t *testing.T) {
	ms := &fakeFleetMetrics{errFor: func(metrics.TargetRef) error { return errors.New("connection refused") }}
	sc := &fakeFleetScorer{}
	f := newFleet(fleetCfg(0), nil, sc, ms, fleetOf(6, 2), nil)

	res, err := f.Worst(t.Context(), FleetRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if !res.MetricsUnavailable {
		t.Error("metrics_unavailable not set")
	}

	if res.InstancesScored != 12 {
		t.Errorf("scored %d, want 12", res.InstancesScored)
	}

	if n := ms.rawCalls.Load(); n != 0 {
		t.Errorf("datasource re-read %d times after the prefilter failed", n)
	}

	for _, it := range res.Items {
		if it.Source != SourceSnapshot {
			t.Errorf("%v: source %q, want snapshot", it.Target, it.Source)
		}
	}
}

func TestFleet_UnmappedGoesToSnapshot(t *testing.T) {
	unmapped := metrics.TargetRef{Cluster: "c000", Instance: "h0"}
	ms := &fakeFleetMetrics{errFor: func(t metrics.TargetRef) error {
		if t == unmapped {
			return metrics.ErrTargetNotMapped
		}

		return nil
	}}
	sc := &fakeFleetScorer{scores: map[metrics.TargetRef]float64{unmapped: 10}}
	f := newFleet(fleetCfg(0), nil, sc, ms, fleetOf(3, 1), nil)

	res, err := f.Worst(t.Context(), FleetRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}

	if res.MetricsUnavailable {
		t.Error("one unmapped target marked the datasource unavailable")
	}

	if res.Items[0].Target != unmapped || res.Items[0].Source != SourceSnapshot {
		t.Errorf("worst %+v, want the unmapped target from the snapshot", res.Items[0])
	}
}

func TestFleet_ConcurrentCallsShareOneComputation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ms := &fakeFleetMetrics{}
		sc := &fakeFleetScorer{delay: time.Second}
		f := newFleet(fleetCfg(0), nil, sc, ms, fleetOf(4, 1), nil)

		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				if _, err := f.Worst(t.Context(), FleetRequest{Limit: 5}); err != nil {
					t.Error(err)
				}
			})
		}

		wg.Wait()

		if n := ms.instant.Load(); n != 1 {
			t.Errorf("prefilter ran %d times, want 1", n)
		}

		if _, err := f.Worst(t.Context(), FleetRequest{Limit: config.DefaultFleetLimit}); err != nil {
			t.Fatal(err)
		}

		if n := ms.instant.Load(); n != 1 {
			t.Errorf("cached result not reused: prefilter ran %d times", n)
		}

		if _, err := f.Worst(t.Context(), FleetRequest{Limit: config.MaxFleetLimit}); err != nil {
			t.Fatal(err)
		}

		if n := ms.instant.Load(); n != 2 {
			t.Errorf("larger limit did not recompute: prefilter ran %d times", n)
		}
	})
}

func TestFleet_FirstCallerCancelDoesNotBreakSecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sc := &fakeFleetScorer{delay: time.Second}
		f := newFleet(fleetCfg(0), nil, sc, &fakeFleetMetrics{}, fleetOf(2, 1), nil)

		first, cancel := context.WithCancel(t.Context())

		var wg sync.WaitGroup

		wg.Go(func() {
			if _, err := f.Worst(first, FleetRequest{}); !errors.Is(err, context.Canceled) {
				t.Errorf("first caller: %v, want canceled", err)
			}
		})

		time.Sleep(500 * time.Millisecond)
		cancel()

		res, err := f.Worst(t.Context(), FleetRequest{})
		if err != nil {
			t.Fatal(err)
		}

		if res.Incomplete || res.InstancesScored != 2 {
			t.Errorf("scored %d incomplete %v, want 2/false", res.InstancesScored, res.Incomplete)
		}

		wg.Wait()
	})
}

func TestFleet_StickyInstanceStaysCandidate(t *testing.T) {
	cl := fleetOf(3, 1)
	a := metrics.TargetRef{Cluster: "c000", Instance: "h0"}
	x := metrics.TargetRef{Cluster: "c002", Instance: "h0"}

	worse := healthySignals()
	worse.Set(metrics.SigCacheHitRatio, 50)

	ms := &fakeFleetMetrics{sigs: map[metrics.TargetRef]metrics.Signals{a: worse}}
	sc := &fakeFleetScorer{scores: map[metrics.TargetRef]float64{x: 10, a: 50}}

	cfg := fleetCfg(0)
	cfg.DefaultLimit = 1
	cfg.AllowExhaustive = true
	f := newFleet(cfg, nil, sc, ms, cl, nil)

	if _, err := f.Worst(t.Context(), FleetRequest{Limit: 1, Exhaustive: true}); err != nil {
		t.Fatal(err)
	}

	res, err := f.Worst(t.Context(), FleetRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}

	if res.Items[0].Target != x {
		t.Errorf("worst %v, want the sticky %v", res.Items[0].Target, x)
	}

	if res.Candidates != 2 {
		t.Errorf("candidates %d, want the estimate's pick plus the sticky one", res.Candidates)
	}
}

func TestFleet_RequestValidation(t *testing.T) {
	f := newFleet(fleetCfg(0), nil, &fakeFleetScorer{}, nil, fleetOf(1, 1), nil)

	if _, err := f.Worst(t.Context(), FleetRequest{Clusters: []string{"c000", "nope"}}); !errors.Is(err, ErrUnknownCluster) {
		t.Errorf("unknown cluster: %v", err)
	}

	if _, err := f.Worst(t.Context(), FleetRequest{Exhaustive: true}); !errors.Is(err, ErrExhaustiveNotAllowed) {
		t.Errorf("exhaustive: %v", err)
	}
}

func TestFleet_ClusterFilter(t *testing.T) {
	f := newFleet(fleetCfg(0), nil, &fakeFleetScorer{}, nil, fleetOf(3, 2), nil)

	res, err := f.Worst(t.Context(), FleetRequest{Clusters: []string{"c001"}})
	if err != nil {
		t.Fatal(err)
	}

	if res.InstancesTotal != 2 {
		t.Fatalf("instances_total %d, want 2", res.InstancesTotal)
	}

	for _, it := range res.Items {
		if it.Target.Cluster != "c001" {
			t.Errorf("row from %s outside the filter", it.Target.Cluster)
		}
	}
}

// The fleet row and the card come from the same Scorer, so they agree.
func TestFleet_ParityWithCard(t *testing.T) {
	repo, ms := newFakes()
	repo.delay, ms.delay = 0, 0

	s := newScorer(&config.Config{}, repo, ms)
	fm := &fakeFleetMetrics{sigs: map[metrics.TargetRef]metrics.Signals{target: ms.sig}}
	f := newFleet(fleetCfg(0), nil, s, fm, fakeClusters{clusters: []dto.ClusterInfo{{
		Name: config.ClusterName(target.Cluster), Instances: []dto.Instance{{HostName: config.Host(target.Instance)}},
	}}}, nil)

	card, err := s.Score(t.Context(), target, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := f.Worst(t.Context(), FleetRequest{})
	if err != nil {
		t.Fatal(err)
	}

	row := res.Items[0]
	if row.Score == nil || *row.Score != card.Result.Score || row.Source != card.Source || row.InRecovery != card.Result.InRecovery {
		t.Errorf("fleet row %+v, card score %v source %q in_recovery %v", row, card.Result.Score, card.Source, card.Result.InRecovery)
	}
}
