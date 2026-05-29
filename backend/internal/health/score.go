package health

import "math"

// RawMetrics contains raw metrics collected from PostgreSQL for health score calculation.
type RawMetrics struct {
	// InRecovery is true when the instance is a standby (pg_is_in_recovery()).
	// Standbys cannot run autovacuum/ANALYZE, so the maintenance category is
	// dropped from the score and its rules are hidden from recommendations —
	// same treatment as the replication category on instances without replicas.
	InRecovery bool

	// Connections
	TotalConnections          int
	ActiveConnections         int
	IdleInTransaction         int
	LongestTransactionSeconds float64
	MaxConnections            int

	// Performance
	CacheHitRatio        float64
	TrackIoTimingEnabled bool

	// Storage
	MaxDeadRatio    float64
	AvgDeadRatio    float64
	TablesHighBloat int

	// Replication
	ReplicaCount         int
	MaxReplayLagSeconds  float64
	MaxLagBytes          int64
	DisconnectedReplicas int

	// Maintenance
	MaxXidAge               int64
	MaxVacuumAgeHours       float64
	TablesNeverVacuumed     int
	AutovacuumEnabled       bool
	TrackCountsEnabled      bool
	TablesWithAutovacuumOff int
	MaxRelfrozenxidAge      int64

	// Horizon
	HorizonLagXids int64

	// WAL & Checkpoint
	TimedCheckpoints     int64
	RequestedCheckpoints int64

	// Locks
	ActiveLockWaiters      int
	LongestLockWaitSeconds float64
	UngrantedLocks         int
	DeadlocksTotal         int64
	HeavyweightLocksTotal  int
	MaxLocksPerTransaction int

	// HOT updates / planner stats / WAL level (P3 minor rules)
	HotUpdateRatio          float64
	NewpageUpdateRatio      float64
	StalePlannerStatsTables int
	WalLevel                string
	LogicalSlotsActive      int
}

// CategoryResult holds the penalty calculation result for one category.
type CategoryResult struct {
	Name    string             `json:"name"`
	Score   float64            `json:"score"`
	Weight  float64            `json:"weight"`
	Penalty float64            `json:"penalty"`
	Details map[string]float64 `json:"details"`
}

// Result holds the final health score and per-category breakdown.
type Result struct {
	Score          float64          `json:"score"`
	Categories     []CategoryResult `json:"categories"`
	HasReplication bool             `json:"has_replication"`
	InRecovery     bool             `json:"in_recovery"`
}

// Default category weights, sum to 1.00. Rationale: see plans/health-score-v3-design.md §1.2.
const (
	weightConnections   = 0.15
	weightPerformance   = 0.15
	weightStorage       = 0.10
	weightReplication   = 0.15
	weightMaintenance   = 0.15
	weightHorizon       = 0.10
	weightWalCheckpoint = 0.10
	weightLocks         = 0.10
)

// Calculate computes the health score from raw metrics using default weights.
func Calculate(m RawMetrics) Result {
	return CalculateWithWeights(m, DefaultWeights())
}

// CalculateWithWeights computes the health score using the provided per-category
// weights. Weights are validated, then normalized; invalid input (NaN, Inf,
// negative, or zero sum) falls back to DefaultWeights so a downstream caller
// passing garbage cannot corrupt the score.
//
// Categories with no signal on this instance are dropped and their weight is
// redistributed proportionally across the remaining ones:
//   - replication: dropped when there are no replicas
//   - maintenance: dropped when pg_is_in_recovery() (standby) — autovacuum/ANALYZE
//     cannot run on a standby, so the metrics would always look stale.
func CalculateWithWeights(m RawMetrics, w Weights) Result {
	hasReplication := m.ReplicaCount > 0

	if err := w.Validate(); err != nil {
		w = DefaultWeights()
	}

	w = w.Normalize()

	categories := []CategoryResult{
		penaltyConnections(m),
		penaltyPerformance(m),
		penaltyStorage(m),
		penaltyReplication(m),
		penaltyMaintenance(m),
		penaltyHorizon(m),
		penaltyWalCheckpoint(m),
		penaltyLocks(m),
	}

	for i := range categories {
		categories[i].Weight = w.byCategory(categories[i].Name)
	}

	var dropped []string
	if !hasReplication {
		dropped = append(dropped, "replication")
	}

	if m.InRecovery {
		dropped = append(dropped, "maintenance")
	}

	if len(dropped) > 0 {
		redistributeWeights(categories, dropped)
	}

	totalPenalty := 0.0
	for i := range categories {
		totalPenalty += categories[i].Penalty * categories[i].Weight
		categories[i].Score = 100 - categories[i].Penalty
	}

	score := math.Max(0, math.Min(100, 100-totalPenalty))

	return Result{
		Score:          math.Round(score*10) / 10,
		Categories:     categories,
		HasReplication: hasReplication,
		InRecovery:     m.InRecovery,
	}
}

