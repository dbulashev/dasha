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

// Transaction-ID age thresholds, tied to PostgreSQL's freeze machinery. Shared
// by the maintenance penalty curve, the xid wraparound rules, and the critical
// score ceiling so all three stay calibrated to the same mechanics:
//   - xidFreezeTableAge  vacuum_freeze_table_age default: regular VACUUM begins
//     aggressively freezing whole tables.
//   - xidFreezeMaxAge    autovacuum_freeze_max_age default: anti-wraparound
//     (emergency) autovacuum is forced, even on tables it would otherwise skip.
//   - xidFailsafeAge     vacuum_failsafe_age default: VACUUM enters failsafe and
//     skips index cleanup to race the wraparound — PostgreSQL's own "emergency".
//   - xidShutdownAge     ~ the point where the server refuses new XIDs (2^31 less
//     a safety margin) and must be brought down for an offline VACUUM.
const (
	xidFreezeTableAge int64 = 150_000_000
	xidFreezeMaxAge   int64 = 200_000_000
	xidFailsafeAge    int64 = 1_600_000_000
	xidShutdownAge    int64 = 2_100_000_000
)

// criticalScoreCeiling caps the overall score when a catastrophic, availability-
// or data-integrity-threatening condition is present. 30 sits in the frontend's
// red band (< 40), so a single silent-killer issue forces the instance into the
// red regardless of how healthy the other categories look. A plain weighted
// average would otherwise dilute it away — e.g. imminent wraparound moves the
// maintenance category by at most its weight (~15 points), leaving the headline
// number green next to a HIGH wraparound recommendation. The ceiling keeps the
// number consistent with the rules engine, which surfaces the same conditions.
const criticalScoreCeiling = 30.0

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

	// Fold in instance-only signals (enabled GUCs, HOT ratio, wal_level) so the
	// score moves on every condition the rules engine reports — applied before
	// the weight drop so a standby's maintenance bumps still get zeroed out.
	applyInstanceAdjustments(categories, m)

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

	// Catastrophic conditions clamp the aggregate into the red regardless of how
	// the weighted average came out — see criticalScoreCeiling.
	if ceiling := criticalCeiling(m); ceiling < score {
		score = ceiling
	}

	return Result{
		Score:          math.Round(score*10) / 10,
		Categories:     categories,
		HasReplication: hasReplication,
		InRecovery:     m.InRecovery,
	}
}

// criticalCeiling returns the maximum score allowed in the presence of a
// catastrophic condition, or 100 when none applies.
//
// Maintenance-class conditions are gated on !InRecovery: a standby cannot run
// autovacuum/ANALYZE and inherits its frozen-xid horizon from the primary, so
// any action belongs there — mirroring the maintenance-category drop in
// CalculateWithWeights.
func criticalCeiling(m RawMetrics) float64 {
	if m.InRecovery {
		return 100
	}

	// Imminent transaction-ID wraparound. Use the larger of the database
	// datfrozenxid age and the per-table relfrozenxid age; the latter also
	// covers a database whose datfrozenxid metric came back empty.
	xidAge := m.MaxXidAge
	if m.MaxRelfrozenxidAge > xidAge {
		xidAge = m.MaxRelfrozenxidAge
	}

	if c := criticalXidCeiling(xidAge); c < 100 {
		return c
	}

	// Autovacuum globally off — dead tuples and frozen-xid age grow unbounded.
	// track_counts off — autovacuum is blind and never triggers, even if enabled.
	if !m.AutovacuumEnabled || !m.TrackCountsEnabled {
		return criticalScoreCeiling
	}

	return 100
}

// criticalXidCeiling clamps the score to the red band once the transaction-ID
// age reaches the wraparound failsafe zone, where PostgreSQL itself enters
// emergency VACUUM. Returns 100 (no clamp) below that. Shared with the per-DB
// path, where the instance-level GUC conditions do not apply.
func criticalXidCeiling(xidAge int64) float64 {
	if xidAge >= xidFailsafeAge {
		return criticalScoreCeiling
	}

	return 100
}

