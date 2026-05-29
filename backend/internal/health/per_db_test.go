package health

import (
	"math"
	"testing"
)

func TestComputePerDB_OnlyApplicableCategories(t *testing.T) {
	scores := ComputePerDB([]PerDBMetrics{
		{Database: "app", SizeBytes: 1 << 30, CacheHitRatio: 99.9},
	}, DefaultWeights(), false)

	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}

	names := make(map[string]bool, len(scores[0].Categories))
	for _, c := range scores[0].Categories {
		names[c.Name] = true
	}

	for _, expected := range PerDBApplicableCategories {
		if !names[expected] {
			t.Errorf("missing applicable category %q in per-DB result", expected)
		}
	}

	for forbidden := range map[string]struct{}{"connections": {}, "replication": {}} {
		if names[forbidden] {
			t.Errorf("category %q must not appear in per-DB result", forbidden)
		}
	}
}

func TestComputePerDB_HealthyScoresHigh(t *testing.T) {
	scores := ComputePerDB([]PerDBMetrics{
		{
			Database:      "healthy",
			SizeBytes:     1 << 30,
			CacheHitRatio: 99.9,
			MaxDeadRatio:  1,
			AvgDeadRatio:  0.5,
			MaxXidAge:     50_000_000,
		},
	}, DefaultWeights(), false)

	if scores[0].Score < 95 {
		t.Errorf("healthy DB should score ≥ 95, got %v", scores[0].Score)
	}
}

func TestComputePerDB_BadStorageDrops(t *testing.T) {
	scores := ComputePerDB([]PerDBMetrics{
		{
			Database:        "bloated",
			SizeBytes:       1 << 30,
			CacheHitRatio:   99.9,
			MaxDeadRatio:    60,
			AvgDeadRatio:    35,
			TablesHighBloat: 25,
		},
	}, DefaultWeights(), false)

	if scores[0].Score > 80 {
		t.Errorf("bloated DB should score < 80, got %v", scores[0].Score)
	}
}

func TestComputePerDB_XidWraparoundFloor(t *testing.T) {
	// A single database in the wraparound failsafe zone must show red, even with
	// every other per-DB signal healthy.
	m := PerDBMetrics{
		Database: "danger", SizeBytes: 1 << 30,
		CacheHitRatio: 99.9, MaxDeadRatio: 1, AvgDeadRatio: 0.5,
		MaxXidAge: xidFailsafeAge,
	}

	primary := ComputePerDB([]PerDBMetrics{m}, DefaultWeights(), false)[0]
	if primary.Score > criticalScoreCeiling {
		t.Errorf("per-DB at failsafe xid age should be clamped to <= %v, got %v", criticalScoreCeiling, primary.Score)
	}

	// On a standby the maintenance signal (incl. xid) is dropped, so no floor —
	// the inherited horizon is the primary's to fix.
	standby := ComputePerDB([]PerDBMetrics{m}, DefaultWeights(), true)[0]
	if standby.Score <= criticalScoreCeiling {
		t.Errorf("standby per-DB must not be floored on inherited xid age, got %v", standby.Score)
	}
}

func TestPerDBCategoryRollup_WeightedBySize(t *testing.T) {
	// Two databases:
	// - tiny (1 MB): performance score 100
	// - huge (10 GB): performance score 50
	// Weighted-mean should heavily favor huge → ~50.
	scores := []DatabaseScore{
		{
			Database:  "tiny",
			SizeBytes: 1 << 20,
			Categories: []CategoryResult{
				{Name: "performance", Weight: 1, Score: 100},
			},
		},
		{
			Database:  "huge",
			SizeBytes: 10 << 30,
			Categories: []CategoryResult{
				{Name: "performance", Weight: 1, Score: 50},
			},
		},
	}

	rollup := PerDBCategoryRollup(scores)

	got := rollup["performance"]
	if got > 51 || got < 49 {
		t.Errorf("rollup performance should be ~50 (dominated by huge DB), got %v", got)
	}
}

func TestPerDBCategoryRollup_PureMean_EqualSize(t *testing.T) {
	scores := []DatabaseScore{
		{Database: "a", SizeBytes: 1 << 30, Categories: []CategoryResult{{Name: "performance", Weight: 1, Score: 80}}},
		{Database: "b", SizeBytes: 1 << 30, Categories: []CategoryResult{{Name: "performance", Weight: 1, Score: 60}}},
	}

	rollup := PerDBCategoryRollup(scores)

	if math.Abs(rollup["performance"]-70) > 0.5 {
		t.Errorf("equal-size databases → mean 70, got %v", rollup["performance"])
	}
}

func TestPerDBCategoryRollup_ZeroSizeNotDropped(t *testing.T) {
	scores := []DatabaseScore{
		{Database: "newly_created", SizeBytes: 0, Categories: []CategoryResult{{Name: "performance", Weight: 1, Score: 100}}},
	}

	rollup := PerDBCategoryRollup(scores)
	if rollup["performance"] != 100 {
		t.Errorf("zero-size DB should still be counted, got %v", rollup["performance"])
	}
}

