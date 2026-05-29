package health

import (
	"math"
	"testing"
)

func TestCalculateWithWeights_InvalidFallsBackToDefaults(t *testing.T) {
	m := RawMetrics{
		TotalConnections: 5, MaxConnections: 100, CacheHitRatio: 99.5,
		ReplicaCount: 1, MaxReplayLagSeconds: 0.1,
		MaxXidAge: 50_000_000, MaxVacuumAgeHours: 12,
		AutovacuumEnabled: true, TrackCountsEnabled: true, TrackIoTimingEnabled: true,
		TimedCheckpoints: 5,
	}

	// Compare against known-good baseline (defaults).
	want := CalculateWithWeights(m, DefaultWeights()).Score

	cases := []struct {
		name string
		w    Weights
	}{
		{"all zero", Weights{}},
		{"NaN in one field", Weights{Connections: math.NaN(), Performance: 0.5}},
		{"+Inf", Weights{Storage: math.Inf(1)}},
		{"negative", Weights{Connections: -0.5, Performance: 0.5}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateWithWeights(m, tc.w).Score
			if math.Abs(got-want) > 0.1 {
				t.Errorf("invalid weights should fall back to defaults: got score=%v, want=%v", got, want)
			}
		})
	}
}

func TestCalculate_HealthyDatabase(t *testing.T) {
	m := RawMetrics{
		TotalConnections:          10,
		ActiveConnections:         5,
		IdleInTransaction:         0,
		LongestTransactionSeconds: 0,
		MaxConnections:            100,
		CacheHitRatio:             99.5,
		MaxDeadRatio:              2,
		AvgDeadRatio:              1,
		TablesHighBloat:           0,
		ReplicaCount:              1,
		MaxReplayLagSeconds:       0.1,
		MaxLagBytes:               1024,
		DisconnectedReplicas:      0,
		MaxXidAge:                 100_000_000,
		MaxVacuumAgeHours:         12,
		TablesNeverVacuumed:       0,
		AutovacuumEnabled:         true,
		TrackCountsEnabled:        true,
		TrackIoTimingEnabled:      true,
		HotUpdateRatio:            0.95,
	}

	r := Calculate(m)
	if r.Score < 95 {
		t.Errorf("healthy database should score >= 95, got %v", r.Score)
	}
	if !r.HasReplication {
		t.Error("expected HasReplication = true")
	}
	if len(r.Categories) != 8 {
		t.Errorf("expected 8 categories, got %d", len(r.Categories))
	}
}

func TestCalculate_CriticalDatabase(t *testing.T) {
	// With 8 categories, a "critical" snapshot must trip enough of them to
	// pull the score below 40. Beyond the original five (connections /
	// performance / storage / replication / maintenance) we engage horizon,
	// wal_checkpoint and locks too — otherwise their zero-penalty weights
	// dilute the average.
	m := RawMetrics{
		TotalConnections:          95,
		ActiveConnections:         80,
		IdleInTransaction:         10,
		LongestTransactionSeconds: 600,
		MaxConnections:            100,
		CacheHitRatio:             80, // <85 → HIGH tier
		MaxDeadRatio:              60,
		AvgDeadRatio:              25,
		TablesHighBloat:           10,
		ReplicaCount:              2,
		MaxReplayLagSeconds:       60,
		MaxLagBytes:               500 * 1024 * 1024,
		DisconnectedReplicas:      1,
		MaxXidAge:                 1_900_000_000, // near wraparound
		MaxVacuumAgeHours:         2000,          // >60 days → upper tier
		TablesNeverVacuumed:       5,
		HorizonLagXids:            100_000_000, // HIGH tier
		TimedCheckpoints:          50,
		RequestedCheckpoints:      50, // 50% requested → HIGH tier
		ActiveLockWaiters:         15,
		LongestLockWaitSeconds:    120,
		UngrantedLocks:            20,
		DeadlocksTotal:            50,
	}

	r := Calculate(m)
	if r.Score > 40 {
		t.Errorf("critical database should score < 40, got %v", r.Score)
	}
}

