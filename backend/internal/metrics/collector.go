package metrics

import (
	"context"
	"slices"
	"time"

	"go.uber.org/zap"
)

// Collector assembles normalized Signals for a target by querying the
// datasource, picking a provider per signal-role and rendering the matching
// catalog expression. Signals a provider does not expose stay absent (Have=false).
type Collector struct {
	matcher *Matcher
	catalog *QueryCatalog
	client  DatasourceClient
	batch   *batcher
	window  string // rate window for counter signals, e.g. "5m"
	exclude string // role-exclusion label fragment for per-role signals (may be empty)
	log     *zap.Logger
}

// BatchLimits bounds one glued datasource request and how many run at once.
type BatchLimits struct {
	MaxQueryBytes  int
	MaxConcurrency int
}

// NewCollector wires the collector. window is the PromQL rate window string;
// exclude is the role-exclusion label fragment (see Config.roleExclusion). Zero
// limits take the datasource defaults. A nil logger is replaced with a no-op.
func NewCollector(m *Matcher, c *QueryCatalog, client DatasourceClient, window, exclude string, limits BatchLimits, logger *zap.Logger) *Collector {
	if window == "" {
		window = "5m"
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	d := Default().Datasource
	if limits.MaxQueryBytes <= 0 {
		limits.MaxQueryBytes = d.MaxQueryBytes
	}

	if limits.MaxConcurrency <= 0 {
		limits.MaxConcurrency = d.MaxConcurrency
	}

	return &Collector{
		matcher: m,
		catalog: c,
		client:  client,
		batch:   newBatcher(client, limits.MaxQueryBytes, limits.MaxConcurrency),
		window:  window,
		exclude: exclude,
		log:     logger,
	}
}

// CoreSignals is the default set collected for a score. Extended as signals
// come online (Phase 4); a provider that lacks one simply skips it.
var CoreSignals = []SignalKind{
	SigTotalConns, SigActiveConns, SigIdleInTx, SigMaxConns,
	SigCacheHitRatio, SigMaxDeadRatio, SigAvgDeadRatio, SigHotUpdateRatio,
	SigDeadlocksTotal,
	SigReplLagSeconds, SigReplLagBytes,
	SigTimedCheckpoints, SigRequestedCheckpoints, SigLocksNotGranted, SigActiveLockWaiters,
	SigXactsLeftWrap, SigChecksumFailRate,
	SigSeqExhaustionMax,
	SigLatencyMs, SigSeqScanRate,
	SigLoadAvg15, SigNumVCPU, SigDiskUsedRatio,
	SigPoolerServers, SigPoolerPoolSize,
}

// signalRole maps a signal to the provider-role that serves it.
func signalRole(s SignalKind) Role {
	switch s {
	case SigPoolerClients, SigPoolerServers, SigPoolerPoolSize:
		return RolePooler
	case SigLoadAvg15, SigNumVCPU, SigDiskUsedRatio:
		return RoleHost
	default:
		return RoleCore
	}
}

func providerForRole(rt ResolvedTarget, role Role) Provider {
	switch role {
	case RolePooler:
		return rt.Providers.Pooler
	case RoleHost:
		return rt.Providers.Host
	default:
		return rt.Providers.Core
	}
}

// Instant collects the given signals (or CoreSignals) at time at.
func (co *Collector) Instant(ctx context.Context, cluster, instance string, at time.Time, sigs ...SignalKind) (Signals, error) {
	t := TargetRef{Cluster: cluster, Instance: instance}

	out, errs := co.InstantMany(ctx, []TargetRef{t}, at, sigs...)
	if err := errs[t]; err != nil {
		return Signals{}, err
	}

	return out[t], nil
}

// InstantMany collects the given signals (or CoreSignals) for every target in
// glued requests. A target that did not resolve or whose request failed is in
// the error map only.
func (co *Collector) InstantMany(ctx context.Context, targets []TargetRef, at time.Time, sigs ...SignalKind) (map[TargetRef]Signals, map[TargetRef]error) {
	if len(sigs) == 0 {
		sigs = CoreSignals
	}

	items, keyErrs := co.plan(targetKeys(targets, sigs))
	vals, fails := co.batch.instant(ctx, items, at)

	errs := make(map[TargetRef]error)

	for k, err := range keyErrs {
		errs[k.Target] = err
	}

	for _, f := range fails {
		for _, k := range f.Keys {
			if _, seen := errs[k.Target]; !seen {
				errs[k.Target] = f.Err
			}
		}
	}

	out := make(map[TargetRef]Signals, len(targets))

	for _, t := range targets {
		if _, failed := errs[t]; !failed {
			out[t] = NewSignals(at)
		}
	}

	for k, v := range vals {
		if s, ok := out[k.Target]; ok {
			s.Set(k.Signal, v)
			out[k.Target] = s
		}
	}

	return out, errs
}

// rangeKeys fetches the series of each key over r in glued requests. A key that
// did not resolve or whose request failed is in the error map.
func (co *Collector) rangeKeys(ctx context.Context, keys []batchKey, r Range) (map[batchKey][]SeriesPoint, map[batchKey]error) {
	items, errs := co.plan(keys)
	pts, fails := co.batch.rangeQ(ctx, items, r)

	for _, f := range fails {
		for _, k := range f.Keys {
			errs[k] = f.Err
		}
	}

	return pts, errs
}

func targetKeys(targets []TargetRef, sigs []SignalKind) []batchKey {
	keys := make([]batchKey, 0, len(targets)*len(sigs))

	for _, t := range targets {
		for _, sig := range sigs {
			keys = append(keys, batchKey{Target: t, Signal: sig})
		}
	}

	return keys
}

// plan renders the keys into batch items, one per distinct expression.
// Uncatalogued keys are dropped; keys of an unresolved target are errors.
func (co *Collector) plan(keys []batchKey) ([]batchItem, map[batchKey]error) {
	var items []batchItem

	errs := make(map[batchKey]error)
	byExpr := make(map[string]int)
	resolved := make(map[TargetRef]ResolvedTarget)
	resolveErr := make(map[TargetRef]error)

	for _, k := range keys {
		rt, ok := resolved[k.Target]
		if !ok {
			if err, failed := resolveErr[k.Target]; failed {
				errs[k] = err

				continue
			}

			var err error

			rt, err = co.matcher.Resolve(k.Target.Cluster, k.Target.Instance)
			if err != nil {
				resolveErr[k.Target] = err
				errs[k] = err

				continue
			}

			resolved[k.Target] = rt
		}

		expr, ok := co.exprFor(rt, k.Signal)
		if !ok {
			continue // provider does not expose this signal -> Have=false
		}

		if i, seen := byExpr[expr]; seen {
			items[i].Keys = append(items[i].Keys, k)

			continue
		}

		byExpr[expr] = len(items)
		items = append(items, batchItem{Expr: expr, Keys: []batchKey{k}})
	}

	return items, errs
}

// Range collects the given signals over r and aligns them into per-timestamp
// Signals sorted ascending.
func (co *Collector) Range(ctx context.Context, cluster, instance string, r Range, sigs ...SignalKind) ([]Signals, error) {
	rt, err := co.matcher.Resolve(cluster, instance)
	if err != nil {
		return nil, err
	}

	if len(sigs) == 0 {
		sigs = CoreSignals
	}

	byTS := make(map[int64]Signals)

	for _, sig := range sigs {
		expr, ok := co.exprFor(rt, sig)
		if !ok {
			continue
		}

		series, err := co.client.QueryRange(ctx, expr, r)
		if err != nil {
			return nil, err
		}

		if len(series) == 0 {
			continue
		}

		// Catalog expressions aggregate to a single series per target.
		for _, p := range series[0].Points {
			key := p.Time.Unix()

			s, exists := byTS[key]
			if !exists {
				s = NewSignals(p.Time)
				byTS[key] = s
			}

			s.Set(sig, p.Value)
		}
	}

	keys := make([]int64, 0, len(byTS))
	for k := range byTS {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	out := make([]Signals, 0, len(keys))
	for _, k := range keys {
		out = append(out, byTS[k])
	}

	return out, nil
}

// exprFor renders the catalog expression for a signal under the target's
// role-provider, returning false when uncatalogued.
func (co *Collector) exprFor(rt ResolvedTarget, sig SignalKind) (string, bool) {
	role := signalRole(sig)
	provider := providerForRole(rt, role)

	sel, err := co.matcher.Selector(provider, role, rt)
	if err != nil {
		// A selector failure is a config/template problem (not a missing signal),
		// so surface it instead of silently dropping the signal.
		co.log.Warn("metrics: selector unavailable, skipping signal",
			zap.String("provider", string(provider)),
			zap.String("role", string(role)),
			zap.String("signal", string(sig)),
			zap.String("env", rt.Env),
			zap.String("service", rt.Service),
			zap.String("host", rt.Host),
			zap.Error(err),
		)

		return "", false
	}

	return co.catalog.Expr(provider, sig, sel, co.window, co.exclude)
}
