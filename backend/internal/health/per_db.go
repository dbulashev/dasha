package health

import "math"

// PerDBMetrics is a per-database raw metric input for health score computation.
// Only Performance / Storage / Maintenance categories make sense at the database
// level; Connections and Replication remain instance-wide.
type PerDBMetrics struct {
	Database  string
	SizeBytes int64

	CacheHitRatio float64

	MaxDeadRatio    float64
	AvgDeadRatio    float64
	TablesHighBloat int

	MaxXidAge           int64
	MaxVacuumAgeHours   float64
	TablesNeverVacuumed int
}

// DatabaseScore is the computed score for a single database.
type DatabaseScore struct {
	Database   string           `json:"database"`
	SizeBytes  int64            `json:"size_bytes"`
	Score      float64          `json:"score"`
	Categories []CategoryResult `json:"categories"`
}

// PerDBApplicableCategories lists categories computed at the per-database level.
// Connections and Replication are excluded.
var PerDBApplicableCategories = []string{"performance", "storage", "maintenance"}

// ComputePerDB calculates per-database scores for the supplied metrics.
// Weights are restricted to applicable categories and renormalized so that
// each per-DB score remains in 0–100 regardless of the absolute weight values.
//
// When inRecovery is true, maintenance is dropped from each per-DB score the
// same way it is dropped at the instance level (CalculateWithWeights): on a
// standby autovacuum / ANALYZE do not run, so the per-DB maintenance
// metrics would only mislead. Its weight is redistributed across the
// remaining applicable categories.
func ComputePerDB(metrics []PerDBMetrics, w Weights, inRecovery bool) []DatabaseScore {
	w = perDBWeights(w, inRecovery)

	result := make([]DatabaseScore, 0, len(metrics))

	for _, m := range metrics {
		raw := RawMetrics{
			CacheHitRatio:       m.CacheHitRatio,
			MaxDeadRatio:        m.MaxDeadRatio,
			AvgDeadRatio:        m.AvgDeadRatio,
			TablesHighBloat:     m.TablesHighBloat,
			MaxXidAge:           m.MaxXidAge,
			MaxVacuumAgeHours:   m.MaxVacuumAgeHours,
			TablesNeverVacuumed: m.TablesNeverVacuumed,
		}

		cats := []CategoryResult{
			penaltyPerformance(raw),
			penaltyStorage(raw),
			penaltyMaintenance(raw),
		}

		if inRecovery {
			// Zero maintenance penalty so the standby never gets dinged
			// for primary-side work it cannot perform.
			for i := range cats {
				if cats[i].Name == "maintenance" {
					cats[i].Penalty = 0
				}
			}
		}

		totalPenalty := 0.0

		for i := range cats {
			cats[i].Weight = w.byCategory(cats[i].Name)
			totalPenalty += cats[i].Penalty * cats[i].Weight
			cats[i].Score = 100 - cats[i].Penalty
		}

		score := math.Max(0, math.Min(100, 100-totalPenalty))

		// Imminent per-database wraparound clamps the score into the red, the same
		// way the instance aggregate does. Skipped on a standby, where maintenance
		// (and with it the xid signal) is dropped — the inherited horizon is the
		// primary's to fix.
		if !inRecovery {
			if c := criticalXidCeiling(m.MaxXidAge); c < score {
				score = c
			}
		}

		result = append(result, DatabaseScore{
			Database:   m.Database,
			SizeBytes:  m.SizeBytes,
			Score:      math.Round(score*10) / 10,
			Categories: cats,
		})
	}

	return result
}

// PerDBCategoryRollup returns the size-weighted mean per-category score across
// all databases. The result is keyed by category name and contains the rollup
// score (0–100) for use by the instance-level aggregate. Categories dropped for
// a database (Weight == 0, e.g. maintenance on a standby) are excluded, so a
// zero-weight Score=100 does not leak into the mean as a misleading green.
func PerDBCategoryRollup(scores []DatabaseScore) map[string]float64 {
	if len(scores) == 0 {
		return nil
	}

	type acc struct {
		weighted float64
		size     int64
	}

	by := make(map[string]*acc, len(PerDBApplicableCategories))

	for _, ds := range scores {
		size := ds.SizeBytes
		if size <= 0 {
			size = 1 // avoid zero-weight DBs being dropped entirely
		}

		for _, c := range ds.Categories {
			if c.Weight == 0 {
				continue // dropped category — don't roll its placeholder Score=100 in
			}

			a, ok := by[c.Name]
			if !ok {
				a = &acc{}
				by[c.Name] = a
			}

			a.weighted += c.Score * float64(size)
			a.size += size
		}
	}

	out := make(map[string]float64, len(by))

	for name, a := range by {
		if a.size == 0 {
			continue
		}

		out[name] = math.Round(a.weighted/float64(a.size)*10) / 10
	}

	return out
}

// WorstDatabase returns the database with the lowest overall score, or
// (nil) if no databases were evaluated.
func WorstDatabase(scores []DatabaseScore) *DatabaseScore {
	if len(scores) == 0 {
		return nil
	}

	worst := scores[0]

	for i := 1; i < len(scores); i++ {
		if scores[i].Score < worst.Score {
			worst = scores[i]
		}
	}

	return &worst
}

// perDBWeights extracts and renormalizes only the per-DB-applicable categories
// (Performance, Storage, Maintenance) so they sum to 1.0 within the per-DB
// score computation. Instance-only weights (Connections, Replication, Horizon,
// WalCheckpoint, Locks) from the input Weights are dropped — they have no
// per-DB meaning.
//
// When the caller zeroed all applicable categories, we fall back to the
// defaults projected onto the same applicable set, not to the full default
// vector — otherwise the per-DB score would silently start counting
// instance-only categories.
func perDBWeights(w Weights, inRecovery bool) Weights {
	out := Weights{
		Performance: w.Performance,
		Storage:     w.Storage,
		Maintenance: w.Maintenance,
	}

	if inRecovery {
		// Drop maintenance so its share gets redistributed by Normalize
		// across performance + storage only.
		out.Maintenance = 0
	}

	// Fallback runs AFTER the recovery drop so a caller whose only non-zero
	// applicable weight was maintenance does not collapse to an all-zero
	// vector (Normalize would then return the receiver unchanged and every
	// per-DB score would silently become 100). On a standby we project
	// defaults only onto performance + storage; maintenance stays zero.
	if out.Sum() <= 0 {
		def := DefaultWeights()
		out.Performance = def.Performance
		out.Storage = def.Storage

		if !inRecovery {
			out.Maintenance = def.Maintenance
		}
	}

	return out.Normalize()
}