// applyInstanceAdjustments folds instance-only signals into the per-category
// penalties so the aggregate moves on every condition the rules engine reports
// (low_cache_hit_ratio's siblings: track_io_timing, HOT ratio, wal_level, and
// the autovacuum/track_counts GUCs). These live here, not in the shared penalty
// functions, because those are reused for per-DB scoring with a zero-value
// RawMetrics where a false bool or a 0.0 HOT ratio would read as "broken".
// Magnitudes mirror the corresponding rule's severity (LOW ≈ 5, HIGH ≈ 80+).
func applyInstanceAdjustments(categories []CategoryResult, m RawMetrics) {
	// Maintenance: autovacuum / track_counts off are silent killers — the same
	// HIGH rules that drive criticalCeiling. Saturate the category so the
	// breakdown turns red to match the floored aggregate.
	if !m.AutovacuumEnabled {
		addPenalty(categories, "maintenance", 100)
	}

	if !m.TrackCountsEnabled {
		addPenalty(categories, "maintenance", 100)
	}

	// Surface the GUC state in the maintenance tooltip, so a saturated (red) bar
	// reads as "autovacuum off" rather than the benign vacuum/xid numbers next to it.
	setDetail(categories, "maintenance", "autovacuum_enabled", b2f(m.AutovacuumEnabled))
	setDetail(categories, "maintenance", "track_counts_enabled", b2f(m.TrackCountsEnabled))

	// Performance: track_io_timing off (LOW) — recommended on, negligible cost.
	if !m.TrackIoTimingEnabled {
		addPenalty(categories, "performance", 5)
	}

	setDetail(categories, "performance", "track_io_timing_enabled", b2f(m.TrackIoTimingEnabled))

	// Storage: low HOT-update ratio (inverted — lower is worse). The SQL returns
	// 1.0 when there are too few updates to judge, so quiet databases score 0.
	switch {
	case m.HotUpdateRatio < 0.50:
		addPenalty(categories, "storage", 30)
	case m.HotUpdateRatio < 0.65:
		addPenalty(categories, "storage", 15)
	case m.HotUpdateRatio < 0.80:
		addPenalty(categories, "storage", 5)
	}

	setDetail(categories, "storage", "hot_update_ratio", m.HotUpdateRatio)

	// WAL & checkpoint: wal_level misconfiguration. wal_level is a string and the
	// Details map is float64-only, so we flag the offending condition by presence
	// (value 1) and let the frontend render it to text — this keeps a penalised
	// wal_checkpoint bar self-explanatory in the tooltip without a schema change.
	if m.WalLevel == "minimal" && m.ReplicaCount > 0 {
		addPenalty(categories, "wal_checkpoint", 80) // HIGH: replicas can't stream
		setDetail(categories, "wal_checkpoint", "wal_level_minimal_with_replicas", 1)
	}

	if m.WalLevel == "logical" && m.LogicalSlotsActive == 0 {
		addPenalty(categories, "wal_checkpoint", 5) // LOW: wasted WAL overhead
		setDetail(categories, "wal_checkpoint", "wal_level_logical_without_slots", 1)
	}

	// Round once, after every addition, to keep penalties at one decimal place
	// without per-call rounding drift.
	for i := range categories {
		categories[i].Penalty = math.Round(categories[i].Penalty*10) / 10
	}
}

// addPenalty adds delta to the named category's penalty, capping at 100.
// Rounding is deferred to a single pass at the end of applyInstanceAdjustments
// so repeated additions cannot accumulate per-call rounding error.
func addPenalty(categories []CategoryResult, name string, delta float64) {
	for i := range categories {
		if categories[i].Name == name {
			categories[i].Penalty = math.Min(categories[i].Penalty+delta, 100)
			return
		}
	}
}

