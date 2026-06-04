package metrics

import (
	"context"
	"sync"
	"time"

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

	mu        sync.Mutex
	baseCache map[string]baselineEntry // seasonal baseline per (target, signal)
}

type baselineEntry struct {
	b  Baseline
	at time.Time
}

// NewService builds the facade from config. Returns (nil, nil) when disabled so
// the DI container can hand callers a nil that still answers Enabled()==false.
// meta is optional: when provided, discovered clusters absent from Targets are
// auto-mapped from their discovery metadata.
func NewService(cfg Config, meta MetadataProvider) (*Service, error) {
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

	return &Service{
		cfg:       cfg,
		matcher:   matcher,
		catalog:   NewQueryCatalog(),
		client:    NewVMClient(cfg.Datasource),
		baseCache: make(map[string]baselineEntry),
	}, nil
}

// Enabled reports whether the metrics path is active (nil-safe).
func (s *Service) Enabled() bool {
	return s != nil
}

// ValidateTarget runs label-matching diagnostics for a Dasha target.
func (s *Service) ValidateTarget(ctx context.Context, cluster, instance string) (Diagnostics, error) {
	return s.matcher.Validate(ctx, s.client, cluster, instance)
}

// Collector returns a catalog-driven collector over the configured window.
func (s *Service) Collector() *Collector {
	return NewCollector(s.matcher, s.catalog, s.client, "5m")
}

// CurrentRaw returns the instant signals as health.RawMetrics with the
// regression ratios (latency, seq-scan) folded in against their seasonal
// baselines — for the rules engine / recommendations.
func (s *Service) CurrentRaw(ctx context.Context, cluster, instance string) (health.RawMetrics, error) {
	sig, err := s.Collector().Instant(ctx, cluster, instance, time.Now())
	if err != nil {
		return health.RawMetrics{}, err
	}

	lb, _ := s.signalBaseline(ctx, cluster, instance, SigLatencyMs).Value(sig.At)
	sb, _ := s.signalBaseline(ctx, cluster, instance, SigSeqScanRate).Value(sig.At)

	return rawWithRegression(sig, Baselines{Latency: lb, SeqScan: sb}), nil
}

// signalBaseline returns the per-(target, signal) seasonal baseline, refreshing
// it from the datasource at most once per QueryCacheTTL. A failed refresh yields
// an empty (unavailable) baseline, which disables that regression penalty.
func (s *Service) signalBaseline(ctx context.Context, cluster, instance string, kind SignalKind) Baseline {
	key := cluster + "/" + instance + "/" + string(kind)

	s.mu.Lock()
	if e, ok := s.baseCache[key]; ok && time.Since(e.at) < s.cfg.Datasource.QueryCacheTTL {
		s.mu.Unlock()

		return e.b
	}
	s.mu.Unlock()

	const step = 30 * time.Minute

	to := time.Now()

	var b Baseline

	sigs, err := s.Collector().Range(ctx, cluster, instance,
		Range{Start: to.Add(-s.cfg.Baseline.Window), End: to, Step: step}, kind)
	if err == nil {
		series := make([]SeriesPoint, 0, len(sigs))

		for _, sig := range sigs {
			if v, ok := sig.Get(kind); ok {
				series = append(series, SeriesPoint{Time: sig.At, Value: v})
			}
		}

		b = BuildBaseline(series, int(s.cfg.Baseline.MinHistory/step))
	}

	s.mu.Lock()
	s.baseCache[key] = baselineEntry{b: b, at: time.Now()}
	s.mu.Unlock()

	return b
}
