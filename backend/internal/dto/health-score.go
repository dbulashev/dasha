package dto

// HealthScoreXidWraparoundDatabase is one row of the per-database XID age
// detail for the xid_wraparound_risk recommendation.
type HealthScoreXidWraparoundDatabase struct {
	Database string
	XidAge   int64
}

// HealthScoreTableReloption is one row of a per-table list keyed by schema
// and table with the matching reloptions string. Used by the
// tables_with_autovacuum_off detail.
type HealthScoreTableReloption struct {
	Schema     string
	Table      string
	RelOptions string
}

// HealthScoreLowHotUpdateTable is one row of the low-HOT-update detail.
type HealthScoreLowHotUpdateTable struct {
	Schema     string
	Table      string
	Updates    int64
	HotUpdates int64
	HotRatio   float64
}

// HealthScoreHighDeadRatioTable is one row of the top tables by dead-tuple
// ratio detail for the high_max_dead_ratio recommendation.
type HealthScoreHighDeadRatioTable struct {
	Schema     string
	Table      string
	LiveTuples int64
	DeadTuples int64
	DeadRatio  float64
}

// HealthScoreHorizonBlockingSession is one row of the horizon-blocking
// sessions detail (top sessions by oldest backend_xmin).
type HealthScoreHorizonBlockingSession struct {
	PID                 int32
	Username            string
	State               string
	WaitEventType       string
	WaitEvent           string
	XactDurationSeconds float64
	BackendXmin         string
	Query               string
}

// HealthScoreDatabaseMetrics holds raw per-database metrics applicable on the
// per-DB level: Performance / Storage / Maintenance.
// Connections and Replication are instance-wide and not collected here.
type HealthScoreDatabaseMetrics struct {
	Database  string
	SizeBytes int64

	// Performance
	CacheHitRatio float64

	// Storage
	MaxDeadRatio    float64
	AvgDeadRatio    float64
	TablesHighBloat int

	// Maintenance
	MaxXidAge                int64
	VacuumBacklogTables      int
	MaxOverdueVacuumAgeHours float64
	TablesNeverVacuumed      int
}

type HealthScoreMetrics struct {
	// InRecovery is true when pg_is_in_recovery() returns true.
	// Standbys cannot run autovacuum/ANALYZE, so the maintenance category
	// is dropped (weight redistributed) when this is set.
	InRecovery bool

	// Connections
	TotalConnections          int
	ActiveConnections         int
	IdleInTransaction         int
	LongestTransactionSeconds float64
	MaxConnections            int

	// Performance
	CacheHitRatio        float64
	TrackIoTimingEnabled bool

	// Storage
	MaxDeadRatio    float64
	AvgDeadRatio    float64
	TablesHighBloat int

	// Replication
	ReplicaCount         int
	MaxReplayLagSeconds  float64
	MaxLagBytes          int64
	DisconnectedReplicas int

	// Maintenance
	MaxXidAge                int64
	VacuumBacklogTables      int
	MaxOverdueVacuumAgeHours float64
	TablesNeverVacuumed      int
	AutovacuumEnabled        bool
	TrackCountsEnabled       bool
	TablesWithAutovacuumOff  int
	MaxRelfrozenxidAge       int64

	// Horizon
	HorizonLagXids int64

	// WAL & Checkpoint
	TimedCheckpoints     int64
	RequestedCheckpoints int64

	// Locks
	ActiveLockWaiters      int
	LongestLockWaitSeconds float64
	UngrantedLocks         int
	DeadlocksTotal         int64
	HeavyweightLocksTotal  int
	MaxLocksPerTransaction int

	// HOT updates / planner stats / WAL level (P3 minor rules)
	HotUpdateRatio          float64
	NewpageUpdateRatio      float64
	StalePlannerStatsTables int
	WalLevel                string
	LogicalSlotsActive      int
}
