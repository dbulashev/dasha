package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetHealthScore(
	ctx context.Context,
	req serverhttp.GetHealthScoreRequestObject,
) (serverhttp.GetHealthScoreResponseObject, error) {
	weights, err := s.loadHealthWeights(ctx, req.Params.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScore | loadHealthWeights | %w", err)
	}

	var (
		result health.Result
		source = "snapshot"
	)

	// Prefer the metrics-backed score when the datasource is configured and the
	// target is mapped; otherwise fall back to the SQL snapshot (graceful). The
	// metrics raw is overlaid with catalog/GUC facts so the score stays in
	// lockstep with its recommendations (score<->rules parity).
	if raw, ok := s.metricsRawWithCatalog(ctx, req.Params.ClusterName, req.Params.Instance); ok {
		result = health.CalculateWithWeights(raw, weights)
		source = "metrics"
	}

	if source == "snapshot" {
		m, err := s.repo.GetHealthScoreMetrics(ctx, req.Params.ClusterName, req.Params.Instance, "")
		if errors.Is(err, repository.ErrNotFound) {
			return serverhttp.GetHealthScore404Response{}, nil
		}

		if err != nil {
			return nil, fmt.Errorf("GetHealthScore | %w", err)
		}

		result = health.CalculateWithWeights(rawFromSnapshot(m), weights)
	}

	categories := make([]serverhttp.HealthScoreCategory, 0, len(result.Categories))
	for _, c := range result.Categories {
		categories = append(categories, serverhttp.HealthScoreCategory{
			Name:    string(c.Name),
			Score:   c.Score,
			Weight:  c.Weight,
			Penalty: c.Penalty,
			Details: c.Details,
		})
	}

	src := source

	return serverhttp.GetHealthScore200JSONResponse{
		Score:          result.Score,
		Categories:     categories,
		HasReplication: result.HasReplication,
		InRecovery:     result.InRecovery,
		Source:         &src,
	}, nil
}

// rawFromSnapshot maps the SQL snapshot onto the score engine's input. Shared by
// the snapshot scoring/recommendation paths and the metrics-mode catalog overlay.
func rawFromSnapshot(m *dto.HealthScoreMetrics) health.RawMetrics {
	return health.RawMetrics{
		InRecovery:                m.InRecovery,
		TotalConnections:          m.TotalConnections,
		ActiveConnections:         m.ActiveConnections,
		IdleInTransaction:         m.IdleInTransaction,
		LongestTransactionSeconds: m.LongestTransactionSeconds,
		MaxConnections:            m.MaxConnections,
		CacheHitRatio:             m.CacheHitRatio,
		TrackIoTimingEnabled:      m.TrackIoTimingEnabled,
		MaxDeadRatio:              m.MaxDeadRatio,
		AvgDeadRatio:              m.AvgDeadRatio,
		TablesHighBloat:           m.TablesHighBloat,
		ReplicaCount:              m.ReplicaCount,
		MaxReplayLagSeconds:       m.MaxReplayLagSeconds,
		MaxLagBytes:               m.MaxLagBytes,
		DisconnectedReplicas:      m.DisconnectedReplicas,
		MaxXidAge:                 m.MaxXidAge,
		MaxVacuumAgeHours:         m.MaxVacuumAgeHours,
		TablesNeverVacuumed:       m.TablesNeverVacuumed,
		AutovacuumEnabled:         m.AutovacuumEnabled,
		TrackCountsEnabled:        m.TrackCountsEnabled,
		TablesWithAutovacuumOff:   m.TablesWithAutovacuumOff,
		MaxRelfrozenxidAge:        m.MaxRelfrozenxidAge,
		HorizonLagXids:            m.HorizonLagXids,
		TimedCheckpoints:          m.TimedCheckpoints,
		RequestedCheckpoints:      m.RequestedCheckpoints,
		ActiveLockWaiters:         m.ActiveLockWaiters,
		LongestLockWaitSeconds:    m.LongestLockWaitSeconds,
		UngrantedLocks:            m.UngrantedLocks,
		DeadlocksTotal:            m.DeadlocksTotal,
		HeavyweightLocksTotal:     m.HeavyweightLocksTotal,
		MaxLocksPerTransaction:    m.MaxLocksPerTransaction,
		HotUpdateRatio:            m.HotUpdateRatio,
		NewpageUpdateRatio:        m.NewpageUpdateRatio,
		StalePlannerStatsTables:   m.StalePlannerStatsTables,
		WalLevel:                  m.WalLevel,
		LogicalSlotsActive:        m.LogicalSlotsActive,
	}
}

