package metrics

import "time"

// SignalKind is a provider-agnostic, normalized metric used by the score engine.
type SignalKind string

const (
	// performance
	SigLatencyMs     SignalKind = "latency_ms"        // calls-weighted mean | pooler p95
	SigIOReadTime    SignalKind = "io_read_time_rate" // pg_stat_io (PG16+)
	SigIOWriteTime   SignalKind = "io_write_time_rate"
	SigCacheHitRatio SignalKind = "cache_hit_ratio" // secondary

	// connections / saturation
	SigLoadAvg15      SignalKind = "load_avg_15"
	SigNumVCPU        SignalKind = "num_vcpu"
	SigTotalConns     SignalKind = "total_connections"
	SigActiveConns    SignalKind = "active_connections"
	SigIdleInTx       SignalKind = "idle_in_transaction"
	SigMaxConns       SignalKind = "max_connections"
	SigPoolerClients  SignalKind = "pooler_client_conns"
	SigPoolerServers  SignalKind = "pooler_server_conns"
	SigPoolerPoolSize SignalKind = "pooler_pool_size"

	// storage
	SigMaxBloatRatio  SignalKind = "max_bloat_ratio" // optional custom collector
	SigMaxDeadRatio   SignalKind = "max_dead_ratio"
	SigAvgDeadRatio   SignalKind = "avg_dead_ratio"
	SigHotUpdateRatio SignalKind = "hot_update_ratio"
	SigDeadlocksTotal SignalKind = "deadlocks_total"

	// maintenance
	SigXactsLeftWrap SignalKind = "xacts_left_before_wraparound"
	SigMaxVacuumAgeH SignalKind = "max_vacuum_age_hours"

	// replication / wal / locks
	SigReplLagBytes         SignalKind = "repl_lag_bytes"
	SigReplLagSeconds       SignalKind = "repl_lag_seconds"
	SigCheckpointsReqRate   SignalKind = "checkpoints_req_rate"
	SigCheckpointsAllRate   SignalKind = "checkpoints_all_rate"
	SigTimedCheckpoints     SignalKind = "timed_checkpoints"
	SigRequestedCheckpoints SignalKind = "requested_checkpoints"
	SigLocksNotGranted      SignalKind = "locks_not_granted"
	SigActiveLockWaiters    SignalKind = "active_lock_waiters"

	// data integrity / capacity (often critical)
	SigChecksumFailRate  SignalKind = "checksum_failures_rate"  // optional; floor candidate
	SigSeqExhaustionMax  SignalKind = "sequence_exhaustion_max" // optional collector
	SigTypeExhaustionMax SignalKind = "datatype_exhaustion_max" // optional collector
)

// Signals holds normalized signal values at a single point in time. A zero
// value is a valid reading, so presence is tracked separately in Have — an
// absent signal (provider does not expose it) must stay neutral in the score,
// the same way track_io stays silent on PG < 16.
type Signals struct {
	At    time.Time
	Value map[SignalKind]float64
	Have  map[SignalKind]bool
}

// NewSignals returns an empty, ready-to-fill Signals for time t.
func NewSignals(t time.Time) Signals {
	return Signals{
		At:    t,
		Value: make(map[SignalKind]float64),
		Have:  make(map[SignalKind]bool),
	}
}

// Set records a value for a signal and marks it present.
func (s Signals) Set(k SignalKind, v float64) {
	s.Value[k] = v
	s.Have[k] = true
}

// Get returns the value and whether the signal is present.
func (s Signals) Get(k SignalKind) (float64, bool) {
	v, ok := s.Value[k]
	if !ok {
		return 0, false
	}

	return v, s.Have[k]
}
