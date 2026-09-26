package healthscore

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/metrics"
)

var (
	ErrUnknownCluster       = errors.New("unknown cluster")
	ErrExhaustiveNotAllowed = errors.New("exhaustive fleet scan is disabled")

	errBudgetExceeded = errors.New("budget exceeded")
	errNoMetrics      = errors.New("no metrics for target")
)

type FleetRequest struct {
	Limit      int
	Clusters   []string // empty = all
	Exhaustive bool
}

type FleetItem struct {
	Target          metrics.TargetRef
	Score           *float64
	Source          Source
	InRecovery      bool
	MetricsDegraded bool
	Err             string

	uncomputed bool
}

type FleetResult struct {
	Items              []FleetItem
	InstancesTotal     int
	InstancesScored    int
	Candidates         int
	Uncomputed         int
	Incomplete         bool
	MetricsUnavailable bool
	ComputedAt         time.Time
	Duration           time.Duration
}

type FleetMetrics interface {
	Enabled() bool
	InstantMany(ctx context.Context, targets []metrics.TargetRef, sigs ...metrics.SignalKind) (map[metrics.TargetRef]metrics.Signals, map[metrics.TargetRef]error)
	CurrentRawMany(ctx context.Context, targets []metrics.TargetRef) map[metrics.TargetRef]metrics.RawResult
}

type ClusterLister interface {
	Clusters(ctx context.Context) ([]dto.ClusterInfo, error)
}

type instanceScorer interface {
	Score(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult) (InstanceScore, error)
	Weights(ctx context.Context, clusterName string) (health.Weights, error)
}

type Fleet struct {
	cfg           config.FleetConfig
	seqThresholds map[string]float64
	scorer        instanceScorer
	metrics       FleetMetrics
	clusters      ClusterLister
	log           *zap.Logger

	mu      sync.Mutex
	flights map[string]*fleetFlight
	known   map[metrics.TargetRef]knownScore
}

type fleetFlight struct {
	done    chan struct{}
	limit   int
	res     FleetResult
	expires time.Time
}

// knownScore remembers the last exact score; topAt is when it last made the
// returned top.
type knownScore struct {
	score float64
	at    time.Time
	topAt time.Time
}

// NewFleet builds the overview. A nil or disabled ms scores the whole fleet
// through the SQL snapshot.
func NewFleet(cfg *config.Config, scorer *Scorer, ms FleetMetrics, clusters ClusterLister, logger *zap.Logger) *Fleet {
	return newFleet(cfg.HealthScore.Fleet, cfg.SchemaLint.SequenceThresholds, scorer, ms, clusters, logger)
}

func newFleet(cfg config.FleetConfig, seq map[string]float64, scorer instanceScorer, ms FleetMetrics, clusters ClusterLister, logger *zap.Logger) *Fleet {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Fleet{
		cfg:           cfg.WithDefaults(),
		seqThresholds: seq,
		scorer:        scorer,
		metrics:       ms,
		clusters:      clusters,
		log:           logger,
		flights:       make(map[string]*fleetFlight),
		known:         make(map[metrics.TargetRef]knownScore),
	}
}

// Worst returns the lowest-scored instances. Concurrent calls with the same
// cluster filter share one computation, detached from the callers'
// cancellation, and its result is reused for result_ttl by any call whose limit
// it covers.
func (f *Fleet) Worst(ctx context.Context, req FleetRequest) (FleetResult, error) {
	if req.Exhaustive && !f.cfg.AllowExhaustive {
		return FleetResult{}, ErrExhaustiveNotAllowed
	}

	if req.Limit <= 0 {
		req.Limit = f.cfg.DefaultLimit
	}

	targets, err := f.targets(ctx, req.Clusters)
	if err != nil {
		return FleetResult{}, err
	}

	fl := f.join(ctx, req, targets)

	select {
	case <-fl.done:
	case <-ctx.Done():
		return FleetResult{}, ctx.Err()
	}

	res := fl.res
	res.Items = slices.Clone(res.Items[:min(len(res.Items), req.Limit)])

	return res, nil
}

