package healthscore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/metrics"
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
	// Cold is the cold-table set of the database the snapshot was read from.
	Cold           hotobjects.ColdSet
	ColdWindowDays int
}

// MetricsSource is the part of *metrics.Service the scorer reads.
type MetricsSource interface {
	Enabled() bool
	CurrentRaw(ctx context.Context, cluster, instance string) (health.RawMetrics, metrics.Signals, error)
}

// Repo is the part of the repository the scorer reads. A missing instance
// comes back as dto.ErrNotFound.
type Repo interface {
	Clusters(ctx context.Context) ([]dto.ClusterInfo, error)
	GetHealthScoreMetrics(ctx context.Context, cluster, instance, database string, cold map[string]dto.ColdArgs) (*dto.HealthScoreMetrics, error)
	GetSequenceHeadroom(ctx context.Context, cluster, instance, database string) (float64, bool, error)
	GetHealthScoreColdTables(ctx context.Context, cluster, instance, database string, cold dto.ColdArgs, limit int) ([]dto.HealthScoreColdTable, int, error)
}

type WeightsRecord struct {
	Weights   health.Weights
	UpdatedAt time.Time
	UpdatedBy string
}

// WeightsStore keeps per-cluster overrides. GetHealthWeights returns nil, nil
// when there is none; UpsertHealthWeights expects validated, normalized weights
// and returns the record as stored.
type WeightsStore interface {
	GetHealthWeights(ctx context.Context, cluster string) (*WeightsRecord, error)
	UpsertHealthWeights(ctx context.Context, cluster string, w health.Weights, user string) (WeightsRecord, error)
	DeleteHealthWeights(ctx context.Context, cluster string) error
}

// Store is what the scorer keeps outside the monitored clusters.
type Store interface {
	WeightsStore
	ColdStore
}

type WeightsSource string

const (
	WeightsDefault  WeightsSource = "default"
	WeightsOverride WeightsSource = "override"
)

type WeightsView struct {
	Weights   health.Weights
	Source    WeightsSource
	UpdatedAt *time.Time
	UpdatedBy *string
}

var (
	ErrInvalidWeights = errors.New("healthscore: invalid weights")
	ErrNoWeightsStore = errors.New("healthscore: storage is not configured")
)

type Scorer struct {
	repo    Repo
	metrics MetricsSource
	weights WeightsStore
	cold    *coldCache
	cfg     *config.Config

	mu      sync.Mutex
	flights map[inputsKey]*inputsFlight
}

// NewScorer builds the scorer; a nil store or metrics service disables the
// weight overrides and cold sets or the metrics path respectively.
func NewScorer(cfg *config.Config, repo Repo, st Store, ms *metrics.Service) *Scorer {
	s := newScorer(cfg, repo, ms)
	if st != nil {
		s.weights = st
		s.cold = newColdCache(st)
	}

	return s
}

func newScorer(cfg *config.Config, repo Repo, ms MetricsSource) *Scorer {
	return &Scorer{repo: repo, metrics: ms, cfg: cfg, flights: make(map[inputsKey]*inputsFlight)}
}

// Score computes the card score. pre, when non-nil, replaces the datasource
// read for this instance; nil means read it now. A missing instance comes back
// as dto.ErrNotFound.
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
		Cold:            in.cold.For(raw.Database),
		ColdWindowDays:  in.cold.WindowDays,
	}, nil
}

// Recommendations evaluates the rules at instance scope (database == "") or
// for one database's drill-down, which always reads the SQL snapshot since the
// datasource is instance-level.
func (s *Scorer) Recommendations(ctx context.Context, t metrics.TargetRef, database string) ([]health.Recommendation, error) {
	in := s.coalescedInputs(ctx, t, database)

	raw, _, _, err := s.compose(in, s.walLevelManaged(ctx, t.Cluster))
	if err != nil {
		return nil, fmt.Errorf("Recommendations | %w", err)
	}

	if !raw.InRecovery {
		raw.Cold = s.coldFacts(ctx, t, raw.Database, in.cold)
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

// Weights returns the weights a score is computed with.
func (s *Scorer) Weights(ctx context.Context, cluster string) (health.Weights, error) {
	v, err := s.WeightsView(ctx, cluster)

	return v.Weights, err
}

// WeightsView returns the per-cluster override, or DefaultWeights when there is
// no storage or no override. An error means the storage read itself failed; the
// returned weights are then DefaultWeights.
func (s *Scorer) WeightsView(ctx context.Context, cluster string) (WeightsView, error) {
	def := WeightsView{Weights: health.DefaultWeights(), Source: WeightsDefault} //nolint:exhaustruct
	if s.weights == nil {
		return def, nil
	}

	rec, err := s.weights.GetHealthWeights(ctx, cluster)
	if err != nil {
		return def, err
	}

	if rec == nil {
		return def, nil
	}

	return overrideView(*rec), nil
}

func overrideView(rec WeightsRecord) WeightsView {
	v := WeightsView{Weights: rec.Weights, Source: WeightsOverride, UpdatedAt: &rec.UpdatedAt} //nolint:exhaustruct
	if rec.UpdatedBy != "" {
		v.UpdatedBy = &rec.UpdatedBy
	}

	return v
}

// SetWeights stores a normalized override. Invalid weights are rejected with
// ErrInvalidWeights even when there is no storage; valid ones without storage
// with ErrNoWeightsStore.
func (s *Scorer) SetWeights(ctx context.Context, cluster string, w health.Weights, user string) (WeightsView, error) {
	if err := w.Validate(); err != nil {
		return WeightsView{}, fmt.Errorf("%w: %w", ErrInvalidWeights, err)
	}

	if s.weights == nil {
		return WeightsView{}, ErrNoWeightsStore
	}

	rec, err := s.weights.UpsertHealthWeights(ctx, cluster, w.Normalize(), user)
	if err != nil {
		return WeightsView{}, err
	}

	return overrideView(rec), nil
}

// ResetWeights drops the override; without storage there is none to drop.
func (s *Scorer) ResetWeights(ctx context.Context, cluster string) (WeightsView, error) {
	if s.weights != nil {
		if err := s.weights.DeleteHealthWeights(ctx, cluster); err != nil {
			return WeightsView{}, err
		}
	}

	return s.WeightsView(ctx, cluster)
}

// IsNotFound reports whether err means the instance is not configured.
func IsNotFound(err error) bool {
	return errors.Is(err, dto.ErrNotFound)
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
		ColdMaxRelfrozenxidAge:     m.ColdMaxRelfrozenxidAge,
		ColdFreezeRatio:            m.ColdFreezeRatio,
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
