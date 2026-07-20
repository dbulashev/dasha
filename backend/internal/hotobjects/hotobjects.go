// Package hotobjects defines the domain model of the hot-tables/indexes
// feature: daily per-host delta snapshots stored as an exact top-N per metric
// class plus a histogram of the tail — the same trick PostgreSQL statistics
// use (MCV list + histogram) applied to activity counters.
// See plans/hot-objects.md and plans/hot-objects-design.md.
package hotobjects

import (
	"time"

	"github.com/google/uuid"
)

// Kind discriminates tables from indexes in storage rows.
type Kind string

const (
	KindTable Kind = "t"
	KindIndex Kind = "i"
)

// Class is an activity class; each class keeps its own top, the way pg_stats
// keeps per-column statistics.
type Class string

const (
	ClassReads  Class = "reads"
	ClassWrites Class = "writes"
	ClassIO     Class = "io"
)

// TableClasses lists classes tracked for tables.
var TableClasses = []Class{ClassReads, ClassWrites, ClassIO}

// IndexClasses lists classes tracked for indexes. PostgreSQL keeps no
// per-index write counters, so indexes have no writes class.
var IndexClasses = []Class{ClassReads, ClassIO}

// Counters is a set of raw counters keyed by pg_stat column name. The same
// shape carries cumulatives (anchors) and deltas (snapshots).
type Counters map[string]int64

// AnchorRow is one object's cumulative sample on one host — the working state
// deltas are computed against, overwritten on every snapshot.
type AnchorRow struct {
	Instance   string
	Kind       Kind
	Schema     string
	Object     string
	TableName  string // for indexes: the table the index belongs to
	CapturedAt time.Time
	StatsReset *time.Time
	SizeBytes  int64
	Counters   Counters
}

// HostWindow is one host's delta window within a snapshot.
type HostWindow struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Complete bool      `json:"complete"`
	// StatsReset is the host's pg_stat_database.stats_reset at capture time —
	// the epoch marker: a change between snapshots invalidates the interval.
	StatsReset *time.Time `json:"stats_reset,omitempty"`
}

// Histogram describes the tail (objects outside the top) of one kind within
// one class: enough to compute coverage honesty and project any object's
// live delta onto a percentile.
type Histogram struct {
	// Deciles are the 10th..90th percentiles of the class ranking key.
	Deciles []float64 `json:"deciles"`
	Count   int       `json:"count"`
	Sum     int64     `json:"sum"`
	Max     int64     `json:"max"`
}

// KindHistogram splits a class histogram by object kind. Writes has no index
// part (no per-index write counters).
type KindHistogram struct {
	Tables  *Histogram `json:"tables,omitempty"`
	Indexes *Histogram `json:"indexes,omitempty"`
}

// KindRatio is a per-kind coverage ratio: sum(top)/sum(all), 0..1.
type KindRatio struct {
	Tables  float64 `json:"tables"`
	Indexes float64 `json:"indexes"`
}

// HostDelta is one host's contribution to a top entry.
type HostDelta struct {
	Complete bool `json:"complete"`
	// InRecovery marks a standby — read activity seen only here is invisible
	// from the primary's counters.
	InRecovery bool     `json:"in_recovery"`
	Delta      Counters `json:"delta"`
}

// TopEntry is one object in the top of one class. Delta is the cluster-wide
// sum over hosts with a valid delta; PerHost keeps the breakdown.
type TopEntry struct {
	Rank      int
	Kind      Kind
	Class     Class
	Schema    string
	Object    string
	TableName string
	SizeBytes int64
	Delta     Counters
	PerHost   map[string]HostDelta
}

// Snapshot is one daily hot-objects capture for a cluster×database.
type Snapshot struct {
	ID           uuid.UUID
	ClusterName  string
	Database     string
	CapturedAt   time.Time
	Windows      map[string]HostWindow
	HostsMissing []string
	Coverage     map[Class]KindRatio
	Histograms   map[Class]KindHistogram
	Top          []TopEntry
}

// HistoryEntry is one appearance of an object in a stored top.
type HistoryEntry struct {
	CapturedAt time.Time
	Class      Class
	Rank       int
	SizeBytes  int64
	Delta      Counters
}
