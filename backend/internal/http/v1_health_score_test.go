package http

import (
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/metrics"
)

// snapshotFixture is the SQL snapshot of one database, deliberately unhealthy
// on every field the datasource might not carry — so a value silently left at
// ToRawMetrics' healthy seed is visible as such in the assertions.
func snapshotFixture() *dto.HealthScoreMetrics {
	return &dto.HealthScoreMetrics{
		Database:             "app_db",
		CacheHitRatio:        82,
		MaxDeadRatio:         60,
		AvgDeadRatio:         35,
		HotUpdateRatio:       0.3,
		MaxXidAge:            1_500_000_000,
		DeadlocksTotal:       7,
		UngrantedLocks:       4,
		ActiveLockWaiters:    3,
		TotalConnections:     90,
		ActiveConnections:    40,
		IdleInTransaction:    12,
		MaxConnections:       100,
		TimedCheckpoints:     20,
		RequestedCheckpoints: 80,
		ReplicaCount:         2,
		MaxReplayLagSeconds:  45,
		MaxLagBytes:          9_000_000,
		DisconnectedReplicas: 1,
	}
}

func TestOverlaySignalGaps_FillsOnlyAbsentSignals(t *testing.T) {
	// A datasource whose postgres-role selector matches nothing: only the two
	// connection signals arrive. Everything else keeps ToRawMetrics' healthy
	// seeds (100% cache hit, zero dead ratio) unless the snapshot fills it —
	// which is the whole point, since matched > 0 means the degraded flag never
	// trips and the gap would otherwise score green.
	sig := metrics.NewSignals(time.Now())
	sig.Set(metrics.SigTotalConns, 5)
	sig.Set(metrics.SigMaxConns, 200)

	m := snapshotFixture()
	raw := sig.ToRawMetrics()

	if raw.CacheHitRatio != 100 {
		t.Fatalf("test sanity: absent cache-hit should be seeded to 100, got %v", raw.CacheHitRatio)
	}

	overlaySignalGaps(&raw, m, sig)

	// Present signals win — the datasource value is the live one.
	if raw.TotalConnections != 5 {
		t.Errorf("TotalConnections: datasource value must survive, got %d", raw.TotalConnections)
	}

	if raw.MaxConnections != 200 {
		t.Errorf("MaxConnections: datasource value must survive, got %d", raw.MaxConnections)
	}

	// Absent signals fall back to the snapshot.
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"CacheHitRatio", raw.CacheHitRatio, 82.0},
		{"MaxDeadRatio", raw.MaxDeadRatio, 60.0},
		{"AvgDeadRatio", raw.AvgDeadRatio, 35.0},
		{"HotUpdateRatio", raw.HotUpdateRatio, 0.3},
		{"MaxXidAge", raw.MaxXidAge, int64(1_500_000_000)},
		{"DeadlocksTotal", raw.DeadlocksTotal, int64(7)},
		{"UngrantedLocks", raw.UngrantedLocks, 4},
		{"ActiveLockWaiters", raw.ActiveLockWaiters, 3},
		{"ActiveConnections", raw.ActiveConnections, 40},
		{"IdleInTransaction", raw.IdleInTransaction, 12},
		{"TimedCheckpoints", raw.TimedCheckpoints, int64(20)},
		{"RequestedCheckpoints", raw.RequestedCheckpoints, int64(80)},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: expected snapshot value %v, got %v", c.name, c.want, c.got)
		}
	}
}

func TestOverlaySignalGaps_MarksBackfilledRulesSnapshotBacked(t *testing.T) {
	// The five instance-wide rules keep their database attribution only when
	// flagged, so the flag has to track the backfill exactly: set for the
	// signals that were missing, clear for the one that arrived.
	sig := metrics.NewSignals(time.Now())
	sig.Set(metrics.SigCacheHitRatio, 99)

	raw := sig.ToRawMetrics()
	overlaySignalGaps(&raw, snapshotFixture(), sig)

	if raw.CacheHitRatio != 99 {
		t.Errorf("CacheHitRatio: datasource value must survive, got %v", raw.CacheHitRatio)
	}

	if raw.SnapshotBackedRules["low_cache_hit_ratio"] {
		t.Error("low_cache_hit_ratio marked snapshot-backed although the signal was present")
	}

	for _, id := range []string{
		"high_max_dead_ratio",
		"high_avg_dead_ratio",
		"low_hot_update_ratio",
		"xid_wraparound_risk",
	} {
		if !raw.SnapshotBackedRules[id] {
			t.Errorf("%s: backfilled from the snapshot but not marked snapshot-backed", id)
		}
	}
}

func TestOverlaySignalGaps_ReplicationIsAllOrNothing(t *testing.T) {
	// ToRawMetrics infers ReplicaCount from either lag signal, so taking the
	// snapshot's replication block whenever one of the two is missing would
	// overwrite a live reading with a stale one.
	t.Run("one lag signal present keeps the datasource view", func(t *testing.T) {
		sig := metrics.NewSignals(time.Now())
		sig.Set(metrics.SigReplLagBytes, 1_000)

		raw := sig.ToRawMetrics()
		overlaySignalGaps(&raw, snapshotFixture(), sig)

		if raw.MaxLagBytes != 1_000 {
			t.Errorf("MaxLagBytes: expected datasource 1000, got %d", raw.MaxLagBytes)
		}

		if raw.ReplicaCount != 1 {
			t.Errorf("ReplicaCount: expected the inferred 1, got %d", raw.ReplicaCount)
		}

		if raw.MaxReplayLagSeconds != 0 {
			t.Errorf("MaxReplayLagSeconds: snapshot must not leak in, got %v", raw.MaxReplayLagSeconds)
		}

		if raw.DisconnectedReplicas != 0 {
			t.Errorf("DisconnectedReplicas: snapshot must not leak in, got %d", raw.DisconnectedReplicas)
		}
	})

	t.Run("neither lag signal falls back to the snapshot", func(t *testing.T) {
		sig := metrics.NewSignals(time.Now())
		sig.Set(metrics.SigTotalConns, 5)

		raw := sig.ToRawMetrics()
		overlaySignalGaps(&raw, snapshotFixture(), sig)

		if raw.ReplicaCount != 2 {
			t.Errorf("ReplicaCount: expected snapshot 2, got %d", raw.ReplicaCount)
		}

		if raw.MaxReplayLagSeconds != 45 {
			t.Errorf("MaxReplayLagSeconds: expected snapshot 45, got %v", raw.MaxReplayLagSeconds)
		}

		if raw.MaxLagBytes != 9_000_000 {
			t.Errorf("MaxLagBytes: expected snapshot 9000000, got %d", raw.MaxLagBytes)
		}

		if raw.DisconnectedReplicas != 1 {
			t.Errorf("DisconnectedReplicas: expected snapshot 1, got %d", raw.DisconnectedReplicas)
		}
	})
}