func TestPerDBCategoryRollup_Empty(t *testing.T) {
	if r := PerDBCategoryRollup(nil); r != nil {
		t.Errorf("empty input should yield nil, got %v", r)
	}
}

func TestPerDBCategoryRollup_SkipsDroppedCategory(t *testing.T) {
	// A dropped category (Weight == 0, e.g. maintenance on a standby) carries a
	// placeholder Score=100 — it must not leak into the rollup as a green entry.
	scores := []DatabaseScore{
		{
			Database:  "standby-db",
			SizeBytes: 1 << 30,
			Categories: []CategoryResult{
				{Name: "performance", Weight: 0.5, Score: 60},
				{Name: "maintenance", Weight: 0, Score: 100},
			},
		},
	}

	rollup := PerDBCategoryRollup(scores)

	if _, ok := rollup["maintenance"]; ok {
		t.Errorf("dropped maintenance (Weight 0) must be excluded, got %v", rollup["maintenance"])
	}

	if rollup["performance"] != 60 {
		t.Errorf("active category should still roll up, got %v", rollup["performance"])
	}
}

func TestWorstDatabase(t *testing.T) {
	if WorstDatabase(nil) != nil {
		t.Error("nil → nil")
	}

	scores := []DatabaseScore{
		{Database: "good", Score: 95},
		{Database: "bad", Score: 40},
		{Database: "mid", Score: 70},
	}

	worst := WorstDatabase(scores)
	if worst == nil || worst.Database != "bad" {
		t.Errorf("worst should be \"bad\", got %+v", worst)
	}
}

func TestPerDBWeights_DropsInapplicable(t *testing.T) {
	in := Weights{
		Connections:   0.30, // dropped
		Performance:   0.20,
		Storage:       0.20,
		Replication:   0.10, // dropped
		Maintenance:   0.20,
		Horizon:       0.05, // dropped
		WalCheckpoint: 0.05, // dropped
		Locks:         0.05, // dropped
	}

	w := perDBWeights(in, false)

	for _, ic := range []string{"connections", "replication", "horizon", "wal_checkpoint", "locks"} {
		if w.byCategory(ic) != 0 {
			t.Errorf("instance-only category %q must be zero, got %v", ic, w.byCategory(ic))
		}
	}

	if math.Abs(w.Sum()-1.0) > 1e-9 {
		t.Errorf("per-DB weights must be normalized to 1.0, got sum=%v", w.Sum())
	}
}

func TestPerDBWeights_FallbackKeepsApplicableOnly(t *testing.T) {
	// User zeroed all per-DB-applicable categories — we must fall back to
	// defaults projected on the same applicable set, NOT to the full default
	// vector (which would silently leak instance-only categories into the
	// per-DB score).
	in := Weights{
		Connections:   0.50,
		Performance:   0,
		Storage:       0,
		Replication:   0.30,
		Maintenance:   0,
		Horizon:       0.10,
		WalCheckpoint: 0.05,
		Locks:         0.05,
	}

	w := perDBWeights(in, false)

	for _, ic := range []string{"connections", "replication", "horizon", "wal_checkpoint", "locks"} {
		if w.byCategory(ic) != 0 {
			t.Errorf("instance-only category %q must stay zero in fallback, got %v", ic, w.byCategory(ic))
		}
	}

	if math.Abs(w.Sum()-1.0) > 1e-9 {
		t.Errorf("fallback weights must be normalized to 1.0, got sum=%v", w.Sum())
	}

	// All three applicable categories should be > 0 (defaults are non-zero).
	if w.Performance <= 0 || w.Storage <= 0 || w.Maintenance <= 0 {
		t.Errorf("fallback must populate all applicable categories, got %+v", w)
	}
}

func TestComputePerDB_InRecoveryDropsMaintenance(t *testing.T) {
	// Catastrophic maintenance metrics on a standby must not pull the score
	// down — autovacuum can't act on a replica, so we mirror the instance-level
	// drop behaviour. Maintenance weight is redistributed onto performance +
	// storage by perDBWeights().
	m := PerDBMetrics{
		Database:            "replica-db",
		SizeBytes:           1 << 30,
		CacheHitRatio:       99.9,
		MaxDeadRatio:        1,
		AvgDeadRatio:        0.5,
		MaxXidAge:           1_900_000_000, // near wraparound
		MaxVacuumAgeHours:   10_000,
		TablesNeverVacuumed: 5,
	}

	primary := ComputePerDB([]PerDBMetrics{m}, DefaultWeights(), false)[0]
	standby := ComputePerDB([]PerDBMetrics{m}, DefaultWeights(), true)[0]

	if primary.Score >= 80 {
		t.Fatalf("test sanity: primary should score < 80 with bad maintenance, got %v", primary.Score)
	}

	if standby.Score < 95 {
		t.Errorf("standby with bad maintenance should still score ≥ 95 (maintenance dropped), got %v", standby.Score)
	}

	for _, c := range standby.Categories {
		if c.Name == "maintenance" {
			if c.Penalty != 0 {
				t.Errorf("standby maintenance penalty must be 0, got %v", c.Penalty)
			}
			if c.Weight != 0 {
				t.Errorf("standby maintenance weight must be 0, got %v", c.Weight)
			}
		}
	}
}
