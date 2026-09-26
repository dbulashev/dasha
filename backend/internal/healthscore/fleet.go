package healthscore

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
	ErrFleetBusy            = errors.New("too many fleet overviews in progress")

	errBudgetExceeded = errors.New("budget exceeded")
	errNoMetrics      = errors.New("no metrics for target")
	errInternal       = errors.New("internal error")
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
	ScoreWith(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult, cc ClusterContext) (InstanceScore, error)
	Weights(ctx context.Context, clusterName string) (health.Weights, error)
}

const (
	metricsBudgetDivisor = 3
	sweepChunk           = 32
	sweepBackoff         = 100 * time.Millisecond
)

type Fleet struct {
	cfg           config.FleetConfig
	seqThresholds map[string]float64
	scorer        instanceScorer
	metrics       FleetMetrics
	clusters      ClusterLister
	log           *zap.Logger

	mu        sync.Mutex
	flights   map[string]*fleetFlight
	known     map[metrics.TargetRef]knownScore
	computing int

	// slots is the snapshot_concurrency pool shared by every computation.
	slots    chan struct{}
	waiting  atomic.Int32
	sweeping atomic.Bool
}

type fleetFlight struct {
	done    chan struct{}
	res     FleetResult
	err     error
	expires time.Time
}

// knownScore remembers the last exact score; topAt is when it last made the
// returned top.
type knownScore struct {
	score float64
	at    time.Time
	topAt time.Time
}

type clusterInputs struct {
	cc  ClusterContext
	err error
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
		slots:         make(chan struct{}, cfg.WithDefaults().SnapshotConcurrency),
		known:         make(map[metrics.TargetRef]knownScore),
	}
}

// Worst returns the lowest-scored instances. Concurrent calls over the same
// set of instances share one computation of the top MaxFleetLimit, detached
// from the callers' cancellation and reused for result_ttl. A call that needs a
// new computation while max_computations are running gets ErrFleetBusy.
func (f *Fleet) Worst(ctx context.Context, req FleetRequest) (FleetResult, error) {
	if req.Exhaustive && !f.cfg.AllowExhaustive {
		return FleetResult{}, ErrExhaustiveNotAllowed
	}

	if req.Limit <= 0 {
		req.Limit = f.cfg.DefaultLimit
	}

	targets, walFixed, err := f.targets(ctx, req.Clusters)
	if err != nil {
		return FleetResult{}, err
	}

	fl, err := f.join(ctx, req.Exhaustive, targets, walFixed)
	if err != nil {
		return FleetResult{}, err
	}

	select {
	case <-fl.done:
	case <-ctx.Done():
		return FleetResult{}, ctx.Err()
	}

	if fl.err != nil {
		return FleetResult{}, fl.err
	}

	res := fl.res
	res.Items = slices.Clone(res.Items[:min(len(res.Items), req.Limit)])

	return res, nil
}

// RetryAfter is how long a caller turned away with ErrFleetBusy should wait.
func (f *Fleet) RetryAfter() time.Duration {
	return f.cfg.Budget
}

func (f *Fleet) join(ctx context.Context, exhaustive bool, targets []metrics.TargetRef, walFixed map[string]bool) (*fleetFlight, error) {
	key := flightKey(exhaustive, targets)
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()

	if fl, ok := f.flights[key]; ok {
		select {
		case <-fl.done:
			if now.Before(fl.expires) {
				return fl, nil
			}
		default:
			return fl, nil
		}
	}

	if f.computing >= f.cfg.MaxComputations {
		return nil, ErrFleetBusy
	}

	f.computing++

	fl := &fleetFlight{done: make(chan struct{})}
	f.flights[key] = fl

	go f.run(context.WithoutCancel(ctx), key, fl, exhaustive, targets, walFixed)

	return fl, nil
}

