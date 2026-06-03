package metrics

import "github.com/dbulashev/dasha/internal/health"

// wraparoundXidLimit approximates the xid age at which PostgreSQL forces a
// shutdown; used to convert the `xacts_left_before_wraparound` countdown back
// into an xid age for the maintenance penalty/floor (~2^31 minus PG's margin).
const wraparoundXidLimit int64 = 2_146_483_648

// ToRawMetrics maps normalized signals onto the score engine's input.
//
// Only cleanly-equivalent signals are mapped here; richer/inverse signals
// (latency regression, host/pooler saturation, exhaustion) are layered in a
// later phase. Inputs whose "healthy" value is non-zero (enabled GUCs, high HOT
// ratio) are seeded to neutral so the snapshot-only adjustments and the critical
// floor do not false-fire when the metrics path does not (yet) carry them.
func (s Signals) ToRawMetrics() health.RawMetrics {
	m := health.RawMetrics{
		CacheHitRatio:        100, // absent cache-hit must read healthy, not "0% hit"
		AutovacuumEnabled:    true,
		TrackCountsEnabled:   true,
		TrackIoTimingEnabled: true,
		HotUpdateRatio:       1.0,
	}

	if v, ok := s.Get(SigCacheHitRatio); ok {
		m.CacheHitRatio = v
	}

	if v, ok := s.Get(SigMaxDeadRatio); ok {
		m.MaxDeadRatio = v
	}

	if v, ok := s.Get(SigAvgDeadRatio); ok {
		m.AvgDeadRatio = v
	}

	if v, ok := s.Get(SigHotUpdateRatio); ok {
		m.HotUpdateRatio = v
	}

	if v, ok := s.Get(SigDeadlocksTotal); ok {
		m.DeadlocksTotal = int64(v)
	}

	if v, ok := s.Get(SigMaxVacuumAgeH); ok {
		m.MaxVacuumAgeHours = v
	}

	if v, ok := s.Get(SigReplLagSeconds); ok {
		m.MaxReplayLagSeconds = v
		m.ReplicaCount = 1 // presence of replication lag implies a replica
	}

	if v, ok := s.Get(SigReplLagBytes); ok {
		m.MaxLagBytes = int64(v)
		m.ReplicaCount = 1
	}

	if v, ok := s.Get(SigLocksNotGranted); ok {
		m.UngrantedLocks = int(v)
	}

	if v, ok := s.Get(SigTotalConns); ok {
		m.TotalConnections = int(v)
	}

	if v, ok := s.Get(SigActiveConns); ok {
		m.ActiveConnections = int(v)
	}

	if v, ok := s.Get(SigIdleInTx); ok {
		m.IdleInTransaction = int(v)
	}

	if v, ok := s.Get(SigMaxConns); ok {
		m.MaxConnections = int(v)
	}

	if v, ok := s.Get(SigXactsLeftWrap); ok {
		age := wraparoundXidLimit - int64(v)
		if age < 0 {
			age = 0
		}

		m.MaxXidAge = age
	}

	if v, ok := s.Get(SigLoadAvg15); ok {
		m.LoadAvg15 = v
	}

	if v, ok := s.Get(SigNumVCPU); ok {
		m.NumVCPU = v
	}

	if v, ok := s.Get(SigPoolerServers); ok {
		m.PoolerServerConns = v
	}

	if v, ok := s.Get(SigPoolerPoolSize); ok {
		m.PoolerPoolSize = v
	}

	if v, ok := s.Get(SigChecksumFailRate); ok {
		m.ChecksumFailuresRate = v
	}

	if v, ok := s.Get(SigTimedCheckpoints); ok {
		m.TimedCheckpoints = int64(v)
	}

	if v, ok := s.Get(SigRequestedCheckpoints); ok {
		m.RequestedCheckpoints = int64(v)
	}

	if v, ok := s.Get(SigActiveLockWaiters); ok {
		m.ActiveLockWaiters = int(v)
	}

	if v, ok := s.Get(SigSeqExhaustionMax); ok {
		m.SequenceExhaustionMax = v
	}

	return m
}

// ScoreFromSignals computes the health score from normalized signals using the
// existing engine. The snapshot path (health.Calculate) stays unchanged.
func ScoreFromSignals(s Signals, w health.Weights) health.Result {
	return ScoreFromSignalsBase(s, w, 0)
}

// ScoreFromSignalsBase scores with a seasonal latency baseline for the
// latency-regression penalty. latencyBaseline <= 0 disables regression.
func ScoreFromSignalsBase(s Signals, w health.Weights, latencyBaseline float64) health.Result {
	return health.CalculateWithWeights(rawWithRegression(s, latencyBaseline), w)
}

// rawWithRegression builds RawMetrics and folds in the latency-regression ratio
// (current latency / seasonal baseline) when a baseline is available.
func rawWithRegression(s Signals, latencyBaseline float64) health.RawMetrics {
	m := s.ToRawMetrics()

	if latencyBaseline > 0 {
		if lat, ok := s.Get(SigLatencyMs); ok && lat > 0 {
			m.LatencyRegressionRatio = lat / latencyBaseline
		}
	}

	return m
}