func TestCalculate_NoReplication(t *testing.T) {
	m := RawMetrics{
		TotalConnections:     10,
		MaxConnections:       100,
		CacheHitRatio:        99.5,
		ReplicaCount:         0,
		AutovacuumEnabled:    true,
		TrackCountsEnabled:   true,
		TrackIoTimingEnabled: true,
		HotUpdateRatio:       0.95,
	}

	r := Calculate(m)
	if r.HasReplication {
		t.Error("expected HasReplication = false")
	}

	// replication weight should be redistributed
	var replWeight float64
	for _, c := range r.Categories {
		if c.Name == "replication" {
			replWeight = c.Weight
		}
	}
	if replWeight != 0 {
		t.Errorf("replication weight should be 0 when no replicas, got %v", replWeight)
	}

	// other weights should sum to 1.0
	var totalWeight float64
	for _, c := range r.Categories {
		totalWeight += c.Weight
	}
	if totalWeight < 0.99 || totalWeight > 1.01 {
		t.Errorf("total weights should sum to ~1.0, got %v", totalWeight)
	}
}

func TestPenaltyConnections_LowUtilization(t *testing.T) {
	m := RawMetrics{TotalConnections: 10, MaxConnections: 100}
	r := penaltyConnections(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty for 10%% utilization, got %v", r.Penalty)
	}
}

func TestPenaltyConnections_HighUtilization(t *testing.T) {
	m := RawMetrics{TotalConnections: 96, MaxConnections: 100}
	r := penaltyConnections(m)
	if r.Penalty < 70 {
		t.Errorf("expected penalty >= 70 for 96%% utilization, got %v", r.Penalty)
	}
}

func TestPenaltyConnections_IdleInTransaction(t *testing.T) {
	m := RawMetrics{TotalConnections: 10, MaxConnections: 100, IdleInTransaction: 5}
	r := penaltyConnections(m)
	if r.Penalty != 25 {
		t.Errorf("expected 25 penalty for 5 idle-in-tx, got %v", r.Penalty)
	}
}

func TestPenaltyConnections_LongTransaction(t *testing.T) {
	m := RawMetrics{TotalConnections: 10, MaxConnections: 100, LongestTransactionSeconds: 600}
	r := penaltyConnections(m)
	if r.Penalty != 20 {
		t.Errorf("expected 20 penalty for 600s longest tx (capped), got %v", r.Penalty)
	}
}

func TestPenaltyPerformance_HighCacheHit(t *testing.T) {
	m := RawMetrics{CacheHitRatio: 99.5}
	r := penaltyPerformance(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty for 99.5%% cache hit, got %v", r.Penalty)
	}
}

func TestPenaltyPerformance_LowCacheHit(t *testing.T) {
	m := RawMetrics{CacheHitRatio: 85}
	r := penaltyPerformance(m)
	if r.Penalty < 50 {
		t.Errorf("expected penalty >= 50 for 85%% cache hit, got %v", r.Penalty)
	}
}

func TestPenaltyStorage_Clean(t *testing.T) {
	m := RawMetrics{MaxDeadRatio: 0, AvgDeadRatio: 0, TablesHighBloat: 0}
	r := penaltyStorage(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty for clean storage, got %v", r.Penalty)
	}
}

func TestPenaltyStorage_HighBloat(t *testing.T) {
	m := RawMetrics{MaxDeadRatio: 55, AvgDeadRatio: 15, TablesHighBloat: 8}
	r := penaltyStorage(m)
	if r.Penalty < 60 {
		t.Errorf("expected penalty >= 60 for high bloat, got %v", r.Penalty)
	}
}

func TestPenaltyReplication_NoReplicas(t *testing.T) {
	m := RawMetrics{ReplicaCount: 0}
	r := penaltyReplication(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty when no replicas, got %v", r.Penalty)
	}
}

func TestPenaltyReplication_HighLag(t *testing.T) {
	m := RawMetrics{ReplicaCount: 1, MaxReplayLagSeconds: 45, MaxLagBytes: 200 * 1024 * 1024}
	r := penaltyReplication(m)
	if r.Penalty < 50 {
		t.Errorf("expected penalty >= 50 for 45s lag, got %v", r.Penalty)
	}
}

func TestPenaltyReplication_DisconnectedReplica(t *testing.T) {
	m := RawMetrics{ReplicaCount: 2, DisconnectedReplicas: 1}
	r := penaltyReplication(m)
	if r.Penalty < 30 {
		t.Errorf("expected penalty >= 30 for disconnected replica, got %v", r.Penalty)
	}
}