// redistributeWeights zeroes the weight (and penalty) of categories whose names
// appear in `drop`, and adds their combined weight to the remaining categories
// proportionally to their current weight. No-op when the dropped set is empty
// or when there is nothing left to receive the redistributed weight.
func redistributeWeights(categories []CategoryResult, drop []string) {
	dropped := make(map[string]bool, len(drop))
	for _, n := range drop {
		dropped[n] = true
	}

	var droppedSum, otherSum float64

	for _, c := range categories {
		if dropped[c.Name] {
			droppedSum += c.Weight
		} else {
			otherSum += c.Weight
		}
	}

	if droppedSum == 0 || otherSum == 0 {
		return
	}

	for i := range categories {
		if dropped[categories[i].Name] {
			categories[i].Weight = 0
			categories[i].Penalty = 0
		} else {
			categories[i].Weight += droppedSum * (categories[i].Weight / otherSum)
		}
	}
}

func penaltyConnections(m RawMetrics) CategoryResult {
	if m.MaxConnections == 0 {
		return CategoryResult{Name: "connections", Weight: weightConnections, Details: map[string]float64{}}
	}

	ratio := float64(m.TotalConnections) / float64(m.MaxConnections)
	penalty := 0.0

	switch {
	case ratio > 0.95:
		penalty = 70 + (ratio-0.95)/0.05*30
	case ratio > 0.80:
		penalty = 30 + (ratio-0.80)/0.15*40
	case ratio > 0.60:
		penalty = (ratio - 0.60) / 0.20 * 30
	}

	// idle in transaction penalty
	idlePenalty := math.Min(float64(m.IdleInTransaction)*5, 30)
	penalty += idlePenalty

	// longest transaction penalty
	if m.LongestTransactionSeconds > 300 {
		txPenalty := math.Min((m.LongestTransactionSeconds-300)/60*5, 20)
		penalty += txPenalty
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "connections",
		Weight:  weightConnections,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"connection_ratio":              math.Round(ratio*1000) / 1000,
			"idle_in_transaction":           float64(m.IdleInTransaction),
			"longest_transaction_seconds":   m.LongestTransactionSeconds,
		},
	}
}

func penaltyPerformance(m RawMetrics) CategoryResult {
	penalty := 0.0

	// Relaxed gradient (was 99/95/90): aligned with low_cache_hit_ratio rule
	// thresholds — OLAP workloads with cold cache shouldn't be penalised
	// indiscriminately.
	switch {
	case m.CacheHitRatio >= 95:
		penalty = 0
	case m.CacheHitRatio >= 90:
		penalty = (95 - m.CacheHitRatio) / 5 * 20
	case m.CacheHitRatio >= 85:
		penalty = 20 + (90-m.CacheHitRatio)/5*30
	default:
		penalty = 50 + (85-m.CacheHitRatio)/10*50
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "performance",
		Weight:  weightPerformance,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"cache_hit_ratio": m.CacheHitRatio,
		},
	}
}

