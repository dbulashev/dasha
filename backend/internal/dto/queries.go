package dto

import "time"

// Scope selects whether a pg_stat_statements-backed answer covers the named
// database only or the whole instance. The view is per-database but its
// contents are instance-wide, so both are legitimate readings of the same data.
const (
	ScopeDatabase = "database"
	ScopeInstance = "instance"
)

type QueryBlocked struct {
	LockedItem                            string
	BlockedPid                            int32
	BlockedDatabase                       string
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
	Pid           int32
	State         string
	Source        string
	Duration      string
	Waiting       bool
	WaitEventType string
	WaitEvent     string
	ClientAddr    string
	Query         string
	StartedAt     time.Time
	DurationMs    float64
	User          string
	BackendType   string
}

type QueryTop10ByTime struct {
	QueryID    int64
	Datname    string
	ExecTime   string
	ExecTimeMs float64
	IoCpuPct   string
	IoPct      float64
	CpuPct     float64
	QueryTrunc string
}

type QueryTop10ByWal struct {
	QueryID    int64
	Datname    string
	WalVolume  string
	WalBytes   int64
	QueryTrunc string
}

type QueryTop10ChartItem struct {
	Metric  string
	QueryID int64
	Datname string
	Pct     float64
}

// QueryStatsStatus describes the query-statistics extension of one connection.
// Source is always filled: with nothing installed it names the one worth
// installing here. Restricted means the role lacks the privileges of
// pg_read_all_stats, and the view then hides the identifier and the text of
// every statement of another user.
type QueryStatsStatus struct {
	Available  bool
	Enabled    bool
	Readable   bool
	Restricted bool
	Source     string
}

// QueryReport is one aggregated pg_stat_statements entry of a single database.
// Shares come in two flavours: the *Pct fields are shares within Datname, the
// *PctInstance ones within the whole instance. Both are computed before the
// report is truncated to the per-database top, so both stay exact in a stored
// snapshot; the API exposes whichever matches the requested scope.
type QueryReport struct {
	QueryID                      int64
	Query                        string
	Usernames                    []string
	Datname                      string
	StddevExecTimeMs             *float64
	StddevPlanTimeMs             *float64
	Rows                         *int64
	RowsPct                      *float64
	RowsPctInstance              *float64
	Calls                        *int64
	CallsPct                     *float64
	CallsPctInstance             *float64
	TotalTimeMs                  *float64
	TotalTimePct                 *float64
	TotalTimePctInstance         *float64
	ExecTimeMs                   *float64
	MinExecTimeMs                *float64
	MaxExecTimeMs                *float64
	MeanExecTimeMs               *float64
	PlanTimeMs                   *float64
	MinPlanTimeMs                *float64
	MaxPlanTimeMs                *float64
	MeanPlanTimeMs               *float64
	IoTimeMs                     *float64
	IoTimePct                    *float64
	IoTimePctInstance            *float64
	CpuTimeMs                    *float64
	CpuTimePct                   *float64
	CpuTimePctInstance           *float64
	CacheHitRatio                *float64
	SharedBlksDirtiedPct         *float64
	SharedBlksDirtiedPctInstance *float64
	SharedBlksWrittenPct         *float64
	SharedBlksWrittenPctInstance *float64
	WalBytes                     *int64
	WalBytesPct                  *float64
	WalBytesPctInstance          *float64
	WalRecords                   *int64
	WalFpi                       *int64
	TempBlks                     *int64
	TempBlksPct                  *float64
	TempBlksPctInstance          *float64
}