// b2f maps a bool to 1.0 / 0.0 for the float64 Details map.
func b2f(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

// setDetail records an extra metric in the named category's Details tooltip.
func setDetail(categories []CategoryResult, name, key string, value float64) {
	for i := range categories {
		if categories[i].Name == name {
			if categories[i].Details == nil {
				categories[i].Details = map[string]float64{}
			}

			categories[i].Details[key] = value

			return
		}
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

	if droppedSum == 0 {
		return
	}

	// Always zero out dropped categories so they cannot contribute to the
	// score, even when otherSum == 0 (pathological case: every non-dropped
	// category has zero weight). Without this step, an early return would
	// leave the dropped weights intact and they'd still drive the aggregate.
	for i := range categories {
		if dropped[categories[i].Name] {
			categories[i].Weight = 0
			categories[i].Penalty = 0
		}
	}

	if otherSum == 0 {
		return
	}

	for i := range categories {
		if !dropped[categories[i].Name] {
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
			"connection_ratio":            math.Round(ratio*1000) / 1000,
			"idle_in_transaction":         float64(m.IdleInTransaction),
			"longest_transaction_seconds": m.LongestTransactionSeconds,
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

	// HOT-chain ruptures (high_newpage_update_ratio rule): a new-page UPDATE
	// could not stay on the same page and had to touch every index. 0 on PG < 16
	// (column absent → stays silent) and under per-DB scoring (not collected).
	switch {
	case m.NewpageUpdateRatio > 0.20:
		penalty += 20
	case m.NewpageUpdateRatio > 0.10:
		penalty += 10
	case m.NewpageUpdateRatio > 0.05:
		penalty += 5
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "storage",
		Weight:  weightStorage,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"max_dead_ratio":       m.MaxDeadRatio,
			"avg_dead_ratio":       m.AvgDeadRatio,
			"tables_high_bloat":    float64(m.TablesHighBloat),
			"newpage_update_ratio": m.NewpageUpdateRatio,
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

	// Transaction-ID age — covers both the database datfrozenxid age and the
	// per-table relfrozenxid age (relfrozenxid_age_outlier rule); the worse of
	// the two drives the curve. Calibrated to PostgreSQL freeze mechanics and
	// escalating steeply through the danger zone toward the ~2.1B shutdown wall.
	// The previous curve flat-lined at 60 above 1.5B, so a DB minutes from a
	// forced shutdown scored the same as one merely at failsafe. Continuous and
	// monotonic:
	//   200M (emergency autovacuum) → 0, 1.6B (failsafe) → 80, 2.1B (shutdown) → 100.
	xidAge := m.MaxXidAge
	if m.MaxRelfrozenxidAge > xidAge {
		xidAge = m.MaxRelfrozenxidAge
	}

	switch {
	case xidAge > xidFailsafeAge:
		penalty = 80 + math.Min(float64(xidAge-xidFailsafeAge)/float64(xidShutdownAge-xidFailsafeAge)*20, 20)
	case xidAge > xidFreezeMaxAge:
		penalty = float64(xidAge-xidFreezeMaxAge) / float64(xidFailsafeAge-xidFreezeMaxAge) * 80
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

	// Per-table hygiene rules: autovacuum_enabled=false on individual tables
	// (tables_with_autovacuum_off) and planner stats drifted past their
	// auto-analyze point (stale_planner_stats). Both 0 = healthy, so they stay
	// neutral under per-DB scoring, which does not collect them.
	if m.TablesWithAutovacuumOff > 0 {
		penalty += math.Min(float64(m.TablesWithAutovacuumOff)*3, 15)
	}

	if m.StalePlannerStatsTables > 0 {
		penalty += math.Min(float64(m.StalePlannerStatsTables)*2, 15)
	}

	penalty = math.Min(penalty, 100)

	return CategoryResult{
		Name:    "maintenance",
		Weight:  weightMaintenance,
		Penalty: math.Round(penalty*10) / 10,
		Details: map[string]float64{
			"max_xid_age":                float64(m.MaxXidAge),
			"max_relfrozenxid_age":       float64(m.MaxRelfrozenxidAge),
			"max_vacuum_age_hours":       m.MaxVacuumAgeHours,
			"tables_never_vacuumed":      float64(m.TablesNeverVacuumed),
			"tables_with_autovacuum_off": float64(m.TablesWithAutovacuumOff),
			"stale_planner_stats_tables": float64(m.StalePlannerStatsTables),
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
