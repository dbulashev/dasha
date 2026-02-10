package dto

type ProgressAnalyze struct {
	Pid               int32
	Datname           string
	TableName         string
	PhaseDescription  string
	SampleBlksTotal   int64
	SampleBlksScanned int64
	ExtStatsTotal     int64
	ExtStatsComputed  int64
	CurrentChildTable string
}

type ProgressBaseBackup struct {
	Pid                 int32
	PhaseDescription    string
	BackupTotal         int64
	BackupStreamed      int64
	ProgressPercentage  *float64
	TablespacesTotal    int64
	TablespacesStreamed int64
}

type ProgressCluster struct {
	Pid               int32
	Datname           string
	TableName         string
	Command           string
	PhaseDescription  string
	ClusterIndex      string
	HeapTuplesScanned int64
	HeapTuplesWritten int64
	HeapBlksTotal     int64
	HeapBlksScanned   int64
	IndexRebuildCount int64
}

type ProgressIndex struct {
	Pid              int32
	Datname          string
	TableName        string
	IndexName        string
	PhaseDescription string
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
	PhaseDescription string
	HeapBlksTotal    int64
	HeapBlksScanned  int64
	HeapBlksVacuumed int64
	IndexVacuumCount int64
	MaxDeadTuples    int64
	NumDeadTuples    int64
}
