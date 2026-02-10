package dto

import "time"

type StatsResetTime struct {
	Time time.Time
}

type DatabaseSize struct {
	SizeBytes  int64
	SizePretty string
}

type DatabaseHealth struct {
	Deadlocks            int64
	Conflicts            int64
	ChecksumFailures     *int64
	ChecksumLastFailure  *time.Time
	XactCommit           int64
	XactRollback         int64
	RollbackRatio        float64
	StatsReset           *time.Time
}