// run computes the flight, publishes it, then spends the rest of the budget on
// the sweep.
func (f *Fleet) run(ctx context.Context, key string, fl *fleetFlight, exhaustive bool, targets []metrics.TargetRef, walFixed map[string]bool) {
	release := sync.OnceFunc(func() {
		f.mu.Lock()
		f.computing--
		f.mu.Unlock()
	})
	defer release()

	var once sync.Once

	publish := func(res FleetResult, err error) {
		once.Do(func() {
			hold := f.cfg.ResultTTL
			if err != nil {
				hold = 0
			}

			fl.res, fl.err, fl.expires = res, err, time.Now().Add(hold)
			close(fl.done)

			time.AfterFunc(hold, func() {
				f.mu.Lock()
				defer f.mu.Unlock()

				if f.flights[key] == fl {
					delete(f.flights, key)
				}
			})
		})
	}

	defer func() {
		if r := recover(); r != nil {
			f.log.Error("fleet health: panic", zap.Any("panic", r), zap.Stack("stack"))
			publish(FleetResult{}, errInternal)
		}
	}()

	start := time.Now()
	stats := &metrics.QueryStats{}

	ctx, cancel := context.WithTimeout(metrics.WithQueryStats(ctx, stats), f.cfg.Budget)
	defer cancel()

	clusters := f.clusterInputs(ctx, walFixed)
	res, sweep := f.compute(ctx, targets, clusters, exhaustive, start, stats)
	publish(res, nil)
	release()

	if f.sweeping.CompareAndSwap(false, true) {
		defer f.sweeping.Store(false)

		f.sweep(ctx, sweep, clusters, start)
	}
}

// flightKey identifies a computation by the instances it covers; targets are
// sorted.
func flightKey(exhaustive bool, targets []metrics.TargetRef) string {
	h := fnv.New64a()

	for _, t := range targets {
		fmt.Fprintf(h, "%s\x00%s\x00", t.Cluster, t.Instance)
	}

	return fmt.Sprintf("%t/%x", exhaustive, h.Sum64())
}

// targets lists the filtered fleet and, per cluster, whether its provider fixes
// wal_level.
func (f *Fleet) targets(ctx context.Context, filter []string) ([]metrics.TargetRef, map[string]bool, error) {
	clusters, err := f.clusters.Clusters(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("Fleet | Clusters | %w", err)
	}

	want := make(map[string]bool, len(filter))
	for _, name := range filter {
		want[name] = true
	}

	var out []metrics.TargetRef

	walFixed := make(map[string]bool, len(clusters))

	for _, c := range clusters {
		name := c.Name.String()
		if len(want) > 0 && !want[name] {
			continue
		}

		walFixed[name] = walLevelFixed(c)

		for _, inst := range c.Instances {
			out = append(out, metrics.TargetRef{Cluster: name, Instance: inst.HostName.String()})
		}
	}

	var missing []string

	for name := range want {
		if _, found := walFixed[name]; !found {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)

		return nil, nil, fmt.Errorf("%w: %s", ErrUnknownCluster, strings.Join(missing, ", "))
	}

	slices.SortFunc(out, compareTargets)

	return out, walFixed, nil
}

func (f *Fleet) clusterInputs(ctx context.Context, walFixed map[string]bool) map[string]clusterInputs {
	out := make(map[string]clusterInputs, len(walFixed))

	for name, fixed := range walFixed {
		w, err := f.scorer.Weights(ctx, name)
		ci := clusterInputs{cc: ClusterContext{Weights: w, WalLevelManaged: fixed}}

		if err != nil {
			ci.err = fmt.Errorf("Score | Weights | %w", err)
		}

		out[name] = ci
	}

	return out
}

func (f *Fleet) metricsTimeout() time.Duration {
	return f.cfg.Budget / metricsBudgetDivisor
}

// fleetPlan is the phase-2 queue: metrics candidates first, then the
// snapshot-only instances. sweep is the rest of the ranked fleet, stalest
// known score first.
type fleetPlan struct {
	candidates  []metrics.TargetRef
	sqlOnly     []metrics.TargetRef
	sweep       []metrics.TargetRef
	withMetrics int
	floor       int
	sticky      int
	unmapped    int
	failed      int
	unavailable bool
}

