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
		age := max(wraparoundXidLimit-int64(v), 0)

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

	if v, ok := s.Get(SigDiskUsedRatio); ok {
		m.DiskUsedRatio = v
	}

	return m
}

// Baselines carries the seasonal baselines folded into the regression penalties.
// A zero field disables that regression (current/baseline can't be formed).
type Baselines struct {
	Latency float64 // seasonal mean query latency
	SeqScan float64 // seasonal seq-scan tuple-read rate
}

// ScoreFromSignals computes the health score from normalized signals using the
// existing engine. The snapshot path (health.Calculate) stays unchanged.
func ScoreFromSignals(s Signals, w health.Weights) health.Result {
	return ScoreFromSignalsBase(s, w, Baselines{})
}

// ScoreFromSignalsBase scores with seasonal baselines for the regression
// penalties (latency, seq-scan). A zero baseline disables its regression.
func ScoreFromSignalsBase(s Signals, w health.Weights, b Baselines) health.Result {
	return health.CalculateWithWeights(rawWithRegression(s, b), w)
}

// rawWithRegression builds RawMetrics and folds in the regression ratios
// (current value / seasonal baseline) for each signal with an available baseline.
func rawWithRegression(s Signals, b Baselines) health.RawMetrics {
	m := s.ToRawMetrics()

	if b.Latency > 0 {
		if lat, ok := s.Get(SigLatencyMs); ok && lat > 0 {
			m.LatencyRegressionRatio = lat / b.Latency
		}
	}

	if b.SeqScan > 0 {
		if v, ok := s.Get(SigSeqScanRate); ok && v > 0 {
			m.SeqScanRegressionRatio = v / b.SeqScan
		}
	}

	return m
}
