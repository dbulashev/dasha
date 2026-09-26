package healthscore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

type Source string

const (
	SourceMetrics  Source = "metrics"
	SourceSnapshot Source = "snapshot"
	SourceNone     Source = "none"
)

// ClusterContext holds the per-cluster inputs of a score.
type ClusterContext struct {
	Weights         health.Weights
	WalLevelManaged bool
}

type InstanceScore struct {
	Result          health.Result
	Source          Source
	MetricsDegraded bool
}

// MetricsSource is the part of *metrics.Service the scorer reads.
type MetricsSource interface {
	Enabled() bool
	CurrentRaw(ctx context.Context, cluster, instance string) (health.RawMetrics, metrics.Signals, error)
}

type WeightsStore interface {
	GetHealthWeights(ctx context.Context, clusterName string) (*storage.HealthWeightsRecord, error)
}

type Scorer struct {
	repo    repository.Repository
	metrics MetricsSource
	weights WeightsStore
	cfg     *config.Config

	mu      sync.Mutex
	flights map[inputsKey]*inputsFlight
}

// NewScorer builds the scorer; a nil storage or metrics service disables the
// weight overrides or the metrics path respectively.
func NewScorer(cfg *config.Config, repo repository.Repository, st *storage.Storage, ms *metrics.Service) *Scorer {
	s := newScorer(cfg, repo, ms)
	if st != nil {
		s.weights = st
	}

	return s
}

func newScorer(cfg *config.Config, repo repository.Repository, ms MetricsSource) *Scorer {
	return &Scorer{repo: repo, metrics: ms, cfg: cfg, flights: make(map[inputsKey]*inputsFlight)}
}

// Score computes the card score. pre, when non-nil, replaces the datasource
// read for this instance; nil means read it now. A missing instance comes back
// as repository.ErrNotFound.
func (s *Scorer) Score(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult) (InstanceScore, error) {
	weights, err := s.Weights(ctx, t.Cluster)
	if err != nil {
		return InstanceScore{}, fmt.Errorf("Score | Weights | %w", err)
	}

	return s.ScoreWith(ctx, t, pre, ClusterContext{Weights: weights, WalLevelManaged: s.walLevelManaged(ctx, t.Cluster)})
}

// ScoreWith is Score with the per-cluster inputs already resolved.
func (s *Scorer) ScoreWith(ctx context.Context, t metrics.TargetRef, pre *metrics.RawResult, cc ClusterContext) (InstanceScore, error) {
	var in inputs
	if pre != nil {
		in = s.read(ctx, t, "", pre)
	} else {
		in = s.coalescedInputs(ctx, t, "")
	}

	raw, src, matched, err := s.compose(in, cc.WalLevelManaged)
	if err != nil {
		return InstanceScore{}, fmt.Errorf("Score | %w", err)
	}

	return InstanceScore{
		Result: health.CalculateWithWeights(raw, cc.Weights),
		Source: src,
		// Resolved but no series matched any selector: the score is built from
		// absent signals and looks green.
		MetricsDegraded: src == SourceMetrics && matched == 0,
	}, nil
}

// Recommendations evaluates the rules at instance scope (database == "") or
// for one database's drill-down, which always reads the SQL snapshot since the
// datasource is instance-level.
func (s *Scorer) Recommendations(ctx context.Context, t metrics.TargetRef, database string) ([]health.Recommendation, error) {
	raw, _, _, err := s.compose(s.coalescedInputs(ctx, t, database), s.walLevelManaged(ctx, t.Cluster))
	if err != nil {
		return nil, fmt.Errorf("Recommendations | %w", err)
	}

	return health.Evaluate(raw, database != ""), nil
}

// compose builds the engine input from one read. The metrics raw needs the
// snapshot overlay: without it zero-valued catalog facts read as "autovacuum
// off" and similar, so a failed snapshot sinks the metrics path too. matched is
// the number of signals the datasource carried.
func (s *Scorer) compose(in inputs, walLevelManaged bool) (raw health.RawMetrics, src Source, matched int, err error) {
	if in.snapErr != nil {
		return health.RawMetrics{}, SourceNone, 0, in.snapErr
	}

	if r := in.metrics; r != nil && r.Err == nil {
		raw = r.Raw
		raw.SnapshotBackedRules = maps.Clone(raw.SnapshotBackedRules)
		raw.MetricsInstanceWide = true
		overlayCatalogFacts(&raw, in.snap)
		overlaySignalGaps(&raw, in.snap, r.Signals)
		src, matched = SourceMetrics, len(r.Signals.Have)
	} else {
		raw, src = rawFromSnapshot(in.snap), SourceSnapshot
	}

	raw.WalLevelManaged = walLevelManaged
	raw.SequenceThresholds = s.cfg.SchemaLint.SequenceThresholds

	// Standbys never carry the headroom; a failed read leaves "no signal".
	if !raw.InRecovery && raw.SequenceExhaustionMax == 0 && in.seqErr == nil && in.seqKnown {
		raw.SequenceExhaustionMax = in.seqWorst
	}

	return raw, src, matched, nil
}