func (f *Fleet) join(ctx context.Context, req FleetRequest, targets []metrics.TargetRef) *fleetFlight {
	key := flightKey(req)
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()

	if fl, ok := f.flights[key]; ok && fl.limit >= req.Limit {
		select {
		case <-fl.done:
			if now.Before(fl.expires) {
				return fl
			}
		default:
			return fl
		}
	}

	fl := &fleetFlight{done: make(chan struct{}), limit: max(req.Limit, f.cfg.DefaultLimit)}
	f.flights[key] = fl

	go func() {
		fl.res = f.compute(context.WithoutCancel(ctx), targets, fl.limit, req.Exhaustive)
		fl.expires = time.Now().Add(f.cfg.ResultTTL)
		close(fl.done)

		time.AfterFunc(f.cfg.ResultTTL, func() {
			f.mu.Lock()
			defer f.mu.Unlock()

			if f.flights[key] == fl {
				delete(f.flights, key)
			}
		})
	}()

	return fl
}

func flightKey(req FleetRequest) string {
	cl := slices.Clone(req.Clusters)
	slices.Sort(cl)
	cl = slices.Compact(cl)

	return fmt.Sprintf("%t\x00%s", req.Exhaustive, strings.Join(cl, "\x00"))
}

func (f *Fleet) targets(ctx context.Context, filter []string) ([]metrics.TargetRef, error) {
	clusters, err := f.clusters.Clusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("Fleet | Clusters | %w", err)
	}

	want := make(map[string]bool, len(filter))
	for _, name := range filter {
		want[name] = true
	}

	var out []metrics.TargetRef

	found := make(map[string]bool, len(want))

	for _, c := range clusters {
		name := c.Name.String()
		if len(want) > 0 && !want[name] {
			continue
		}

		found[name] = true

		for _, inst := range c.Instances {
			out = append(out, metrics.TargetRef{Cluster: name, Instance: inst.HostName.String()})
		}
	}

	var missing []string

	for name := range want {
		if !found[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)

		return nil, fmt.Errorf("%w: %s", ErrUnknownCluster, strings.Join(missing, ", "))
	}

	slices.SortFunc(out, compareTargets)

	return out, nil
}

// fleetPlan is the phase-2 queue: metrics candidates first, then the
// snapshot-only instances.
type fleetPlan struct {
	candidates  []metrics.TargetRef
	sqlOnly     []metrics.TargetRef
	withMetrics int
	floor       int
	sticky      int
	unmapped    int
	failed      int
	unavailable bool
}

func (f *Fleet) compute(ctx context.Context, targets []metrics.TargetRef, limit int, exhaustive bool) FleetResult {
	start := time.Now()
	stats := &metrics.QueryStats{}

	ctx, cancel := context.WithTimeout(metrics.WithQueryStats(ctx, stats), f.cfg.Budget)
	defer cancel()

	plan := f.prefilter(ctx, targets, limit, exhaustive)
	phase1 := time.Since(start)

	items := f.scoreAll(ctx, plan)
	f.remember(items, limit, start)

	res := FleetResult{
		InstancesTotal:     len(targets),
		Candidates:         len(plan.candidates),
		MetricsUnavailable: plan.unavailable,
		ComputedAt:         start,
	}

	errs := 0

	for _, it := range items {
		switch {
		case it.Score != nil:
			res.InstancesScored++
		case it.uncomputed:
			res.Uncomputed++
		default:
			errs++
		}
	}

	res.Incomplete = res.Uncomputed > 0
	res.Items = items[:min(len(items), limit)]
	res.Duration = time.Since(start)

	counts := stats.Counts()

	if plan.unmapped > 0 || plan.failed > 0 {
		f.log.Warn("fleet health: targets without prefilter signals",
			zap.Int("unmapped", plan.unmapped),
			zap.Int("failed", plan.failed),
			zap.Bool("metrics_unavailable", plan.unavailable),
		)
	}

	f.log.Info("fleet health: done",
		zap.Int("instances", len(targets)),
		zap.Int("metrics", plan.withMetrics),
		zap.Int("sql_only", len(plan.sqlOnly)),
		zap.Int("floor", plan.floor),
		zap.Int("sticky", plan.sticky),
		zap.Int("candidates", len(plan.candidates)),
		zap.Int("vm_instant", counts.Instant),
		zap.Int("vm_range", counts.Range),
		zap.Any("vm_status", counts.ByCode),
		zap.Int64("phase1_ms", phase1.Milliseconds()),
		zap.Int64("phase2_ms", (res.Duration-phase1).Milliseconds()),
		zap.Int("errors", errs),
		zap.Int("uncomputed", res.Uncomputed),
		zap.Bool("incomplete", res.Incomplete),
		zap.Bool("exhaustive", exhaustive),
	)

	return res
}

