package dto

type ProgressAnalyze struct {
	Pid               int32
	Datname           string
	TableName         string
	Phase             string
	SampleBlksTotal   int64
	SampleBlksScanned int64
	ExtStatsTotal     int64
	ExtStatsComputed  int64
	CurrentChildTable string
	StartedBy         *string
}

type ProgressBaseBackup struct {
	Pid                 int32
	Phase               string
	BackupTotal         int64
	BackupStreamed      int64
	ProgressPercentage  *float64
	TablespacesTotal    int64
	TablespacesStreamed int64
	BackupType          *string
}

type ProgressCluster struct {
	Pid       int32
	Datname   string
	TableName string
	Command   string
	Phase     string
	// ClusterIndex is nil when the table is rewritten without ordering.
	ClusterIndex      *string
	HeapTuplesScanned int64
	// HeapTuplesWritten is PG < 19; Inserted/Updated/Deleted are PG 19+.
	HeapTuplesWritten  *int64
	HeapTuplesInserted *int64
	HeapTuplesUpdated  *int64
	HeapTuplesDeleted  *int64
	HeapBlksTotal      int64
	HeapBlksScanned    int64
	IndexRebuildCount  int64
}

type ProgressCopy struct {
	Pid                int32
	Datname            string
	TableName          *string
	Command            string
	Type               string
	Query              *string
	BytesProcessed     int64
	BytesTotal         *int64
	TuplesProcessed    int64
	TuplesExcluded     int64
	TuplesSkipped      *int64
	TableSize          *int64
	TableTuples        *int64
	ElapsedSeconds     *float64
	ProgressPercentage *float64
	EtaSeconds         *float64
}

type ProgressIndex struct {
	Pid              int32
	Datname          string
	TableName        string
	IndexName        string
	Phase            string
	LockersTotal     int64
	LockersDone      int64
	CurrentLockerPid int32
	BlocksTotal      int64
	BlocksDone       int64
	TuplesTotal      int64
	TuplesDone       int64
	PartitionsTotal  int64
	PartitionsDone   int64
}

type ProgressVacuum struct {
	Pid              int32
	Datname          string
	TableName        string
	Phase            string
	HeapBlksTotal    int64
	HeapBlksScanned  int64
	HeapBlksVacuumed int64
	IndexVacuumCount int64
	// MaxDeadTuples is NULL on PG 17+, which caps dead tuples by memory
	// (MaxDeadTupleBytes) rather than by row count.
	MaxDeadTuples *int64
	NumDeadTuples int64
	// DeadTupleBytes / MaxDeadTupleBytes are PG 17+ only (NULL before).
	DeadTupleBytes    *int64
	MaxDeadTupleBytes *int64
	// StartedBy / Mode are PG 19+ only (NULL before).
	StartedBy *string
	Mode      *string
}
