package health

import "testing"

func TestRegistry_AllRulesEvaluable(t *testing.T) {
	// All rules must produce nil or a Hit; no panic / no missing severity.
	m := RawMetrics{}

	for _, r := range Registry {
		hit := r.Evaluate(m)
		if hit != nil && hit.Severity == "" {
			t.Errorf("rule %q returned Hit without Severity", r.ID)
		}
	}
}

func TestRegistry_NoDuplicateIDs(t *testing.T) {
	seen := make(map[string]bool, len(Registry))
	for _, r := range Registry {
		if seen[r.ID] {
			t.Errorf("duplicate rule ID: %s", r.ID)
		}

		seen[r.ID] = true
	}
}

func TestEvaluate_HealthyDatabaseTriggersNoRules(t *testing.T) {
	m := RawMetrics{
		TotalConnections:         5,
		MaxConnections:           100,
		CacheHitRatio:            99.5,
		ReplicaCount:             1,
		MaxReplayLagSeconds:      0.1,
		MaxXidAge:                50_000_000,
		MaxOverdueVacuumAgeHours: 12,
		// Healthy state for new P1 rules:
		AutovacuumEnabled:    true,
		TrackCountsEnabled:   true,
		TrackIoTimingEnabled: true,
		HorizonLagXids:       0,
		MaxRelfrozenxidAge:   50_000_000,
		// Below requested_checkpoint_ratio's sample threshold (< 10 total).
		TimedCheckpoints:     5,
		RequestedCheckpoints: 0,
		// Healthy state for P3 rules:
		HotUpdateRatio:     1.0,       // > 0.80 → low_hot_update_ratio silent
		NewpageUpdateRatio: 0,         // < 0.05 → high_newpage_update_ratio silent
		WalLevel:           "replica", // neither "minimal" nor "logical"
	}

	got := Evaluate(m, false)
	if len(got) != 0 {
		t.Errorf("healthy DB → expect 0 recommendations, got %d: %+v", len(got), got)
	}
}

func TestEvaluate_DatabaseScopeFiltersInstanceOnlyCategories(t *testing.T) {
	// Trigger both an instance-only (connections) and a per-DB (storage) rule.
	m := RawMetrics{
		TotalConnections: 90,
		MaxConnections:   100,
		MaxDeadRatio:     55,
	}

	instance := Evaluate(m, false)
	if len(instance) < 2 {
		t.Fatalf("expected ≥2 recs at instance scope, got %d", len(instance))
	}

	dbScoped := Evaluate(m, true)
	for _, r := range dbScoped {
		if r.Category == "connections" || r.Category == "replication" {
			t.Errorf("db scope should hide %q, but got %+v", r.Category, r)
		}
	}
}

func TestEvaluate_SortedHighFirst(t *testing.T) {
	m := RawMetrics{
		TotalConnections:  98, // HIGH connection_ratio
		MaxConnections:    100,
		IdleInTransaction: 2,  // LOW idle_in_tx (threshold lowered to ≥2)
		MaxDeadRatio:      25, // MEDIUM dead_ratio
	}

	got := Evaluate(m, false)
	if len(got) < 3 {
		t.Fatalf("expected ≥ 3 recs, got %d", len(got))
	}

	for i := 1; i < len(got); i++ {
		if severityRank(got[i-1].Severity) > severityRank(got[i].Severity) {
			t.Errorf("rec %d (%s) ranked above rec %d (%s)", i-1, got[i-1].Severity, i, got[i].Severity)
		}
	}
}