// Weights returns the per-cluster override, or DefaultWeights when there is no
// storage or no override. An error means the storage read itself failed; the
// returned weights are then DefaultWeights.
func (s *Scorer) Weights(ctx context.Context, clusterName string) (health.Weights, error) {
	if s.weights == nil {
		return health.DefaultWeights(), nil
	}

	rec, err := s.weights.GetHealthWeights(ctx, clusterName)
	if err != nil {
		return health.DefaultWeights(), err
	}

	if rec == nil {
		return health.DefaultWeights(), nil
	}

	return rec.Weights, nil
}

// IsNotFound reports whether err means the instance is not configured.
func IsNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}

func (s *Scorer) walLevelManaged(ctx context.Context, clusterName string) bool {
	clusters, err := s.repo.Clusters(ctx)
	if err != nil {
		return false
	}

	for _, c := range clusters {
		if c.Name.String() == clusterName {
			return walLevelFixed(c)
		}
	}

	return false
}

// walLevelFixed reports whether the cluster's provider fixes wal_level (Yandex
// MDB forces logical), so the wasted-overhead rule must not fire.
func walLevelFixed(c dto.ClusterInfo) bool {
	return c.Source == config.SourceYandexMDB
}

// rawFromSnapshot maps the SQL snapshot onto the score engine's input.
func rawFromSnapshot(m *dto.HealthScoreMetrics) health.RawMetrics {
	return health.RawMetrics{
		InRecovery:                 m.InRecovery,
		Database:                   m.Database,
		TotalConnections:           m.TotalConnections,
		ActiveConnections:          m.ActiveConnections,
		IdleInTransaction:          m.IdleInTransaction,
		IdleInTransactionDatabase:  m.IdleInTransactionDatabase,
		LongestTransactionSeconds:  m.LongestTransactionSeconds,
		LongestTransactionDatabase: m.LongestTransactionDatabase,
		MaxConnections:             m.MaxConnections,
		CacheHitRatio:              m.CacheHitRatio,
		CacheSampleBlocks:          m.CacheSampleBlocks,
		TrackIoTimingEnabled:       m.TrackIoTimingEnabled,
		MaxDeadRatio:               m.MaxDeadRatio,
		AvgDeadRatio:               m.AvgDeadRatio,
		TablesHighBloat:            m.TablesHighBloat,
		ReplicaCount:               m.ReplicaCount,
		MaxReplayLagSeconds:        m.MaxReplayLagSeconds,
		MaxLagBytes:                m.MaxLagBytes,
		DisconnectedReplicas:       m.DisconnectedReplicas,
		MaxXidAge:                  m.MaxXidAge,
		VacuumBacklogTables:        m.VacuumBacklogTables,
		MaxOverdueVacuumAgeHours:   m.MaxOverdueVacuumAgeHours,
		TablesNeverVacuumed:        m.TablesNeverVacuumed,
		AutovacuumEnabled:          m.AutovacuumEnabled,
		TrackCountsEnabled:         m.TrackCountsEnabled,
		TablesWithAutovacuumOff:    m.TablesWithAutovacuumOff,
		MaxRelfrozenxidAge:         m.MaxRelfrozenxidAge,
		HorizonLagXids:             m.HorizonLagXids,
		HorizonDatabase:            m.HorizonDatabase,
		TimedCheckpoints:           m.TimedCheckpoints,
		RequestedCheckpoints:       m.RequestedCheckpoints,
		ActiveLockWaiters:          m.ActiveLockWaiters,
		LongestLockWaitSeconds:     m.LongestLockWaitSeconds,
		UngrantedLocks:             m.UngrantedLocks,
		DeadlocksTotal:             m.DeadlocksTotal,
		HeavyweightLocksTotal:      m.HeavyweightLocksTotal,
		MaxLocksPerTransaction:     m.MaxLocksPerTransaction,
		HotUpdateRatio:             m.HotUpdateRatio,
		NewpageUpdateRatio:         m.NewpageUpdateRatio,
		StalePlannerStatsTables:    m.StalePlannerStatsTables,
		WalLevel:                   m.WalLevel,
		LogicalSlotsActive:         m.LogicalSlotsActive,
	}
}
