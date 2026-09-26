package metrics

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/health"
)

// Service is the application-facing facade over the metrics data path. It is
// nil when the feature is disabled; all methods are nil-safe enough that callers
// gate on Enabled() before using the rest.
type Service struct {
	cfg     Config
	matcher *Matcher
	catalog *QueryCatalog
	client  DatasourceClient
	log     *zap.Logger

	collector *Collector

	mu         sync.Mutex
	baseCache  map[batchKey]baselineEntry
	baseFlight map[batchKey]chan struct{}

	onFlightWait func() // test hook: called before awaiting another caller's refresh
}

type baselineEntry struct {
	b       Baseline
	expires time.Time
}

// RawResult is CurrentRaw for one target of CurrentRawMany.
type RawResult struct {
	Raw     health.RawMetrics
	Signals Signals
	Err     error
}

// NewService builds the facade from config. Returns (nil, nil) when disabled so
// the DI container can hand callers a nil that still answers Enabled()==false.
// meta is optional: when provided, discovered clusters absent from Targets are
// auto-mapped from their discovery metadata. A nil logger is replaced with a no-op.
func NewService(cfg Config, meta MetadataProvider, logger *zap.Logger) (*Service, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	matcher, err := NewMatcher(cfg, meta)
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return newService(cfg, matcher, NewVMClient(cfg.Datasource, logger), logger), nil
}

func newService(cfg Config, matcher *Matcher, client DatasourceClient, logger *zap.Logger) *Service {
	s := &Service{
		cfg:        cfg,
		matcher:    matcher,
		catalog:    NewQueryCatalog(),
		client:     countingClient{inner: client},
		log:        logger,
		baseCache:  make(map[batchKey]baselineEntry),
		baseFlight: make(map[batchKey]chan struct{}),
	}

	limits := BatchLimits{MaxQueryBytes: cfg.Datasource.MaxQueryBytes, MaxConcurrency: cfg.Datasource.MaxConcurrency}
	s.collector = NewCollector(s.matcher, s.catalog, s.client, "5m", cfg.roleExclusion(), limits, logger)

	return s
}

// Enabled reports whether the metrics path is active (nil-safe).
func (s *Service) Enabled() bool {
	return s != nil
}

// ValidateTarget runs label-matching diagnostics for a Dasha target.
func (s *Service) ValidateTarget(ctx context.Context, cluster, instance string) (Diagnostics, error) {
	return s.matcher.Validate(ctx, s.client, cluster, instance)
}

// Collector returns the shared catalog-driven collector; its requests count
// against one datasource.max_concurrency.
func (s *Service) Collector() *Collector {
	return s.collector
}

// CurrentRaw returns the instant signals as health.RawMetrics with the
// regression ratios (latency, seq-scan) folded in against their seasonal
// baselines — for the rules engine / recommendations.
// The signals themselves come back alongside so the caller can tell which inputs
// the datasource actually carried: absent ones keep ToRawMetrics' neutral seeds
// (a healthy-looking value), and only presence distinguishes those from a real
// reading. An empty Have means the target resolved but no selector matched
// anything (likely a label-scheme mismatch).
func (s *Service) CurrentRaw(ctx context.Context, cluster, instance string) (health.RawMetrics, Signals, error) {
	t := TargetRef{Cluster: cluster, Instance: instance}
	r := s.CurrentRawMany(ctx, []TargetRef{t})[t]

	return r.Raw, r.Signals, r.Err
}

// CurrentRawMany is CurrentRaw for many targets in glued requests.
func (s *Service) CurrentRawMany(ctx context.Context, targets []TargetRef) map[TargetRef]RawResult {
	sigs, errs := s.Collector().InstantMany(ctx, targets, time.Now())

	out := make(map[TargetRef]RawResult, len(targets))
	ok := make([]TargetRef, 0, len(sigs))

	for _, t := range targets {
		if err := errs[t]; err != nil {
			out[t] = RawResult{Err: err}

			continue
		}

		ok = append(ok, t)
	}

	bases := s.baselines(ctx, targetKeys(ok, []SignalKind{SigLatencyMs, SigSeqScanRate}))

	for _, t := range ok {
		sig := sigs[t]
		lb, _ := bases[batchKey{Target: t, Signal: SigLatencyMs}].Value(sig.At)
		sb, _ := bases[batchKey{Target: t, Signal: SigSeqScanRate}].Value(sig.At)

		out[t] = RawResult{Raw: rawWithRegression(sig, Baselines{Latency: lb, SeqScan: sb}), Signals: sig}
	}

	return out
}

// baselines returns the seasonal baseline per key, refreshing stale ones in one
// glued range batch at most once per Baseline.CacheTTL. A key already being
// refreshed by another caller is awaited, not fetched again; if that refresh was
// cancelled, the key is refreshed again under ctx. A missing or empty baseline
// disables that regression penalty.
func (s *Service) baselines(ctx context.Context, keys []batchKey) map[batchKey]Baseline {
	out := make(map[batchKey]Baseline, len(keys))

	for pending := s.baselineRound(ctx, keys, out); len(pending) > 0 && ctx.Err() == nil; {
		pending = s.baselineRound(ctx, pending, out)
	}

	return out
}

// baselineRound fills out with the keys it can resolve and returns those left
// uncached.
func (s *Service) baselineRound(ctx context.Context, keys []batchKey, out map[batchKey]Baseline) []batchKey {
	now := time.Now()

	var (
		mine    []batchKey
		waitFor []chan struct{}
	)

	s.mu.Lock()

	for _, k := range keys {
		if e, ok := s.baseCache[k]; ok && now.Before(e.expires) {
			out[k] = e.b

			continue
		}

		if ch, ok := s.baseFlight[k]; ok {
			waitFor = append(waitFor, ch)

			continue
		}

		s.baseFlight[k] = make(chan struct{})
		mine = append(mine, k)
	}

	s.mu.Unlock()

	if len(mine) > 0 {
		s.refreshBaselines(ctx, mine)
	}

	if len(waitFor) > 0 && s.onFlightWait != nil {
		s.onFlightWait()
	}

	for _, ch := range waitFor {
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var left []batchKey

	for _, k := range keys {
		if _, done := out[k]; done {
			continue
		}

		if e, ok := s.baseCache[k]; ok {
			out[k] = e.b
		} else {
			left = append(left, k)
		}
	}

	return left
}

// refreshBaselines fetches the keys' history and caches the result. A failed key
// keeps its previous baseline for at most a minute; a cancelled refresh caches
// nothing.
func (s *Service) refreshBaselines(ctx context.Context, keys []batchKey) {
	const step = 30 * time.Minute

	to := time.Now()

	pts, errs := s.Collector().rangeKeys(ctx, keys, Range{Start: to.Add(-s.cfg.Baseline.Window), End: to, Step: step})
	cancelled := ctx.Err() != nil
	minPoints := int(s.cfg.Baseline.MinHistory / step)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, k := range keys {
		if !cancelled {
			e := baselineEntry{expires: now.Add(s.cfg.Baseline.CacheTTL)}

			if _, failed := errs[k]; failed {
				e.expires = now.Add(min(s.cfg.Baseline.CacheTTL, time.Minute))
				e.b = s.baseCache[k].b
			} else {
				e.b = BuildBaseline(pts[k], minPoints)
			}

			s.baseCache[k] = e
		}

		close(s.baseFlight[k])
		delete(s.baseFlight, k)
	}
}