func penaltyStorage(m RawMetrics) CategoryResult {
	penalty := 0.0

	// Aligned with high_max_dead_ratio thresholds (30/20/10) and
	// high_avg_dead_ratio (25/15/5).
	switch {
	case m.MaxDeadRatio > 30:
		penalty = 40
	case m.MaxDeadRatio > 20:
		penalty = 20 + (m.MaxDeadRatio-20)/10*20
	default:
		penalty = m.MaxDeadRatio / 20 * 20
	}

	if m.AvgDeadRatio > 15 {
		penalty += math.Min((m.AvgDeadRatio-15)*2, 30)
	}

	if m.TablesHighBloat > 5 {
		penalty += math.Min(float64(m.TablesHighBloat)*3, 30)
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "storage",
		Weight:  weightStorage,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"max_dead_ratio":    m.MaxDeadRatio,
			"avg_dead_ratio":    m.AvgDeadRatio,
			"tables_high_bloat": float64(m.TablesHighBloat),
		},
	}
}

func penaltyReplication(m RawMetrics) CategoryResult {
	if m.ReplicaCount == 0 {
		return CategoryResult{
			Name:    "replication",
			Weight:  weightReplication,
			Penalty: 0,
			Details: map[string]float64{
				"replica_count": 0,
			},
		}
	}

	penalty := 0.0

	switch {
	case m.MaxReplayLagSeconds > 30:
		penalty = 50
	case m.MaxReplayLagSeconds > 5:
		penalty = 10 + (m.MaxReplayLagSeconds-5)/25*40
	case m.MaxReplayLagSeconds > 1:
		penalty = (m.MaxReplayLagSeconds - 1) / 4 * 10
	}

	lagMB := float64(m.MaxLagBytes) / (1024 * 1024)
	if lagMB > 100 {
		penalty += math.Min(lagMB/10, 30)
	}

	if m.DisconnectedReplicas > 0 {
		penalty += math.Min(float64(m.DisconnectedReplicas)*30, 60)
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "replication",
		Weight:  weightReplication,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"replica_count":          float64(m.ReplicaCount),
			"max_replay_lag_seconds": m.MaxReplayLagSeconds,
			"max_lag_bytes":          float64(m.MaxLagBytes),
			"disconnected_replicas":  float64(m.DisconnectedReplicas),
		},
	}
}

func penaltyMaintenance(m RawMetrics) CategoryResult {
	penalty := 0.0

	switch {
	case m.MaxXidAge > 1_500_000_000:
		penalty = 60
	case m.MaxXidAge > 1_000_000_000:
		penalty = 20 + float64(m.MaxXidAge-1_000_000_000)/500_000_000*40
	case m.MaxXidAge > 500_000_000:
		penalty = 5 + float64(m.MaxXidAge-500_000_000)/500_000_000*15
	}

	// Aligned with stale_vacuum thresholds (7/21/60 days = 168/504/1440 h).
	// Middle-tier addition capped at 30 so the function stays monotonic at
	// the 1440 h boundary — without the cap, 1440 h = 15 + 19.5 = 34.5 would
	// dip back to 30 right after the boundary.
	if m.MaxVacuumAgeHours > 1440 { // 60 days
		penalty += 30
	} else if m.MaxVacuumAgeHours > 504 { // 21 days
		penalty += math.Min(15+(m.MaxVacuumAgeHours-504)/24*0.5, 30)
	} else if m.MaxVacuumAgeHours > 168 { // 7 days
		penalty += (m.MaxVacuumAgeHours - 168) / 24 * 1
	}

	if m.TablesNeverVacuumed > 0 {
		penalty += math.Min(float64(m.TablesNeverVacuumed)*5, 20)
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "maintenance",
		Weight:  weightMaintenance,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"max_xid_age":           float64(m.MaxXidAge),
			"max_vacuum_age_hours":  m.MaxVacuumAgeHours,
			"tables_never_vacuumed": float64(m.TablesNeverVacuumed),
		},
	}
}

// penaltyHorizon scores the DB transaction horizon lag.
// Thresholds mirror the horizon_lag_xids rule: 1M / 10M / 100M xids.
func penaltyHorizon(m RawMetrics) CategoryResult {
	lag := float64(m.HorizonLagXids)
	penalty := 0.0

	switch {
	case lag > 100_000_000:
		penalty = 70 + math.Min((lag-100_000_000)/100_000_000*30, 30)
	case lag > 10_000_000:
		penalty = 30 + (lag-10_000_000)/90_000_000*40
	case lag > 1_000_000:
		penalty = (lag - 1_000_000) / 9_000_000 * 30
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "horizon",
		Weight:  weightHorizon,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"horizon_lag_xids": lag,
		},
	}
}