func TestPenaltyMaintenance_Healthy(t *testing.T) {
	m := RawMetrics{MaxXidAge: 100_000_000, MaxVacuumAgeHours: 12}
	r := penaltyMaintenance(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty for healthy maintenance, got %v", r.Penalty)
	}
}

func TestPenaltyMaintenance_XidDanger(t *testing.T) {
	m := RawMetrics{MaxXidAge: 1_600_000_000}
	r := penaltyMaintenance(m)
	if r.Penalty < 60 {
		t.Errorf("expected penalty >= 60 for XID > 1.5B, got %v", r.Penalty)
	}
}

func TestPenaltyMaintenance_XidEscalatesToShutdown(t *testing.T) {
	// The xid penalty must keep climbing through the danger zone instead of
	// flat-lining: emergency-autovacuum threshold → 0, failsafe ≈ 80, shutdown
	// wall → 100, strictly monotonic in between.
	at := func(age int64) float64 { return penaltyMaintenance(RawMetrics{MaxXidAge: age}).Penalty }

	if p := at(xidFreezeMaxAge); p != 0 {
		t.Errorf("penalty at emergency-autovacuum threshold should be 0, got %v", p)
	}

	if p := at(xidFailsafeAge); p < 79 || p > 81 {
		t.Errorf("penalty at failsafe should be ~80, got %v", p)
	}

	if p := at(xidShutdownAge); p < 99.9 {
		t.Errorf("penalty at shutdown wall should be ~100, got %v", p)
	}

	if at(1_800_000_000) <= at(xidFailsafeAge) {
		t.Error("penalty must keep increasing past failsafe (was previously flat at 60)")
	}
}

func TestCriticalCeiling_FloorsCatastrophicConditions(t *testing.T) {
	healthy := RawMetrics{
		TotalConnections: 10, MaxConnections: 100, CacheHitRatio: 99.5,
		ReplicaCount: 1, MaxReplayLagSeconds: 0.1,
		MaxXidAge: 50_000_000, MaxVacuumAgeHours: 12,
		AutovacuumEnabled: true, TrackCountsEnabled: true,
		TrackIoTimingEnabled: true, HotUpdateRatio: 0.95,
	}

	cases := []struct {
		name   string
		mutate func(*RawMetrics)
		red    bool
	}{
		{"baseline healthy", func(*RawMetrics) {}, false},
		{"xid at failsafe", func(m *RawMetrics) { m.MaxXidAge = xidFailsafeAge }, true},
		{"relfrozenxid at failsafe", func(m *RawMetrics) { m.MaxRelfrozenxidAge = xidFailsafeAge }, true},
		{"autovacuum off", func(m *RawMetrics) { m.AutovacuumEnabled = false }, true},
		{"track_counts off", func(m *RawMetrics) { m.TrackCountsEnabled = false }, true},
		{"standby ignores maintenance floor", func(m *RawMetrics) {
			m.InRecovery = true
			m.MaxXidAge = 1_900_000_000
			m.AutovacuumEnabled = false
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := healthy
			tc.mutate(&m)
			score := Calculate(m).Score

			if tc.red && score > criticalScoreCeiling {
				t.Errorf("expected score clamped to <= %v (red), got %v", criticalScoreCeiling, score)
			}

			if !tc.red && score <= criticalScoreCeiling {
				t.Errorf("expected healthy score well above %v, got %v", criticalScoreCeiling, score)
			}
		})
	}
}

