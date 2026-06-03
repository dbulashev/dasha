package metrics

import (
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/health"
)

// Empty signals must NOT false-fire the critical floor or the inverted-default
// adjustments (autovacuum/track_counts off, low HOT ratio) — the metrics path
// seeds those to neutral.
func TestScoreFromSignals_EmptyIsHealthy(t *testing.T) {
	r := ScoreFromSignals(NewSignals(time.Now()), health.DefaultWeights())

	if r.Score < 95 {
		t.Errorf("empty signals should score healthy (>=95), got %v — neutralization broken", r.Score)
	}
}

func TestScoreFromSignals_LowCacheHitDropsPerformance(t *testing.T) {
	s := NewSignals(time.Now())
	s.Set(SigCacheHitRatio, 80) // <85 -> performance HIGH tier

	r := ScoreFromSignals(s, health.DefaultWeights())

	var perf float64
	for _, c := range r.Categories {
		if c.Name == "performance" {
			perf = c.Score
		}
	}

	if perf >= 100 {
		t.Errorf("low cache hit should drop performance below 100, got %v", perf)
	}
}

func TestScoreFromSignals_WraparoundFloor(t *testing.T) {
	s := NewSignals(time.Now())
	// Tiny xacts_left -> xid age in the failsafe zone -> floor to red.
	s.Set(SigXactsLeftWrap, 100_000_000)

	r := ScoreFromSignals(s, health.DefaultWeights())

	if r.Score > 30 {
		t.Errorf("near-wraparound should clamp score into the red (<=30), got %v", r.Score)
	}
}

func TestScoreFromSignals_ChecksumFloor(t *testing.T) {
	s := NewSignals(time.Now())
	s.Set(SigChecksumFailRate, 1) // any data-page checksum failure is critical

	if r := ScoreFromSignals(s, health.DefaultWeights()); r.Score > 30 {
		t.Errorf("checksum failures should floor score into the red (<=30), got %v", r.Score)
	}
}

func TestScoreFromSignals_HostAndPoolerSaturation(t *testing.T) {
	conn := func(r health.Result) float64 {
		for _, c := range r.Categories {
			if c.Name == "connections" {
				return c.Score
			}
		}

		return -1
	}

	host := NewSignals(time.Now())
	host.Set(SigLoadAvg15, 8)
	host.Set(SigNumVCPU, 2) // load/vCPU = 4 -> saturated

	if got := conn(ScoreFromSignals(host, health.DefaultWeights())); got >= 100 {
		t.Errorf("host saturation should drop connections below 100, got %v", got)
	}

	pool := NewSignals(time.Now())
	pool.Set(SigPoolerServers, 9)
	pool.Set(SigPoolerPoolSize, 10) // 0.9 saturation

	if got := conn(ScoreFromSignals(pool, health.DefaultWeights())); got >= 100 {
		t.Errorf("pooler saturation should drop connections below 100, got %v", got)
	}
}

func TestScoreFromSignalsBase_LatencyRegression(t *testing.T) {
	perf := func(r health.Result) float64 {
		for _, c := range r.Categories {
			if c.Name == "performance" {
				return c.Score
			}
		}

		return -1
	}

	s := NewSignals(time.Now())
	s.Set(SigLatencyMs, 400) // 4x a 100ms baseline -> regression

	if got := perf(ScoreFromSignalsBase(s, health.DefaultWeights(), 100)); got >= 100 {
		t.Errorf("latency regression should drop performance below 100, got %v", got)
	}

	// No baseline -> latency must not penalize performance.
	if got := perf(ScoreFromSignalsBase(s, health.DefaultWeights(), 0)); got < 100 {
		t.Errorf("without a baseline, latency should be neutral, got %v", got)
	}
}

func TestToRawMetrics_ReplicaInferredFromLag(t *testing.T) {
	s := NewSignals(time.Now())
	s.Set(SigReplLagSeconds, 45)

	m := s.ToRawMetrics()
	if m.ReplicaCount != 1 {
		t.Errorf("replication lag signal should imply ReplicaCount=1, got %d", m.ReplicaCount)
	}

	if m.MaxReplayLagSeconds != 45 {
		t.Errorf("lag seconds not mapped: %v", m.MaxReplayLagSeconds)
	}
}