func TestRule_HighConnectionRatio(t *testing.T) {
	tests := []struct {
		ratio float64
		want  Severity
	}{
		{0.5, ""},
		{0.65, SeverityLow},
		{0.85, SeverityMedium},
		{0.97, SeverityHigh},
	}

	for _, tc := range tests {
		m := RawMetrics{TotalConnections: int(tc.ratio * 100), MaxConnections: 100}
		hit := findRule(t, "high_connection_ratio").Evaluate(m)

		if tc.want == "" {
			if hit != nil {
				t.Errorf("ratio %.2f → want nil, got %v", tc.ratio, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("ratio %.2f → want %s, got %v", tc.ratio, tc.want, hit)
		}
	}
}

func TestRule_HighConnectionRatio_ZeroMaxConnections(t *testing.T) {
	m := RawMetrics{TotalConnections: 50, MaxConnections: 0}
	if hit := findRule(t, "high_connection_ratio").Evaluate(m); hit != nil {
		t.Errorf("zero max_connections must not trigger, got %+v", hit)
	}
}

func TestRule_LowCacheHitRatio(t *testing.T) {
	// Relaxed thresholds: 95/90/85 instead of 99/95/90.
	tests := []struct {
		ratio float64
		want  Severity
	}{
		{99, ""},
		{95, ""}, // boundary: below 95 triggers LOW
		{92, SeverityLow},
		{87, SeverityMedium},
		{80, SeverityHigh},
	}

	for _, tc := range tests {
		hit := findRule(t, "low_cache_hit_ratio").Evaluate(RawMetrics{CacheHitRatio: tc.ratio})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("ratio %v → want nil, got %v", tc.ratio, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("ratio %v → want %s, got %v", tc.ratio, tc.want, hit)
		}
	}
}

func TestRule_LowCacheHitRatioNeedsSample(t *testing.T) {
	tiny := RawMetrics{CacheHitRatio: 40, CacheSampleBlocks: 900}
	if hit := findRule(t, "low_cache_hit_ratio").Evaluate(tiny); hit != nil {
		t.Errorf("tiny block sample must not trigger, got %+v", hit)
	}

	busy := RawMetrics{CacheHitRatio: 40, CacheSampleBlocks: minCacheSampleBlocks}
	if hit := findRule(t, "low_cache_hit_ratio").Evaluate(busy); hit == nil {
		t.Error("sufficient block sample must still trigger")
	}
}

func TestRule_ReplicationOnlyTriggersWithReplicas(t *testing.T) {
	noRepl := RawMetrics{ReplicaCount: 0, MaxReplayLagSeconds: 60}
	for _, rid := range []string{"replication_lag_time", "replication_lag_bytes"} {
		if hit := findRule(t, rid).Evaluate(noRepl); hit != nil {
			t.Errorf("%s must not trigger without replicas, got %+v", rid, hit)
		}
	}
}

func TestRule_DisconnectedReplicasSeverity(t *testing.T) {
	r := findRule(t, "disconnected_replicas")
	if hit := r.Evaluate(RawMetrics{DisconnectedReplicas: 0}); hit != nil {
		t.Errorf("0 disconnected → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{DisconnectedReplicas: 1}); hit == nil || hit.Severity != SeverityMedium {
		t.Errorf("1 disconnected → MEDIUM, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{DisconnectedReplicas: 3}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("3 disconnected → HIGH, got %+v", hit)
	}
}

func TestRule_XidWraparoundRisk(t *testing.T) {
	// Recalibrated thresholds: 150M (vacuum_freeze_table_age),
	// 200M (autovacuum_freeze_max_age), 1.6B (vacuum_failsafe_age).
	r := findRule(t, "xid_wraparound_risk")
	cases := []struct {
		age  int64
		want Severity
	}{
		{100_000_000, ""},
		{160_000_000, SeverityLow},    // > 150M, < 200M
		{250_000_000, SeverityMedium}, // > 200M, < 1.6B
		{1_700_000_000, SeverityHigh}, // > 1.6B (failsafe)
	}

	for _, tc := range cases {
		hit := r.Evaluate(RawMetrics{MaxXidAge: tc.age})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("xid_age %d → want nil, got %v", tc.age, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("xid_age %d → want %s, got %v", tc.age, tc.want, hit)
		}
	}
}

func TestRule_AutovacuumDisabled(t *testing.T) {
	r := findRule(t, "autovacuum_disabled")

	if hit := r.Evaluate(RawMetrics{AutovacuumEnabled: true}); hit != nil {
		t.Errorf("enabled → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{AutovacuumEnabled: false}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("disabled → HIGH, got %+v", hit)
	}
}

func TestRule_TrackCountsDisabled(t *testing.T) {
	r := findRule(t, "track_counts_disabled")

	if hit := r.Evaluate(RawMetrics{TrackCountsEnabled: true}); hit != nil {
		t.Errorf("enabled → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{TrackCountsEnabled: false}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("disabled → HIGH, got %+v", hit)
	}
}

func TestRule_TablesWithAutovacuumOff(t *testing.T) {
	r := findRule(t, "tables_with_autovacuum_off")

	if hit := r.Evaluate(RawMetrics{TablesWithAutovacuumOff: 0}); hit != nil {
		t.Errorf("0 → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{TablesWithAutovacuumOff: 1}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("1 → LOW, got %+v", hit)
	}
}

func TestRule_RelfrozenxidAgeOutlier(t *testing.T) {
	// Shares thresholds with xid_wraparound_risk.
	r := findRule(t, "relfrozenxid_age_outlier")

	if hit := r.Evaluate(RawMetrics{MaxRelfrozenxidAge: 100_000_000}); hit != nil {
		t.Errorf("100M → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{MaxRelfrozenxidAge: 1_700_000_000}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("1.7B → HIGH, got %+v", hit)
	}
}

func TestRule_HorizonLagXids(t *testing.T) {
	r := findRule(t, "horizon_lag_xids")
	cases := []struct {
		lag  int64
		want Severity
	}{
		{500_000, ""},
		{5_000_000, SeverityLow},
		{50_000_000, SeverityMedium},
		{200_000_000, SeverityHigh},
	}

	for _, tc := range cases {
		hit := r.Evaluate(RawMetrics{HorizonLagXids: tc.lag})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("lag %d → want nil, got %v", tc.lag, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("lag %d → want %s, got %v", tc.lag, tc.want, hit)
		}
	}
}

func TestRule_RequestedCheckpointRatio(t *testing.T) {
	r := findRule(t, "requested_checkpoint_ratio")

	// Below sample-count threshold → no signal regardless of ratio.
	if hit := r.Evaluate(RawMetrics{TimedCheckpoints: 5, RequestedCheckpoints: 4}); hit != nil {
		t.Errorf("low sample count → nil, got %+v", hit)
	}

	// Healthy: only timed checkpoints.
	if hit := r.Evaluate(RawMetrics{TimedCheckpoints: 100, RequestedCheckpoints: 0}); hit != nil {
		t.Errorf("0%% requested → nil, got %+v", hit)
	}

	// 10% requested → MEDIUM.
	if hit := r.Evaluate(RawMetrics{TimedCheckpoints: 90, RequestedCheckpoints: 10}); hit == nil || hit.Severity != SeverityMedium {
		t.Errorf("10%% requested → MEDIUM, got %+v", hit)
	}

	// 50% requested → HIGH.
	if hit := r.Evaluate(RawMetrics{TimedCheckpoints: 50, RequestedCheckpoints: 50}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("50%% requested → HIGH, got %+v", hit)
	}
}

func TestRule_TrackIoTimingDisabled(t *testing.T) {
	r := findRule(t, "track_io_timing_disabled")

	if hit := r.Evaluate(RawMetrics{TrackIoTimingEnabled: true}); hit != nil {
		t.Errorf("enabled → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{TrackIoTimingEnabled: false}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("disabled → LOW, got %+v", hit)
	}
}

func TestEvaluate_DatabaseScopeFiltersNewInstanceCategories(t *testing.T) {
	// Trigger rules across the three new instance-only categories.
	m := RawMetrics{
		HorizonLagXids:    50_000_000, // horizon
		AutovacuumEnabled: true, TrackCountsEnabled: true,
		TimedCheckpoints:     50,
		RequestedCheckpoints: 50, // wal_checkpoint
	}

	dbScoped := Evaluate(m, true)
	for _, r := range dbScoped {
		if instanceOnlyCategories[r.Category] {
			t.Errorf("db scope must hide %q, but got %+v", r.Category, r)
		}
	}
}

func TestRule_ActiveLockWaiters(t *testing.T) {
	// Tightened thresholds: LOW ≥1, MED ≥3, HIGH ≥10.
	r := findRule(t, "active_lock_waiters")
	cases := []struct {
		n    int
		want Severity
	}{
		{0, ""},
		{1, SeverityLow},
		{3, SeverityMedium},
		{5, SeverityMedium},
		{10, SeverityHigh},
	}

	for _, tc := range cases {
		hit := r.Evaluate(RawMetrics{ActiveLockWaiters: tc.n})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("n=%d → want nil, got %v", tc.n, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("n=%d → want %s, got %v", tc.n, tc.want, hit)
		}
	}
}

func TestRule_LongestLockWaitSeconds(t *testing.T) {
	r := findRule(t, "longest_lock_wait_seconds")
	cases := []struct {
		s    float64
		want Severity
	}{
		{5, ""},
		{15, SeverityLow},
		{40, SeverityMedium},
		{120, SeverityHigh},
	}

	for _, tc := range cases {
		hit := r.Evaluate(RawMetrics{LongestLockWaitSeconds: tc.s})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("s=%v → want nil, got %v", tc.s, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("s=%v → want %s, got %v", tc.s, tc.want, hit)
		}
	}
}

func TestRule_UngrantedLocks(t *testing.T) {
	// Tightened thresholds: LOW ≥2, MED ≥5, HIGH ≥15.
	r := findRule(t, "ungranted_locks")

	if hit := r.Evaluate(RawMetrics{UngrantedLocks: 1}); hit != nil {
		t.Errorf("below threshold → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{UngrantedLocks: 2}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("2 → LOW, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{UngrantedLocks: 5}); hit == nil || hit.Severity != SeverityMedium {
		t.Errorf("5 → MED, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{UngrantedLocks: 20}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("20 → HIGH, got %+v", hit)
	}
}

func TestRule_DeadlocksRate(t *testing.T) {
	// Counter without history → only LOW at ≥1 (no MED/HIGH gradation
	// possible without per-day normalisation).
	r := findRule(t, "deadlocks_rate")

	if hit := r.Evaluate(RawMetrics{DeadlocksTotal: 0}); hit != nil {
		t.Errorf("0 → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{DeadlocksTotal: 1}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("1 → LOW, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{DeadlocksTotal: 500}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("500 → LOW (no HIGH gradation), got %+v", hit)
	}
}

func TestRule_LockPoolSaturation(t *testing.T) {
	r := findRule(t, "lock_pool_saturation")

	// Without GUC info we cannot evaluate — must return nil.
	if hit := r.Evaluate(RawMetrics{HeavyweightLocksTotal: 1000}); hit != nil {
		t.Errorf("no GUC info → nil, got %+v", hit)
	}

	// 64 × 100 = 6400 capacity, 5000 locks → ratio 0.78 → MED (>0.6, <0.8).
	m := RawMetrics{
		MaxConnections:         100,
		MaxLocksPerTransaction: 64,
		HeavyweightLocksTotal:  5000,
	}

	if hit := r.Evaluate(m); hit == nil || hit.Severity != SeverityMedium {
		t.Errorf("ratio 0.78 → MED, got %+v", hit)
	}

	// 5500 / 6400 = 0.86 → HIGH.
	m.HeavyweightLocksTotal = 5500
	if hit := r.Evaluate(m); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("ratio 0.86 → HIGH, got %+v", hit)
	}
}

func TestPenaltyLocks_Clean(t *testing.T) {
	// Healthy snapshot: zero waiters, no deadlocks, fresh pool.
	m := RawMetrics{
		MaxConnections:         100,
		MaxLocksPerTransaction: 64,
		HeavyweightLocksTotal:  50, // ratio ~0.008 → far below 0.5
	}

	c := penaltyLocks(m)
	if c.Penalty != 0 {
		t.Errorf("clean snapshot → 0 penalty, got %v", c.Penalty)
	}
}

func TestPenaltyLocks_HeavyContention(t *testing.T) {
	m := RawMetrics{
		ActiveLockWaiters:      10,
		LongestLockWaitSeconds: 120,
		UngrantedLocks:         25,
		DeadlocksTotal:         50,
		HeavyweightLocksTotal:  5500,
		MaxConnections:         100,
		MaxLocksPerTransaction: 64,
	}

	c := penaltyLocks(m)
	if c.Penalty < 80 {
		t.Errorf("heavy contention → ≥80 penalty, got %v", c.Penalty)
	}
}

func TestRule_LowHotUpdateRatio(t *testing.T) {
	// Relaxed thresholds: <0.80 LOW, <0.65 MED, <0.50 HIGH.
	r := findRule(t, "low_hot_update_ratio")
	cases := []struct {
		ratio float64
		want  Severity
	}{
		{0.95, ""},
		{0.85, ""},
		{0.70, SeverityLow},
		{0.55, SeverityMedium},
		{0.20, SeverityHigh},
	}

	for _, tc := range cases {
		hit := r.Evaluate(RawMetrics{HotUpdateRatio: tc.ratio})

		if tc.want == "" {
			if hit != nil {
				t.Errorf("ratio %v → want nil, got %v", tc.ratio, hit.Severity)
			}

			continue
		}

		if hit == nil || hit.Severity != tc.want {
			t.Errorf("ratio %v → want %s, got %v", tc.ratio, tc.want, hit)
		}
	}
}

func TestRule_HighNewpageUpdateRatio(t *testing.T) {
	r := findRule(t, "high_newpage_update_ratio")

	if hit := r.Evaluate(RawMetrics{NewpageUpdateRatio: 0.02}); hit != nil {
		t.Errorf("below threshold → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{NewpageUpdateRatio: 0.08}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("0.08 → LOW, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{NewpageUpdateRatio: 0.30}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("0.30 → HIGH, got %+v", hit)
	}
}

func TestRule_VacuumBacklog(t *testing.T) {
	r := findRule(t, "vacuum_backlog")

	if hit := r.Evaluate(RawMetrics{VacuumBacklogTables: 5}); hit != nil {
		t.Errorf("5 tables → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{VacuumBacklogTables: 6}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("6 tables → LOW, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{VacuumBacklogTables: 15}); hit == nil || hit.Severity != SeverityMedium {
		t.Errorf("15 tables → MEDIUM, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{VacuumBacklogTables: 30}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("30 tables → HIGH, got %+v", hit)
	}
}

func TestStaleVacuumUsesOverdueAge(t *testing.T) {
	r := findRule(t, "stale_vacuum")

	// Below 7 days overdue → no hit; well past 60 days → HIGH.
	if hit := r.Evaluate(RawMetrics{MaxOverdueVacuumAgeHours: 100}); hit != nil {
		t.Errorf("100h → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{MaxOverdueVacuumAgeHours: 2000}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("2000h → HIGH, got %+v", hit)
	}
}

func TestRule_StalePlannerStats(t *testing.T) {
	r := findRule(t, "stale_planner_stats")

	if hit := r.Evaluate(RawMetrics{StalePlannerStatsTables: 2}); hit != nil {
		t.Errorf("2 tables → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{StalePlannerStatsTables: 4}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("4 tables → LOW, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{StalePlannerStatsTables: 15}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("15 tables → HIGH, got %+v", hit)
	}
}

func TestRule_WalLevelMinimalWithReplicas(t *testing.T) {
	r := findRule(t, "wal_level_minimal_with_replicas")

	// wal_level=minimal but no replicas — internally consistent, no signal.
	if hit := r.Evaluate(RawMetrics{WalLevel: "minimal", ReplicaCount: 0}); hit != nil {
		t.Errorf("no replicas → nil, got %+v", hit)
	}

	// wal_level=replica with replicas — normal.
	if hit := r.Evaluate(RawMetrics{WalLevel: "replica", ReplicaCount: 2}); hit != nil {
		t.Errorf("replica + replicas → nil, got %+v", hit)
	}

	// Inconsistent: minimal + active replicas.
	if hit := r.Evaluate(RawMetrics{WalLevel: "minimal", ReplicaCount: 1}); hit == nil || hit.Severity != SeverityHigh {
		t.Errorf("minimal + replicas → HIGH, got %+v", hit)
	}
}

func TestRule_WalLevelLogicalWithoutPublications(t *testing.T) {
	r := findRule(t, "wal_level_logical_without_publications")

	// wal_level=replica — rule does not apply.
	if hit := r.Evaluate(RawMetrics{WalLevel: "replica"}); hit != nil {
		t.Errorf("replica → nil, got %+v", hit)
	}

	// wal_level=logical with active slots — not wasted.
	if hit := r.Evaluate(RawMetrics{WalLevel: "logical", LogicalSlotsActive: 2}); hit != nil {
		t.Errorf("logical + active slots → nil, got %+v", hit)
	}

	// wal_level=logical without slots — overhead.
	if hit := r.Evaluate(RawMetrics{WalLevel: "logical", LogicalSlotsActive: 0}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("logical + no slots → LOW, got %+v", hit)
	}
}

func findRule(t *testing.T, id string) Rule {
	t.Helper()

	for _, r := range Registry {
		if r.ID == id {
			return r
		}
	}

	t.Fatalf("rule %q not found in Registry", id)

	return Rule{}
}

func TestSequenceExhaustion_SeveritiesMatchSchemaLintLevels(t *testing.T) {
	// The page and the recommendation must agree: a finding shown as an error
	// there cannot come back as LOW here. Both read the same thresholds.
	cases := []struct {
		name    string
		freePct float64
		want    Severity
	}{
		{"below the error threshold", 4, SeverityHigh},
		{"below the warning threshold", 9, SeverityMedium},
		{"below the notice threshold", 19, SeverityLow},
		{"enough headroom", 25, ""},
	}

	rule := findRule(t, "sequence_exhaustion")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit := rule.Evaluate(RawMetrics{SequenceExhaustionMax: 1 - tc.freePct/100}) //nolint:exhaustruct

			if tc.want == "" {
				if hit != nil {
					t.Fatalf("%.0f%% free must not trigger, got %s", tc.freePct, hit.Severity)
				}

				return
			}

			if hit == nil {
				t.Fatalf("%.0f%% free must trigger %s, got nothing", tc.freePct, tc.want)
			}

			if hit.Severity != tc.want {
				t.Errorf("%.0f%% free: severity = %s, want %s", tc.freePct, hit.Severity, tc.want)
			}
		})
	}
}

func TestSequenceExhaustion_FollowsConfiguredThresholds(t *testing.T) {
	rule := findRule(t, "sequence_exhaustion")

	// 30% free is healthy by default and a problem once the operator says so —
	// the rule must read the same overrides the page does, or the two would
	// disagree about the very same sequence.
	m := RawMetrics{SequenceExhaustionMax: 0.7} //nolint:exhaustruct

	if hit := rule.Evaluate(m); hit != nil {
		t.Fatalf("30%% free must be silent by default, got %s", hit.Severity)
	}

	// 30% free now sits in the warning band (between the error and the warning
	// threshold), which the page shows as a warning and the score as MEDIUM.
	m.SequenceThresholds = map[string]float64{"error": 20, "warning": 40, "notice": 50}

	hit := rule.Evaluate(m)
	if hit == nil || hit.Severity != SeverityMedium {
		t.Fatalf("with the raised thresholds 30%% free is MEDIUM, got %+v", hit)
	}
}

func TestSequenceExhaustion_BoundaryMatchesThePage(t *testing.T) {
	rule := findRule(t, "sequence_exhaustion")

	// Exactly 5% free: the page calls this a warning (its error threshold is
	// strictly below 5%), so the recommendation must not be HIGH.
	hit := rule.Evaluate(RawMetrics{SequenceExhaustionMax: 0.95}) //nolint:exhaustruct
	if hit == nil || hit.Severity != SeverityMedium {
		t.Fatalf("at exactly 5%% free want MEDIUM, got %+v", hit)
	}
}

func TestRules_MetricsBackedConditionsFire(t *testing.T) {
	cases := []struct {
		name string
		m    RawMetrics
		rule string
	}{
		{"checksum", RawMetrics{ChecksumFailuresRate: 1}, "checksum_failures"},
		{"host cpu", RawMetrics{NumVCPU: 2, LoadAvg15: 8}, "host_cpu_saturation"},
		{"pooler", RawMetrics{PoolerPoolSize: 10, PoolerServerConns: 9}, "pooler_saturation"},
		{"sequence", RawMetrics{SequenceExhaustionMax: 0.9}, "sequence_exhaustion"},
		{"latency", RawMetrics{LatencyRegressionRatio: 4}, "latency_regression"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := false

			for _, r := range Evaluate(tc.m, false) {
				if r.RuleID == tc.rule {
					found = true
				}
			}

			if !found {
				t.Errorf("expected rule %q to fire for its metrics-backed condition", tc.rule)
			}
		})
	}
}

func TestEvaluate_DatabaseAttribution(t *testing.T) {
	// pg_stat_activity is instance-wide, so the session behind an activity rule
	// usually lives in another database than the snapshot's. Each recommendation
	// has to carry the database the UI should open, not the selected one.
	m := RawMetrics{
		Database:                   "snapshot_db",
		MaxConnections:             100,
		TotalConnections:           5,
		CacheHitRatio:              99.9,
		IdleInTransaction:          6,
		IdleInTransactionDatabase:  "idle_db",
		LongestTransactionSeconds:  3600,
		LongestTransactionDatabase: "long_tx_db",
		HorizonLagXids:             50_000_000,
		HorizonDatabase:            "horizon_db",
		MaxDeadRatio:               50,
		HotUpdateRatio:             1.0,
		AutovacuumEnabled:          true,
		TrackCountsEnabled:         true,
		TrackIoTimingEnabled:       true,
		WalLevel:                   "replica",
	}

	got := make(map[string]string, len(Registry))
	for _, r := range Evaluate(m, false) {
		got[r.RuleID] = r.Database
	}

	want := map[string]string{
		"idle_in_transaction":      "idle_db",
		"long_running_transaction": "long_tx_db",
		"horizon_lag_xids":         "horizon_db",
		"high_max_dead_ratio":      "snapshot_db", // per-database metric
	}

	for rule, db := range want {
		if _, ok := got[rule]; !ok {
			t.Fatalf("test sanity: rule %q did not fire", rule)
		}

		if got[rule] != db {
			t.Errorf("rule %q: expected database %q, got %q", rule, db, got[rule])
		}
	}
}

func TestEvaluate_InstanceWideRuleHasNoDatabase(t *testing.T) {
	// Nothing pins a saturated connection pool to one database — it must stay
	// unattributed so the UI labels it cluster-wide.
	m := RawMetrics{
		Database:         "snapshot_db",
		MaxConnections:   100,
		TotalConnections: 99,
		CacheHitRatio:    99.9,
		HotUpdateRatio:   1.0,
	}

	for _, r := range Evaluate(m, false) {
		if r.RuleID != "high_connection_ratio" {
			continue
		}

		if r.Database != "" {
			t.Errorf("high_connection_ratio must not name a database, got %q", r.Database)
		}

		return
	}

	t.Fatal("test sanity: high_connection_ratio did not fire")
}

// metricsBackedRaw is a metrics-mode snapshot: the datasource signals are
// instance-wide aggregates, the rest is the catalog overlay read from Database.
func metricsBackedRaw() RawMetrics {
	return RawMetrics{
		Database:            "catalog_db",
		MetricsInstanceWide: true,

		MaxConnections:   100,
		TotalConnections: 5,

		// From the datasource — sum()/max() over every database.
		CacheHitRatio:  80,
		MaxDeadRatio:   50,
		AvgDeadRatio:   30,
		HotUpdateRatio: 0.4,
		MaxXidAge:      xidFreezeMaxAge,

		// Overlaid from the catalog snapshot of Database.
		TablesHighBloat:         8,
		NewpageUpdateRatio:      0.25,
		VacuumBacklogTables:     10,
		TablesNeverVacuumed:     3,
		TablesWithAutovacuumOff: 2,
		StalePlannerStatsTables: 6,

		AutovacuumEnabled:    true,
		TrackCountsEnabled:   true,
		TrackIoTimingEnabled: true,
	}
}

func TestEvaluate_MetricsModeDropsDatabaseOnAggregateRules(t *testing.T) {
	// In metrics mode the per-database-looking signals are instance-wide
	// PromQL aggregates, so naming the catalog snapshot's database would point
	// the UI at a database that has nothing to do with the finding.
	got := make(map[string]string, len(Registry))
	for _, r := range Evaluate(metricsBackedRaw(), false) {
		got[r.RuleID] = r.Database
	}

	want := map[string]string{
		// Datasource aggregates — unattributed.
		"low_cache_hit_ratio":  "",
		"high_max_dead_ratio":  "",
		"high_avg_dead_ratio":  "",
		"low_hot_update_ratio": "",
		"xid_wraparound_risk":  "",
		// Catalog overlay — still describes one database.
		"many_bloated_tables":        "catalog_db",
		"high_newpage_update_ratio":  "catalog_db",
		"vacuum_backlog":             "catalog_db",
		"tables_never_vacuumed":      "catalog_db",
		"tables_with_autovacuum_off": "catalog_db",
		"stale_planner_stats":        "catalog_db",
	}

	for rule, db := range want {
		if _, ok := got[rule]; !ok {
			t.Fatalf("test sanity: rule %q did not fire", rule)
		}

		if got[rule] != db {
			t.Errorf("rule %q in metrics mode: expected database %q, got %q", rule, db, got[rule])
		}
	}
}

func TestEvaluate_SnapshotModeKeepsDatabaseOnSameRules(t *testing.T) {
	// The very same values read from a SQL snapshot ARE facts about that
	// database, so the attribution must survive there — the drop is a property
	// of the metrics path only.
	m := metricsBackedRaw()
	m.MetricsInstanceWide = false

	got := make(map[string]string, len(Registry))
	for _, r := range Evaluate(m, false) {
		got[r.RuleID] = r.Database
	}

	checked := 0

	for rule := range metricsInstanceWideRules {
		if _, ok := got[rule]; !ok {
			continue // needs a signal this fixture does not carry
		}

		checked++

		if got[rule] != "catalog_db" {
			t.Errorf("rule %q in snapshot mode: expected database %q, got %q", rule, "catalog_db", got[rule])
		}
	}

	// Without this the test passes vacuously the day the fixture stops firing
	// any of them — which is exactly when the assertion would matter.
	if checked == 0 {
		t.Fatal("test sanity: the fixture fired none of the metrics-aggregate rules")
	}
}

func TestEvaluate_SequenceExhaustionScope(t *testing.T) {
	// At instance scope the worst sequence is picked across every database of
	// the instance — by the datasource's max() and, just as much, by the SQL
	// fallback sweeping all pools. Only the drill-down reads one database.
	m := metricsBackedRaw()
	m.MetricsInstanceWide = false // the SQL path is instance-wide here as well
	m.SequenceExhaustionMax = 0.99
	m.SequenceThresholds = map[string]float64{"error": 20, "warning": 40, "notice": 50}

	find := func(t *testing.T, databaseScoped bool) Recommendation {
		t.Helper()

		for _, r := range Evaluate(m, databaseScoped) {
			if r.RuleID == "sequence_exhaustion" {
				return r
			}
		}

		t.Fatalf("test sanity: sequence_exhaustion did not fire (databaseScoped=%v)", databaseScoped)

		return Recommendation{} //nolint:exhaustruct // unreachable, t.Fatalf stops the test
	}

	if got := find(t, false).Database; got != "" {
		t.Errorf("at instance scope sequence_exhaustion spans every database, got %q", got)
	}

	if got := find(t, true).Database; got != "catalog_db" {
		t.Errorf("in the drill-down sequence_exhaustion describes one database, got %q", got)
	}
}

func TestUnattributedRuleSets_MatchRegistry(t *testing.T) {
	// A typo or a renamed/removed rule would silently leave the entry dead, and
	// an instance-only category is already unattributed — the entry would be
	// redundant. Both mean a map has drifted from the registry.
	byID := make(map[string]Rule, len(Registry))
	for _, r := range Registry {
		byID[r.ID] = r
	}

	sets := map[string]map[string]bool{
		"metricsInstanceWideRules":    metricsInstanceWideRules,
		"instanceScopeAggregateRules": instanceScopeAggregateRules,
	}

	for name, set := range sets {
		for id := range set {
			r, ok := byID[id]
			if !ok {
				t.Errorf("%s names %q, which is not in the registry", name, id)
				continue
			}

			if instanceOnlyCategories[r.Category] {
				t.Errorf("%s: rule %q is in instance-only category %q — the entry is redundant",
					name, id, r.Category)
			}
		}
	}

	// instanceScopeAggregateRules already drops the attribution at instance
	// scope, which is the only scope the metrics path runs in — listing a rule
	// in both would make the metrics entry unreachable.
	for id := range instanceScopeAggregateRules {
		if metricsInstanceWideRules[id] {
			t.Errorf("rule %q is in both sets — the metricsInstanceWideRules entry is dead", id)
		}
	}
}

func TestEvaluate_MetricsModeKeepsDatabaseOnSnapshotBackedRules(t *testing.T) {
	// Still metrics mode, but the datasource carried no series for these two
	// signals, so the handler backfilled them from the SQL snapshot of
	// Database. Those values are per-database facts after all — dropping the
	// attribution would send the user looking for a cache-hit problem across
	// the whole instance when it was read from one database.
	m := metricsBackedRaw()
	m.MarkSnapshotBacked("low_cache_hit_ratio", "xid_wraparound_risk")

	got := make(map[string]string, len(Registry))
	for _, r := range Evaluate(m, false) {
		got[r.RuleID] = r.Database
	}

	want := map[string]string{
		// Backfilled from the snapshot — attributed.
		"low_cache_hit_ratio": "catalog_db",
		"xid_wraparound_risk": "catalog_db",
		// Still datasource aggregates — unattributed.
		"high_max_dead_ratio":  "",
		"high_avg_dead_ratio":  "",
		"low_hot_update_ratio": "",
	}

	for rule, db := range want {
		if _, ok := got[rule]; !ok {
			t.Fatalf("test sanity: rule %q did not fire", rule)
		}

		if got[rule] != db {
			t.Errorf("rule %q: expected database %q, got %q", rule, db, got[rule])
		}
	}
}

func TestMarkSnapshotBacked_AllocatesAndAccumulates(t *testing.T) {
	var m RawMetrics // nil map — the handler marks rule by rule as it backfills

	m.MarkSnapshotBacked("low_cache_hit_ratio")
	m.MarkSnapshotBacked("high_max_dead_ratio", "xid_wraparound_risk")

	for _, id := range []string{"low_cache_hit_ratio", "high_max_dead_ratio", "xid_wraparound_risk"} {
		if !m.SnapshotBackedRules[id] {
			t.Errorf("rule %q not marked", id)
		}
	}

	if m.SnapshotBackedRules["low_hot_update_ratio"] {
		t.Error("unmarked rule reads as snapshot-backed")
	}
}