func TestInstanceAdjustments_CoverRuleConditions(t *testing.T) {
	// Every instance-only condition that produces a rule must also move the
	// score: the category it belongs to should drop below its clean baseline.
	base := RawMetrics{
		TotalConnections: 10, MaxConnections: 100, CacheHitRatio: 99.5,
		ReplicaCount: 1, MaxReplayLagSeconds: 0.1,
		MaxXidAge: 50_000_000, MaxVacuumAgeHours: 12,
		AutovacuumEnabled: true, TrackCountsEnabled: true,
		TrackIoTimingEnabled: true, HotUpdateRatio: 0.95,
		WalLevel: "replica",
	}

	catScore := func(r Result, name string) float64 {
		for _, c := range r.Categories {
			if c.Name == name {
				return c.Score
			}
		}

		t.Fatalf("category %q not found", name)

		return 0
	}

	clean := Calculate(base)

	cases := []struct {
		name     string
		category string
		mutate   func(*RawMetrics)
	}{
		{"track_io_timing off", "performance", func(m *RawMetrics) { m.TrackIoTimingEnabled = false }},
		{"low hot-update ratio", "storage", func(m *RawMetrics) { m.HotUpdateRatio = 0.3 }},
		{"newpage update ratio", "storage", func(m *RawMetrics) { m.NewpageUpdateRatio = 0.25 }},
		{"tables with autovacuum off", "maintenance", func(m *RawMetrics) { m.TablesWithAutovacuumOff = 3 }},
		{"stale planner stats", "maintenance", func(m *RawMetrics) { m.StalePlannerStatsTables = 8 }},
		{"relfrozenxid outlier", "maintenance", func(m *RawMetrics) { m.MaxRelfrozenxidAge = 1_000_000_000 }},
		{"wal_level minimal with replicas", "wal_checkpoint", func(m *RawMetrics) { m.WalLevel = "minimal" }},
		{"wal_level logical without slots", "wal_checkpoint", func(m *RawMetrics) { m.WalLevel = "logical" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			tc.mutate(&m)

			got := catScore(Calculate(m), tc.category)
			if baseline := catScore(clean, tc.category); got >= baseline {
				t.Errorf("%s category should drop below clean baseline %v, got %v", tc.category, baseline, got)
			}
		})
	}
}

func TestInstanceAdjustments_WalLevelTooltipFlag(t *testing.T) {
	// wal_level can't go in the float64 Details map, so the offending condition
	// is surfaced as a presence flag for the tooltip. Present only when the
	// misconfiguration actually holds.
	walDetail := func(m RawMetrics, key string) bool {
		for _, c := range Calculate(m).Categories {
			if c.Name == "wal_checkpoint" {
				_, ok := c.Details[key]
				return ok
			}
		}

		return false
	}

	base := RawMetrics{
		TotalConnections: 10, MaxConnections: 100, CacheHitRatio: 99.5,
		AutovacuumEnabled: true, TrackCountsEnabled: true,
		TrackIoTimingEnabled: true, HotUpdateRatio: 0.95,
		WalLevel: "replica",
	}

	if walDetail(base, "wal_level_minimal_with_replicas") ||
		walDetail(base, "wal_level_logical_without_slots") {
		t.Error("no wal_level flag expected on a healthy replica config")
	}

	minimal := base
	minimal.WalLevel = "minimal"
	minimal.ReplicaCount = 1

	if !walDetail(minimal, "wal_level_minimal_with_replicas") {
		t.Error("expected wal_level_minimal_with_replicas flag in tooltip details")
	}

	logical := base
	logical.WalLevel = "logical"

	if !walDetail(logical, "wal_level_logical_without_slots") {
		t.Error("expected wal_level_logical_without_slots flag in tooltip details")
	}
}

func TestPenaltyMaintenance_StaleVacuum(t *testing.T) {
	// Relaxed thresholds (7/21/60 days): 10-day age is no longer treated as a
	// strong signal — read-mostly tables legitimately go weeks without vacuum.
	// 30 days lands in the second tier (21–60d) and still triggers a meaningful
	// penalty.
	m := RawMetrics{MaxVacuumAgeHours: 720} // 30 days
	r := penaltyMaintenance(m)
	if r.Penalty < 10 {
		t.Errorf("expected penalty >= 10 for 30-day vacuum age, got %v", r.Penalty)
	}
}

func TestPenaltyConnections_ZeroMaxConnections(t *testing.T) {
	m := RawMetrics{MaxConnections: 0}
	r := penaltyConnections(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty when max_connections is 0, got %v", r.Penalty)
	}
}

