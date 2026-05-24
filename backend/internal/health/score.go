package health

import "math"

// RawMetrics contains raw metrics collected from PostgreSQL for health score calculation.
type RawMetrics struct {
	// Connections
	TotalConnections          int
	ActiveConnections         int
	IdleInTransaction         int
	LongestTransactionSeconds float64
	MaxConnections            int

	// Performance
	CacheHitRatio float64

	// Storage
	MaxDeadRatio    float64
	AvgDeadRatio    float64
	TablesHighBloat int

	// Replication
	ReplicaCount          int
	MaxReplayLagSeconds   float64
	MaxLagBytes           int64
	DisconnectedReplicas  int

	// Maintenance
	MaxXidAge           int64
	MaxVacuumAgeHours   float64
	TablesNeverVacuumed int
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
}

const (
	weightConnections  = 0.20
	weightPerformance  = 0.25
	weightStorage      = 0.20
	weightReplication  = 0.15
	weightMaintenance  = 0.20
)

// Calculate computes the health score from raw metrics using default weights.
func Calculate(m RawMetrics) Result {
	return CalculateWithWeights(m, DefaultWeights())
}

// CalculateWithWeights computes the health score using the provided per-category
// weights. Weights are normalized before applying; if no replication is present,
// the replication weight is redistributed proportionally across remaining categories.
func CalculateWithWeights(m RawMetrics, w Weights) Result {
	hasReplication := m.ReplicaCount > 0
	w = w.Normalize()

	categories := []CategoryResult{
		penaltyConnections(m),
		penaltyPerformance(m),
		penaltyStorage(m),
		penaltyReplication(m),
		penaltyMaintenance(m),
	}

	for i := range categories {
		categories[i].Weight = w.byCategory(categories[i].Name)
	}

	if !hasReplication {
		redistributeWeights(categories)
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
	}
}

func redistributeWeights(categories []CategoryResult) {
	var replWeight float64
	var otherWeightSum float64

	for _, c := range categories {
		if c.Name == "replication" {
			replWeight = c.Weight
		} else {
			otherWeightSum += c.Weight
		}
	}

	if replWeight == 0 || otherWeightSum == 0 {
		return
	}

	for i := range categories {
		if categories[i].Name == "replication" {
			categories[i].Weight = 0
			categories[i].Penalty = 0
		} else {
			categories[i].Weight += replWeight * (categories[i].Weight / otherWeightSum)
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

	switch {
	case m.CacheHitRatio >= 99:
		penalty = 0
	case m.CacheHitRatio >= 95:
		penalty = (99 - m.CacheHitRatio) / 4 * 20
	case m.CacheHitRatio >= 90:
		penalty = 20 + (95-m.CacheHitRatio)/5*30
	default:
		penalty = 50 + (90-m.CacheHitRatio)/10*50
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

	switch {
	case m.MaxDeadRatio > 50:
		penalty = 40
	case m.MaxDeadRatio > 20:
		penalty = 10 + (m.MaxDeadRatio-20)/30*30
	default:
		penalty = m.MaxDeadRatio / 20 * 10
	}

	if m.AvgDeadRatio > 10 {
		penalty += math.Min(m.AvgDeadRatio*2, 30)
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

	if m.MaxVacuumAgeHours > 168 { // 7 days
		penalty += math.Min((m.MaxVacuumAgeHours-168)/24*5, 30)
	} else if m.MaxVacuumAgeHours > 48 {
		penalty += (m.MaxVacuumAgeHours - 48) / 120 * 10
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
