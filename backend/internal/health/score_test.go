package health

import (
	"testing"
)

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
	}

	r := Calculate(m)
	if r.Score < 95 {
		t.Errorf("healthy database should score >= 95, got %v", r.Score)
	}
	if !r.HasReplication {
		t.Error("expected HasReplication = true")
	}
	if len(r.Categories) != 5 {
		t.Errorf("expected 5 categories, got %d", len(r.Categories))
	}
}

func TestCalculate_CriticalDatabase(t *testing.T) {
	m := RawMetrics{
		TotalConnections:          95,
		ActiveConnections:         80,
		IdleInTransaction:         10,
		LongestTransactionSeconds: 600,
		MaxConnections:            100,
		CacheHitRatio:             85,
		MaxDeadRatio:              60,
		AvgDeadRatio:              25,
		TablesHighBloat:           10,
		ReplicaCount:              2,
		MaxReplayLagSeconds:       60,
		MaxLagBytes:               500 * 1024 * 1024,
		DisconnectedReplicas:      1,
		MaxXidAge:                 1_600_000_000,
		MaxVacuumAgeHours:         300,
		TablesNeverVacuumed:       5,
	}

	r := Calculate(m)
	if r.Score > 40 {
		t.Errorf("critical database should score < 40, got %v", r.Score)
	}
}

func TestCalculate_NoReplication(t *testing.T) {
	m := RawMetrics{
		TotalConnections: 10,
		MaxConnections:   100,
		CacheHitRatio:    99.5,
		ReplicaCount:     0,
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

func TestPenaltyMaintenance_StaleVacuum(t *testing.T) {
	m := RawMetrics{MaxVacuumAgeHours: 240} // 10 days
	r := penaltyMaintenance(m)
	if r.Penalty < 10 {
		t.Errorf("expected penalty >= 10 for 10-day vacuum age, got %v", r.Penalty)
	}
}

func TestPenaltyConnections_ZeroMaxConnections(t *testing.T) {
	m := RawMetrics{MaxConnections: 0}
	r := penaltyConnections(m)
	if r.Penalty != 0 {
		t.Errorf("expected 0 penalty when max_connections is 0, got %v", r.Penalty)
	}
}

func TestRedistributeWeights(t *testing.T) {
	categories := []CategoryResult{
		{Name: "connections", Weight: 0.20},
		{Name: "performance", Weight: 0.25},
		{Name: "storage", Weight: 0.20},
		{Name: "replication", Weight: 0.15},
		{Name: "maintenance", Weight: 0.20},
	}

	redistributeWeights(categories)

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

	// Check proportional redistribution
	for _, c := range categories {
		switch c.Name {
		case "connections":
			if c.Weight < 0.235 || c.Weight > 0.236 {
				t.Errorf("connections weight should be ~0.2353, got %v", c.Weight)
			}
		case "performance":
			if c.Weight < 0.294 || c.Weight > 0.295 {
				t.Errorf("performance weight should be ~0.2941, got %v", c.Weight)
			}
		}
	}
}
