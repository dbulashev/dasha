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
		TotalConnections:    5,
		MaxConnections:      100,
		CacheHitRatio:       99.5,
		ReplicaCount:        1,
		MaxReplayLagSeconds: 0.1,
		MaxXidAge:           50_000_000,
		MaxVacuumAgeHours:   12,
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
		IdleInTransaction: 2, // LOW idle_in_tx (threshold lowered to ≥2)
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
		{160_000_000, SeverityLow},     // > 150M, < 200M
		{250_000_000, SeverityMedium},  // > 200M, < 1.6B
		{1_700_000_000, SeverityHigh},  // > 1.6B (failsafe)
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
		HorizonLagXids:    50_000_000,             // horizon
		AutovacuumEnabled: true, TrackCountsEnabled: true,
		TimedCheckpoints:    50,
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

func TestRule_AnalyzeDisabledTables(t *testing.T) {
	r := findRule(t, "analyze_disabled_tables")

	if hit := r.Evaluate(RawMetrics{AnalyzeDisabledTables: 0}); hit != nil {
		t.Errorf("0 → nil, got %+v", hit)
	}

	if hit := r.Evaluate(RawMetrics{AnalyzeDisabledTables: 3}); hit == nil || hit.Severity != SeverityLow {
		t.Errorf("3 → LOW, got %+v", hit)
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