// penaltyWalCheckpoint scores the share of unplanned (requested) checkpoints.
// Needs a minimum sample of 10 checkpoints to avoid false positives on fresh servers.
func penaltyWalCheckpoint(m RawMetrics) CategoryResult {
	total := m.TimedCheckpoints + m.RequestedCheckpoints
	ratio := 0.0
	penalty := 0.0

	if total >= 10 {
		ratio = float64(m.RequestedCheckpoints) / float64(total)

		// Aligned with requested_checkpoint_ratio rule (0.20 / 0.10 / 0.05).
		switch {
		case ratio > 0.20:
			penalty = 70 + math.Min((ratio-0.20)/0.30*30, 30)
		case ratio > 0.10:
			penalty = 30 + (ratio-0.10)/0.10*40
		case ratio > 0.05:
			penalty = (ratio - 0.05) / 0.05 * 30
		}
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "wal_checkpoint",
		Weight:  weightWalCheckpoint,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"requested_checkpoint_ratio": math.Round(ratio*10000) / 10000,
			"timed_checkpoints":          float64(m.TimedCheckpoints),
			"requested_checkpoints":      float64(m.RequestedCheckpoints),
		},
	}
}

// penaltyLocks scores heavy-lock contention from a snapshot of pg_stat_activity
// (`wait_event_type = 'Lock'`) and pg_locks (ungranted, total vs capacity), plus
// cumulative deadlocks from pg_stat_database. Thresholds mirror the locks rules.
func penaltyLocks(m RawMetrics) CategoryResult {
	penalty := 0.0

	// Active lock waiters — aligned with rule thresholds 1 / 3 / 10
	// (LOW / MED / HIGH) → roughly 10 / 30 / 60 penalty points.
	switch {
	case m.ActiveLockWaiters >= 10:
		penalty += 60
	case m.ActiveLockWaiters >= 3:
		penalty += 30
	case m.ActiveLockWaiters >= 1:
		penalty += 10
	}

	// Longest lock wait seconds: 10 / 30 / 60 → up to 40 penalty points.
	switch {
	case m.LongestLockWaitSeconds > 60:
		penalty += 40
	case m.LongestLockWaitSeconds > 30:
		penalty += 20
	case m.LongestLockWaitSeconds > 10:
		penalty += 5
	}

	// Ungranted locks queue length — aligned with rule thresholds 2 / 5 / 15
	// (LOW / MED / HIGH) → up to 30 points.
	switch {
	case m.UngrantedLocks >= 15:
		penalty += 30
	case m.UngrantedLocks >= 5:
		penalty += 15
	case m.UngrantedLocks >= 2:
		penalty += 5
	}

	// Pool saturation: ratio of pg_locks rows to capacity.
	saturation := 0.0

	if m.MaxLocksPerTransaction > 0 && m.MaxConnections > 0 {
		capacity := float64(m.MaxLocksPerTransaction) * float64(m.MaxConnections)
		if capacity > 0 {
			saturation = float64(m.HeavyweightLocksTotal) / capacity
		}

		switch {
		case saturation > 0.8:
			penalty += 30
		case saturation > 0.6:
			penalty += 15
		case saturation > 0.5:
			penalty += 5
		}
	}

	// Deadlocks: absolute count since stats_reset. Caps at 20 points so a base
	// cluster with 1-2 historical deadlocks doesn't dominate the locks score.
	switch {
	case m.DeadlocksTotal >= 100:
		penalty += 20
	case m.DeadlocksTotal >= 10:
		penalty += 10
	case m.DeadlocksTotal >= 1:
		penalty += 3
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "locks",
		Weight:  weightLocks,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"active_lock_waiters":       float64(m.ActiveLockWaiters),
			"longest_lock_wait_seconds": m.LongestLockWaitSeconds,
			"ungranted_locks":           float64(m.UngrantedLocks),
			"deadlocks_total":           float64(m.DeadlocksTotal),
			"heavyweight_locks":         float64(m.HeavyweightLocksTotal),
			"lock_pool_saturation":      math.Round(saturation*10000) / 10000,
		},
	}
}