// prefilter splits the fleet into metrics candidates and snapshot-only
// instances. Exhaustive makes every target a candidate.
func (f *Fleet) prefilter(ctx context.Context, targets []metrics.TargetRef, limit int, exhaustive bool) fleetPlan {
	var plan fleetPlan

	if f.metrics == nil || !f.metrics.Enabled() {
		plan.sqlOnly = f.byKnownScore(targets)

		return plan
	}

	if exhaustive {
		plan.candidates = targets
		plan.withMetrics = len(targets)

		return plan
	}

	sigs, errs := f.metrics.InstantMany(ctx, targets, metrics.PrefilterSignals...)

	type ranked struct {
		t     metrics.TargetRef
		score float64
		floor bool
	}

	var (
		rankedAll []ranked
		unranked  []metrics.TargetRef
		failed    []metrics.TargetRef
		sqlOnly   []metrics.TargetRef
	)

	weights := make(map[string]health.Weights)

	for _, t := range targets {
		if err := errs[t]; err != nil {
			if errors.Is(err, metrics.ErrTargetNotMapped) {
				sqlOnly = append(sqlOnly, t)
				plan.unmapped++
			} else {
				failed = append(failed, t)
			}

			continue
		}

		sig := sigs[t]
		if len(sig.Have) == 0 {
			unranked = append(unranked, t)

			continue
		}

		w, ok := weights[t.Cluster]
		if !ok {
			w, _ = f.scorer.Weights(ctx, t.Cluster)
			weights[t.Cluster] = w
		}

		raw := sig.ToRawMetrics()
		raw.SequenceThresholds = f.seqThresholds

		rankedAll = append(rankedAll, ranked{t: t, score: health.CalculateWithWeights(raw, w).Score, floor: health.Floored(raw)})
	}

	plan.failed = len(failed)

	if len(failed) > 0 && len(rankedAll) == 0 && len(unranked) == 0 && ctx.Err() == nil {
		plan.unavailable = true
		sqlOnly = append(sqlOnly, failed...)
		failed = nil
	}

	plan.withMetrics = len(targets) - len(sqlOnly)

	slices.SortStableFunc(rankedAll, func(a, b ranked) int { return cmp.Compare(a.score, b.score) })

	picked := make(map[metrics.TargetRef]bool)
	add := func(t metrics.TargetRef) bool {
		if picked[t] {
			return false
		}

		picked[t] = true
		plan.candidates = append(plan.candidates, t)

		return true
	}

	for _, r := range rankedAll {
		if r.floor && add(r.t) {
			plan.floor++
		}
	}

	now := time.Now()

	f.mu.Lock()
	for _, r := range rankedAll {
		if k, ok := f.known[r.t]; ok && now.Sub(k.topAt) < f.cfg.StickyTTL && add(r.t) {
			plan.sticky++
		}
	}
	f.mu.Unlock()

	for _, r := range rankedAll[:min(len(rankedAll), limit+*f.cfg.CandidateMargin)] {
		add(r.t)
	}

	for _, t := range unranked {
		add(t)
	}

	for _, t := range failed {
		add(t)
	}

	plan.sqlOnly = f.byKnownScore(sqlOnly)

	return plan
}

