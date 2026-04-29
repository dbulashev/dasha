package dto

import "time"

type QueryBlocked struct {
	LockedItem                            string
	BlockedPid                            int32
	BlockedUser                           string
	BlockedQuery                          string
	BlockedDuration                       string
	BlockedDurationMs                     *float64
	BlockedMode                           string
	BlockingPid                           int32
	BlockingUser                          string
	StateOfBlockingProcess                string
	CurrentOrRecentQueryInBlockingProcess string
	BlockingDuration                      string
	BlockingDurationMs                    *float64
	BlockingMode                          string
}

type QueryRunning struct {
	Pid         int32
	State       string
	Source      string
	Duration    string
	Waiting     bool
	Query       string
	StartedAt   time.Time
	DurationMs  float64
	User        string
	BackendType string
}

type QueryTop10ByTime struct {
	QueryID    int64
	ExecTime   string
	ExecTimeMs float64
	IoCpuPct   string
	IoPct      float64
	CpuPct     float64
	QueryTrunc string
}

type QueryTop10ByWal struct {
	QueryID    int64
	WalVolume  string
	WalBytes   int64
	QueryTrunc string
}

type QueryTop10ChartItem struct {
	Metric  string
	QueryID int64
	Pct     float64
}

type QueryStatsStatus struct {
	Available bool
	Enabled   bool
	Readable  bool
}

type QueryReport struct {
	QueryID              int64
	Query                string
	Usernames            []string
	StddevExecTimeMs     *float64
	StddevPlanTimeMs     *float64
	Rows                 *int64
	RowsPct              *float64
	Calls                *int64
	CallsPct             *float64
	TotalTimeMs          *float64
	TotalTimePct         *float64
	ExecTimeMs           *float64
	MinExecTimeMs        *float64
	MaxExecTimeMs        *float64
	MeanExecTimeMs       *float64
	PlanTimeMs           *float64
	MinPlanTimeMs        *float64
	MaxPlanTimeMs        *float64
	MeanPlanTimeMs       *float64
	IoTimeMs             *float64
	IoTimePct            *float64
	CpuTimeMs            *float64
	CpuTimePct           *float64
	CacheHitRatio        *float64
	SharedBlksDirtiedPct *float64
	SharedBlksWrittenPct *float64
	WalBytes             *int64
	WalBytesPct          *float64
	WalRecords           *int64
	WalFpi               *int64
	TempBlks             *int64
	TempBlksPct          *float64
}
