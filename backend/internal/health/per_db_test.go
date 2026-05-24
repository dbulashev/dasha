package health

import (
	"math"
	"testing"
)

func TestComputePerDB_OnlyApplicableCategories(t *testing.T) {
	scores := ComputePerDB([]PerDBMetrics{
		{Database: "app", SizeBytes: 1 << 30, CacheHitRatio: 99.9},
	}, DefaultWeights())

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
	}, DefaultWeights())

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
	}, DefaultWeights())

	if scores[0].Score > 80 {
		t.Errorf("bloated DB should score < 80, got %v", scores[0].Score)
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
				{Name: "performance", Score: 100},
			},
		},
		{
			Database:  "huge",
			SizeBytes: 10 << 30,
			Categories: []CategoryResult{
				{Name: "performance", Score: 50},
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
		{Database: "a", SizeBytes: 1 << 30, Categories: []CategoryResult{{Name: "performance", Score: 80}}},
		{Database: "b", SizeBytes: 1 << 30, Categories: []CategoryResult{{Name: "performance", Score: 60}}},
	}

	rollup := PerDBCategoryRollup(scores)

	if math.Abs(rollup["performance"]-70) > 0.5 {
		t.Errorf("equal-size databases → mean 70, got %v", rollup["performance"])
	}
}

func TestPerDBCategoryRollup_ZeroSizeNotDropped(t *testing.T) {
	scores := []DatabaseScore{
		{Database: "newly_created", SizeBytes: 0, Categories: []CategoryResult{{Name: "performance", Score: 100}}},
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
		Connections: 0.30, // dropped
		Performance: 0.20,
		Storage:     0.20,
		Replication: 0.10, // dropped
		Maintenance: 0.20,
	}

	w := perDBWeights(in)

	if w.Connections != 0 || w.Replication != 0 {
		t.Errorf("connections/replication must be zero in per-DB weights, got %+v", w)
	}

	if math.Abs(w.Sum()-1.0) > 1e-9 {
		t.Errorf("per-DB weights must be normalized to 1.0, got sum=%v", w.Sum())
	}
}
