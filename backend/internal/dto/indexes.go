package dto

import "time"

type IndexBloat struct {
	Schema     string
	Table      string
	Index      string
	BloatBytes int64
	IndexBytes int64
	Definition string
	Primary    bool
}

type IndexBtreeOnArray struct {
	Table string
	Index string
}

type IndexCaching struct {
	Schema  string
	Table   string
	Index   string
	HitRate float64
}

type IndexHitRate struct {
	Rate float64
}

type IndexInvalidOrNotReady struct {
	Table      string
	IndexName  string
	IsValid    bool
	IsReady    bool
	Constraint string
}

type IndexMissing struct {
	Schema                  string
	Table                   string
	PercentOfTimesIndexUsed *float64
	EstimatedRows           int64
}

type IndexSimilar1 struct {
	Table                   string
	I1UniqueIndexName       string
	I2IndexName             string
	I1UniqueIndexDefinition string
	I2IndexDefinition       string
	I1UsedInConstraint      string
	I2UsedInConstraint      string
}

type IndexSimilar2 struct {
	Table   string
	FkName  string
	FkName2 string
}

type IndexSimilar3 struct {
	Table                     string
	I1IndexName               string
	I2IndexName               string
	SimplifiedIndexDefinition string
	I1IndexDefinition         string
	I2IndexDefinition         string
	I1UsedInConstraint        string
	I2UsedInConstraint        string
}

type IndexTopKBySize struct {
	Tablespace string
	Table      string
	Index      string
	Size       string
	SizeBytes  int64
}

type IndexUnused struct {
	Schema     string
	Table      string
	Index      string
	SizeBytes  int64
	IndexScans int64
}

// IndexScanSample is one index's scan counter on ONE host, together with the window
// the counter was accumulated over. idx_scan is per-instance and is NOT replicated,
// so the same index has a different sample on every host of a cluster.
//
// StatsReset is nil when the database's statistics were never reset; WindowDays then
// falls back to postmaster uptime, which understates the real window (see the SQL).
//
// Root* names the only unit that can actually be DROPped. For a plain index that is
// the index itself. For a partition's child index it is the top-level partitioned
// index: PostgreSQL refuses to drop a child ("index <parent> requires it") and its
// HINT points at the parent, which would remove the index from EVERY partition. So
// the caller sums the children up to the root before judging anything — a cold
// partition showing zero scans is partition pruning working, not a dead index.
type IndexScanSample struct {
	Schema        string
	Table         string
	Index         string
	RootSchema    string
	RootIndex     string
	RootTable     string
	IsPartitioned bool
	SizeBytes     int64
	IndexScans    int64
	StatsReset    *time.Time
	WindowDays    float64
	InRecovery    bool
}

// IndexHostSample attributes an IndexScanSample to the host it came from.
type IndexHostSample struct {
	Instance string
	Sample   IndexScanSample
}

// IndexClusterScans is the raw material for the unused-index report: every host's
// samples plus the hosts that could not be reached. Unreachable hosts matter for
// correctness, not just completeness — an index idle on the hosts we did reach may
// be serving the whole read workload on the one we did not, so a missing host must
// block a "drop it" verdict rather than be silently skipped.
type IndexClusterScans struct {
	Samples     []IndexHostSample
	Unreachable []string
}

type IndexUsage struct {
	Schema                  string
	Table                   string
	PercentOfTimesIndexUsed *float64
	EstimatedRows           int64
}