func (f *Fleet) compute(
	ctx context.Context,
	targets []metrics.TargetRef,
	clusters map[string]clusterInputs,
	exhaustive bool,
	start time.Time,
	stats *metrics.QueryStats,
) (FleetResult, []metrics.TargetRef) {
	plan := f.prefilter(ctx, targets, clusters, exhaustive)
	phase1 := time.Since(start)

	items := f.scoreAll(ctx, plan, clusters, f.acquire)
	f.remember(items, f.cfg.DefaultLimit, start)

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
	res.Items = items[:min(len(items), config.MaxFleetLimit)]
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
		zap.Int("sweep", len(plan.sweep)),
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

	return res, plan.sweep
}

// prefilter splits the fleet into metrics candidates, snapshot-only instances
// and the sweep. Exhaustive makes every target a candidate. An instance ranks
// by the lower of its metrics estimate and its last exact score.
func (f *Fleet) prefilter(ctx context.Context, targets []metrics.TargetRef, clusters map[string]clusterInputs, exhaustive bool) fleetPlan {
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

	mctx, cancel := context.WithTimeout(ctx, f.metricsTimeout())
	sigs, errs := f.metrics.InstantMany(mctx, targets, metrics.PrefilterSignals...)

	cancel()

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

		raw := sig.ToRawMetrics()
		raw.SequenceThresholds = f.seqThresholds

		rankedAll = append(rankedAll, ranked{
			t:     t,
			score: health.CalculateWithWeights(raw, clusters[t.Cluster].cc.Weights).Score,
			floor: health.Floored(raw),
		})
	}

	plan.failed = len(failed)

	if len(failed) > 0 && len(rankedAll) == 0 && len(unranked) == 0 && ctx.Err() == nil {
		plan.unavailable = true
		sqlOnly = append(sqlOnly, failed...)
		failed = nil
	}

	plan.withMetrics = len(targets) - len(sqlOnly)

	now := time.Now()
	sticky := make(map[metrics.TargetRef]bool)

	f.mu.Lock()
	for i, r := range rankedAll {
		k, ok := f.known[r.t]
		if !ok {
			continue
		}

		if now.Sub(k.at) < f.cfg.StickyTTL {
			rankedAll[i].score = min(r.score, k.score)
		}

		if now.Sub(k.topAt) < f.cfg.StickyTTL {
			sticky[r.t] = true
		}
	}
	f.mu.Unlock()

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

	for _, r := range rankedAll {
		if sticky[r.t] && add(r.t) {
			plan.sticky++
		}
	}

	for _, r := range rankedAll[:min(len(rankedAll), config.MaxFleetLimit+*f.cfg.CandidateMargin)] {
		add(r.t)
	}

	for _, t := range unranked {
		add(t)
	}

	for _, t := range failed {
		add(t)
	}

	for _, r := range rankedAll {
		if !picked[r.t] {
			plan.sweep = append(plan.sweep, r.t)
		}
	}

	plan.sqlOnly = f.byKnownScore(sqlOnly)
	plan.sweep = f.stalestFirst(plan.sweep)

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

// stalestFirst orders targets by the age of their last exact score, unknown
// first.
func (f *Fleet) stalestFirst(targets []metrics.TargetRef) []metrics.TargetRef {
	at := make(map[metrics.TargetRef]time.Time, len(targets))

	f.mu.Lock()
	for _, t := range targets {
		at[t] = f.known[t].at
	}
	f.mu.Unlock()

	slices.SortStableFunc(targets, func(a, b metrics.TargetRef) int { return at[a].Compare(at[b]) })

	return targets
}

// sweep exact-scores the instances the prefilter passed over, for the ranking
// of the next computations.
func (f *Fleet) sweep(ctx context.Context, targets []metrics.TargetRef, clusters map[string]clusterInputs, at time.Time) {
	scored := 0

	for chunk := range slices.Chunk(targets, sweepChunk) {
		if ctx.Err() != nil {
			break
		}

		items := f.scoreAll(ctx, fleetPlan{candidates: chunk}, clusters, f.acquireIdle)
		f.remember(items, 0, at)

		for _, it := range items {
			if it.Score != nil {
				scored++
			}
		}
	}

	if len(targets) > 0 {
		f.log.Info("fleet health: sweep done", zap.Int("targets", len(targets)), zap.Int("scored", scored))
	}
}

type fleetJob struct {
	i   int
	t   metrics.TargetRef
	pre *metrics.RawResult
}