// overlayCatalogFacts fills metrics-derived RawMetrics with catalog/GUC facts a
// Prometheus-style datasource cannot express (per-table autovacuum/vacuum state,
// relfrozenxid age, planner-stat drift, GUCs, wal_level, MVCC horizon, lock-pool
// sizing, in-recovery). Time-series-derived fields keep their metrics values;
// only the gaps the collector leaves neutral are written, so catalog-only rules
// (e.g. tables_with_autovacuum_off) keep firing — and the score keeps penalising
// them — even when a datasource is configured.
func overlayCatalogFacts(raw *health.RawMetrics, m *dto.HealthScoreMetrics) {
	raw.InRecovery = m.InRecovery

	// Connections — longest transaction is a catalog/activity fact, not scraped.
	raw.LongestTransactionSeconds = m.LongestTransactionSeconds

	// Performance — track_io_timing GUC.
	raw.TrackIoTimingEnabled = m.TrackIoTimingEnabled

	// Storage — bloat-table count and newpage-update ratio.
	raw.TablesHighBloat = m.TablesHighBloat
	raw.NewpageUpdateRatio = m.NewpageUpdateRatio

	// Maintenance — per-table autovacuum/vacuum state, relfrozenxid, GUCs.
	raw.TablesNeverVacuumed = m.TablesNeverVacuumed
	raw.TablesWithAutovacuumOff = m.TablesWithAutovacuumOff
	raw.MaxRelfrozenxidAge = m.MaxRelfrozenxidAge
	raw.StalePlannerStatsTables = m.StalePlannerStatsTables
	raw.AutovacuumEnabled = m.AutovacuumEnabled
	raw.TrackCountsEnabled = m.TrackCountsEnabled

	// Horizon — oldest backend_xmin pinning VACUUM.
	raw.HorizonLagXids = m.HorizonLagXids

	// WAL / checkpoint configuration.
	raw.WalLevel = m.WalLevel
	raw.LogicalSlotsActive = m.LogicalSlotsActive

	// Locks — heavyweight-pool sizing and longest wait (catalog/activity).
	raw.LongestLockWaitSeconds = m.LongestLockWaitSeconds
	raw.HeavyweightLocksTotal = m.HeavyweightLocksTotal
	raw.MaxLocksPerTransaction = m.MaxLocksPerTransaction
}

// metricsRawWithCatalog returns the instant metrics-backed RawMetrics enriched
// with the catalog overlay, or ok=false when the datasource is disabled,
// unreachable, the target is unmapped, or the catalog snapshot cannot be read
// (caller then falls back to the pure snapshot). The overlay is mandatory: a
// metrics-only RawMetrics carries zero-valued catalog/GUC facts that the scorer
// would misread as "autovacuum off" and similar, so a snapshot read failure
// must sink the metrics result rather than emit a wrong-but-alive score.
func (s *Handlers) metricsRawWithCatalog(ctx context.Context, cluster, instance string) (health.RawMetrics, bool) {
	if !s.metrics.Enabled() {
		return health.RawMetrics{}, false
	}

	raw, err := s.metrics.CurrentRaw(ctx, cluster, instance)
	if err != nil {
		return health.RawMetrics{}, false
	}

	m, sErr := s.repo.GetHealthScoreMetrics(ctx, cluster, instance, "")
	if sErr != nil {
		return health.RawMetrics{}, false
	}

	overlayCatalogFacts(&raw, m)

	return raw, true
}

func (s *Handlers) GetHealthScoreRecommendations(
	ctx context.Context,
	req serverhttp.GetHealthScoreRecommendationsRequestObject,
) (serverhttp.GetHealthScoreRecommendationsResponseObject, error) {
	database := ""
	if req.Params.Database != nil {
		database = *req.Params.Database
	}

	var raw health.RawMetrics

	// Metrics-backed recommendations at instance scope when available (overlaid
	// with catalog/GUC facts so catalog-only rules still fire); the per-DB
	// drill-down (database != "") stays on the SQL snapshot since the collector
	// is instance-level.
	useMetrics := false

	if database == "" {
		if r, ok := s.metricsRawWithCatalog(ctx, req.Params.ClusterName, req.Params.Instance); ok {
			raw = r
			useMetrics = true
		}
	}

	if !useMetrics {
		m, err := s.repo.GetHealthScoreMetrics(ctx, req.Params.ClusterName, req.Params.Instance, database)
		if errors.Is(err, repository.ErrNotFound) {
			return serverhttp.GetHealthScoreRecommendations404Response{}, nil
		}

		if err != nil {
			return nil, fmt.Errorf("GetHealthScoreRecommendations | %w", err)
		}

		raw = rawFromSnapshot(m)
	}

	recs := health.Evaluate(raw, database != "")

	out := make([]serverhttp.HealthScoreRecommendation, 0, len(recs))
	for _, r := range recs {
		var ctxPtr *map[string]any
		if len(r.Context) > 0 {
			c := r.Context
			ctxPtr = &c
		}

		var routePtr *string
		if r.RelatedRoute != "" {
			route := r.RelatedRoute
			routePtr = &route
		}

		out = append(out, serverhttp.HealthScoreRecommendation{
			RuleId:       r.RuleID,
			Category:     string(r.Category),
			Severity:     serverhttp.HealthScoreRecommendationSeverity(r.Severity),
			MetricValue:  r.MetricValue,
			Context:      ctxPtr,
			RelatedRoute: routePtr,
		})
	}

	return serverhttp.GetHealthScoreRecommendations200JSONResponse{
		Recommendations: out,
	}, nil
}
