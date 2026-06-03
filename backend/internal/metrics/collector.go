package metrics

import (
	"context"
	"sort"
	"time"
)

// Collector assembles normalized Signals for a target by querying the
// datasource, picking a provider per signal-role and rendering the matching
// catalog expression. Signals a provider does not expose stay absent (Have=false).
type Collector struct {
	matcher *Matcher
	catalog *QueryCatalog
	client  DatasourceClient
	window  string // rate window for counter signals, e.g. "5m"
}

// NewCollector wires the collector. window is the PromQL rate window string.
func NewCollector(m *Matcher, c *QueryCatalog, client DatasourceClient, window string) *Collector {
	if window == "" {
		window = "5m"
	}

	return &Collector{matcher: m, catalog: c, client: client, window: window}
}

// CoreSignals is the default set collected for a score. Extended as signals
// come online (Phase 4); a provider that lacks one simply skips it.
var CoreSignals = []SignalKind{
	SigTotalConns, SigActiveConns, SigIdleInTx, SigMaxConns,
	SigCacheHitRatio, SigMaxDeadRatio, SigAvgDeadRatio, SigHotUpdateRatio, SigMaxVacuumAgeH,
	SigDeadlocksTotal,
	SigReplLagSeconds, SigReplLagBytes,
	SigTimedCheckpoints, SigRequestedCheckpoints, SigLocksNotGranted, SigActiveLockWaiters,
	SigXactsLeftWrap, SigChecksumFailRate,
	SigMaxBloatRatio, SigSeqExhaustionMax, SigTypeExhaustionMax,
	SigLatencyMs,
	SigLoadAvg15, SigNumVCPU,
	SigPoolerClients, SigPoolerServers, SigPoolerPoolSize,
}

// signalRole maps a signal to the provider-role that serves it.
func signalRole(s SignalKind) Role {
	switch s {
	case SigPoolerClients, SigPoolerServers, SigPoolerPoolSize:
		return RolePooler
	case SigLoadAvg15, SigNumVCPU:
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
	rt, err := co.matcher.Resolve(cluster, instance)
	if err != nil {
		return Signals{}, err
	}

	if len(sigs) == 0 {
		sigs = CoreSignals
	}

	out := NewSignals(at)

	for _, sig := range sigs {
		expr, ok := co.exprFor(rt, sig)
		if !ok {
			continue // provider does not expose this signal -> Have=false
		}

		samples, err := co.client.QueryInstant(ctx, expr, at)
		if err != nil {
			return Signals{}, err
		}

		if len(samples) > 0 {
			out.Set(sig, samples[0].Value)
		}
	}

	return out, nil
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

	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

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
		return "", false
	}

	return co.catalog.Expr(provider, sig, sel, co.window)
}