// scoreAll scores the plan's queue and returns the rows sorted worst first,
// unscored last. Snapshot-only instances start while the candidates' metrics
// are read; candidates go first once they are in. Rows the budget did not reach
// are marked uncomputed.
func (f *Fleet) scoreAll(ctx context.Context, plan fleetPlan, clusters map[string]clusterInputs, acquire func(context.Context) bool) []FleetItem {
	cands, sqlOnly := plan.candidates, plan.sqlOnly

	items := make([]FleetItem, 0, len(cands)+len(sqlOnly))
	for _, t := range slices.Concat(cands, sqlOnly) {
		items = append(items, uncomputedItem(t))
	}

	raws := make(chan map[metrics.TargetRef]metrics.RawResult, 1)
	pending := len(cands) > 0

	if pending {
		go f.readRaw(ctx, cands, raws)
	}

	var (
		got    map[metrics.TargetRef]metrics.RawResult
		ci, si int
		wg     sync.WaitGroup
	)

	candidate := func() fleetJob {
		t := cands[ci]

		r, ok := got[t]
		if !ok {
			r = metrics.RawResult{Err: errNoMetrics}
		}

		ci++

		return fleetJob{i: ci - 1, t: t, pre: &r}
	}

dispatch:
	for ci < len(cands) || si < len(sqlOnly) {
		if !acquire(ctx) {
			break dispatch
		}

		if pending {
			select {
			case got = <-raws:
				pending = false
			default:
			}
		}

		var j fleetJob

		switch {
		case !pending && ci < len(cands):
			j = candidate()
		case si < len(sqlOnly):
			j = fleetJob{i: len(cands) + si, t: sqlOnly[si], pre: &metrics.RawResult{Err: errNoMetrics}}
			si++
		default:
			<-f.slots

			select {
			case got = <-raws:
				pending = false

				continue
			case <-ctx.Done():
				break dispatch
			}
		}

		wg.Go(func() {
			defer func() { <-f.slots }()

			items[j.i] = f.scoreOne(ctx, j.t, j.pre, clusters[j.t.Cluster])
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

func (f *Fleet) acquire(ctx context.Context) bool {
	f.waiting.Add(1)
	defer f.waiting.Add(-1)

	select {
	case f.slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// acquireIdle takes a slot only while no computation is waiting for one.
func (f *Fleet) acquireIdle(ctx context.Context) bool {
	for {
		if f.waiting.Load() == 0 {
			select {
			case f.slots <- struct{}{}:
				return true
			default:
			}
		}

		select {
		case <-time.After(sweepBackoff):
		case <-ctx.Done():
			return false
		}
	}
}

// readRaw always sends one map, nil after a panic.
func (f *Fleet) readRaw(ctx context.Context, targets []metrics.TargetRef, out chan<- map[metrics.TargetRef]metrics.RawResult) {
	var raws map[metrics.TargetRef]metrics.RawResult

	defer func() { out <- raws }()
	defer f.recovered("metrics read")

	mctx, cancel := context.WithTimeout(ctx, f.metricsTimeout())
	defer cancel()

	raws = f.metrics.CurrentRawMany(mctx, targets)
}

func (f *Fleet) recovered(where string) {
	if r := recover(); r != nil {
		f.log.Error("fleet health: panic", zap.String("in", where), zap.Any("panic", r), zap.Stack("stack"))
	}
}

func (f *Fleet) scoreOne(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult, ci clusterInputs) (it FleetItem) {
	defer func() {
		if r := recover(); r != nil {
			f.log.Error("fleet health: panic",
				zap.String("cluster", t.Cluster),
				zap.String("instance", t.Instance),
				zap.Any("panic", r),
				zap.Stack("stack"),
			)

			it = FleetItem{Target: t, Source: SourceNone, Err: errInternal.Error()}
		}
	}()

	if ci.err != nil {
		return FleetItem{Target: t, Source: SourceNone, Err: ci.err.Error()}
	}

	ictx, cancel := context.WithTimeout(ctx, f.cfg.InstanceTimeout)
	defer cancel()

	sc, err := f.scorer.ScoreWith(ictx, t, pre, ci.cc)
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