func TestCalculate_InRecovery_DropsMaintenance(t *testing.T) {
	// On a standby maintenance has no signal — autovacuum/ANALYZE can't run —
	// so its weight must be redistributed and its penalty zeroed regardless of
	// any maintenance metric values fed in.
	m := RawMetrics{
		InRecovery:       true,
		TotalConnections: 5,
		MaxConnections:   100,
		CacheHitRatio:    99.5,
		ReplicaCount:     1, // replication is active (cascading standby chain ignored)
		// Catastrophic maintenance metrics that MUST be ignored on a standby:
		MaxXidAge:          1_900_000_000,
		MaxVacuumAgeHours:  10_000,
		AutovacuumEnabled:  false,
		TrackCountsEnabled: false,
		// Non-maintenance signals stay healthy so only the maintenance drop is exercised.
		TrackIoTimingEnabled: true,
		HotUpdateRatio:       0.95,
	}

	r := Calculate(m)

	if !r.InRecovery {
		t.Error("expected InRecovery = true in result")
	}

	var maintWeight, maintPenalty float64
	for _, c := range r.Categories {
		if c.Name == "maintenance" {
			maintWeight = c.Weight
			maintPenalty = c.Penalty
		}
	}

	if maintWeight != 0 {
		t.Errorf("maintenance weight must be 0 on a standby, got %v", maintWeight)
	}

	if maintPenalty != 0 {
		t.Errorf("maintenance penalty must be zeroed on a standby, got %v", maintPenalty)
	}

	// Surviving weights still sum to 1.0.
	var total float64
	for _, c := range r.Categories {
		total += c.Weight
	}

	if total < 0.99 || total > 1.01 {
		t.Errorf("remaining weights should sum to ~1.0, got %v", total)
	}

	// A standby with otherwise healthy metrics scores essentially 100.
	if r.Score < 95 {
		t.Errorf("healthy standby should keep a high score, got %v", r.Score)
	}
}

func TestCalculate_InRecovery_NoReplicas_DropsBoth(t *testing.T) {
	// Standby with no downstream cascaded standbys: both replication and
	// maintenance are dropped, weight redistributed across the remaining six.
	m := RawMetrics{
		InRecovery:       true,
		TotalConnections: 5,
		MaxConnections:   100,
		CacheHitRatio:    99.5,
		ReplicaCount:     0,
	}

	r := Calculate(m)

	for _, c := range r.Categories {
		if (c.Name == "replication" || c.Name == "maintenance") && c.Weight != 0 {
			t.Errorf("%s weight must be 0, got %v", c.Name, c.Weight)
		}
	}

	var total float64
	for _, c := range r.Categories {
		total += c.Weight
	}

	if total < 0.99 || total > 1.01 {
		t.Errorf("remaining weights should sum to ~1.0, got %v", total)
	}
}

func TestEvaluate_InRecovery_HidesMaintenanceRules(t *testing.T) {
	// Catastrophic maintenance metrics must not surface on a standby.
	m := RawMetrics{
		InRecovery:         true,
		MaxXidAge:          1_900_000_000,
		MaxVacuumAgeHours:  10_000,
		AutovacuumEnabled:  false,
		TrackCountsEnabled: false,
	}

	for _, r := range Evaluate(m, false) {
		if r.Category == "maintenance" {
			t.Errorf("maintenance rule %q should be hidden on a standby, got %+v", r.RuleID, r)
		}
	}
}

func TestRedistributeWeights(t *testing.T) {
	// Use the 8-category default arrangement (sum = 1.00).
	categories := []CategoryResult{
		{Name: "connections", Weight: 0.15},
		{Name: "performance", Weight: 0.15},
		{Name: "storage", Weight: 0.10},
		{Name: "replication", Weight: 0.15},
		{Name: "maintenance", Weight: 0.15},
		{Name: "horizon", Weight: 0.10},
		{Name: "wal_checkpoint", Weight: 0.10},
		{Name: "locks", Weight: 0.10},
	}

	redistributeWeights(categories, []string{"replication"})

	var total float64
	for _, c := range categories {
		if c.Name == "replication" && c.Weight != 0 {
			t.Errorf("replication weight should be 0 after redistribution, got %v", c.Weight)
		}
		total += c.Weight
	}

	if total < 0.99 || total > 1.01 {
		t.Errorf("total weights should sum to ~1.0 after redistribution, got %v", total)
	}

	// Replication's 0.15 is split across the 7 remaining categories in proportion
	// to their pre-redistribution weights (sum 0.85).
	// Categories at 0.15 → 0.15 + 0.15*(0.15/0.85) ≈ 0.1765
	// Categories at 0.10 → 0.10 + 0.15*(0.10/0.85) ≈ 0.1176
	for _, c := range categories {
		switch c.Name {
		case "connections", "performance", "maintenance":
			if c.Weight < 0.175 || c.Weight > 0.178 {
				t.Errorf("%s weight should be ~0.1765, got %v", c.Name, c.Weight)
			}
		case "storage", "horizon", "wal_checkpoint", "locks":
			if c.Weight < 0.117 || c.Weight > 0.119 {
				t.Errorf("%s weight should be ~0.1176, got %v", c.Name, c.Weight)
			}
		}
	}
}
