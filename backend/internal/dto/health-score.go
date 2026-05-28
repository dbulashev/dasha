package dto

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
	MaxXidAge           int64
	MaxVacuumAgeHours   float64
	TablesNeverVacuumed int
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
	MaxXidAge               int64
	MaxVacuumAgeHours       float64
	TablesNeverVacuumed     int
	AutovacuumEnabled       bool
	TrackCountsEnabled      bool
	TablesWithAutovacuumOff int
	MaxRelfrozenxidAge      int64

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
	AnalyzeDisabledTables   int
	WalLevel                string
	LogicalSlotsActive      int
}