// byKnownScore orders targets by their last exact score, unknown last.
func (f *Fleet) byKnownScore(targets []metrics.TargetRef) []metrics.TargetRef {
	now := time.Now()
	scores := make(map[metrics.TargetRef]float64)

	f.mu.Lock()
	for _, t := range targets {
		if k, ok := f.known[t]; ok && now.Sub(k.at) < f.cfg.StickyTTL {
			scores[t] = k.score
		}
	}
	f.mu.Unlock()

	out := slices.Clone(targets)
	slices.SortStableFunc(out, func(a, b metrics.TargetRef) int {
		sa, okA := scores[a]
		sb, okB := scores[b]

		switch {
		case okA && okB:
			return cmp.Compare(sa, sb)
		case okA:
			return -1
		case okB:
			return 1
		default:
			return 0
		}
	})

	return out
}

// scoreAll scores the plan's queue in order and returns the rows sorted worst
// first, unscored last. Rows the budget did not reach are marked uncomputed.
func (f *Fleet) scoreAll(ctx context.Context, plan fleetPlan) []FleetItem {
	type queued struct {
		t   metrics.TargetRef
		pre *metrics.RawResult
	}

	queue := make([]queued, 0, len(plan.candidates)+len(plan.sqlOnly))

	if len(plan.candidates) > 0 {
		raws := f.metrics.CurrentRawMany(ctx, plan.candidates)

		for _, t := range plan.candidates {
			r, ok := raws[t]
			if !ok {
				r = metrics.RawResult{Err: errNoMetrics}
			}

			queue = append(queue, queued{t: t, pre: &r})
		}
	}

	for _, t := range plan.sqlOnly {
		queue = append(queue, queued{t: t, pre: &metrics.RawResult{Err: errNoMetrics}})
	}

	items := make([]FleetItem, len(queue))
	for i, q := range queue {
		items[i] = uncomputedItem(q.t)
	}

	sem := make(chan struct{}, f.cfg.SnapshotConcurrency)

	var wg sync.WaitGroup

enqueue:
	for i, q := range queue {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break enqueue
		}

		wg.Go(func() {
			defer func() { <-sem }()

			items[i] = f.scoreOne(ctx, q.t, q.pre)
		})
	}

	wg.Wait()

	slices.SortStableFunc(items, func(a, b FleetItem) int {
		switch {
		case a.Score != nil && b.Score != nil:
			return cmp.Or(cmp.Compare(*a.Score, *b.Score), compareTargets(a.Target, b.Target))
		case a.Score != nil:
			return -1
		case b.Score != nil:
			return 1
		default:
			return compareTargets(a.Target, b.Target)
		}
	})

	return items
}

func (f *Fleet) scoreOne(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult) FleetItem {
	ictx, cancel := context.WithTimeout(ctx, f.cfg.InstanceTimeout)
	defer cancel()

	sc, err := f.scorer.Score(ictx, t, pre)
	if err != nil {
		if ctx.Err() != nil {
			return uncomputedItem(t)
		}

		return FleetItem{Target: t, Source: SourceNone, Err: err.Error()}
	}

	score := sc.Result.Score

	return FleetItem{
		Target:          t,
		Score:           &score,
		Source:          sc.Source,
		InRecovery:      sc.Result.InRecovery,
		MetricsDegraded: sc.MetricsDegraded,
	}
}

func uncomputedItem(t metrics.TargetRef) FleetItem {
	return FleetItem{Target: t, Source: SourceNone, Err: errBudgetExceeded.Error(), uncomputed: true}
}

// remember records the exact scores of a finished computation; the first
// limit rows become sticky candidates for the next ones.
func (f *Fleet) remember(items []FleetItem, limit int, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, it := range items {
		if it.Score == nil {
			continue
		}

		k := f.known[it.Target]
		k.score, k.at = *it.Score, at

		if i < limit {
			k.topAt = at
		}

		f.known[it.Target] = k
	}

	for t, k := range f.known {
		if at.Sub(k.at) >= f.cfg.StickyTTL && at.Sub(k.topAt) >= f.cfg.StickyTTL {
			delete(f.known, t)
		}
	}
}

func compareTargets(a, b metrics.TargetRef) int {
	return cmp.Or(cmp.Compare(a.Cluster, b.Cluster), cmp.Compare(a.Instance, b.Instance))
}
