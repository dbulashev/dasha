// Package statlock is the domain model of the lock wait statistics: raw
// cumulative pg_stat_lock slices captured per host and the deltas between them,
// laid out into chart points by the statio bucket planner.
package statlock

import (
	"errors"
	"sort"
	"time"

	"github.com/dbulashev/dasha/internal/statio"
)

const MinVersionNum = 190000

var ErrUnsupportedVersion = errors.New("pg_stat_lock requires PostgreSQL 19 or newer")

// Row is one lock type. Only waits longer than deadlock_timeout are counted.
type Row struct {
	LockType         string
	Waits            int64
	WaitTimeMs       int64
	FastpathExceeded int64
}

type Snapshot struct {
	CapturedAt time.Time
	VersionNum int
	// StatsReset is the newest stats_reset among the rows: a reset moves it
	// forward whichever rows it touched.
	StatsReset             *time.Time
	DeadlockTimeoutMs      int
	MaxLocksPerTransaction int
	Rows                   []Row
}

type Meta struct {
	CapturedAt             time.Time
	VersionNum             int
	StatsReset             *time.Time
	DeadlockTimeoutMs      int
	MaxLocksPerTransaction int
}

func (s Snapshot) Meta() Meta {
	return Meta{
		CapturedAt:             s.CapturedAt,
		VersionNum:             s.VersionNum,
		StatsReset:             s.StatsReset,
		DeadlockTimeoutMs:      s.DeadlockTimeoutMs,
		MaxLocksPerTransaction: s.MaxLocksPerTransaction,
	}
}

func (m Meta) epoch() statio.Meta {
	return statio.Meta{CapturedAt: m.CapturedAt, VersionNum: m.VersionNum, StatsReset: m.StatsReset}
}

// Interval is the delta between two adjacent snapshots of one host.
type Interval struct {
	From     time.Time
	To       time.Time
	Duration time.Duration
	Complete bool
	Rows     []Row
}

// Bucket is one point of a series. Duration sums only the measured spans;
// Partial means part of the bucket could not be measured — a gap, not a zero.
type Bucket struct {
	From     time.Time
	To       time.Time
	Duration time.Duration
	Partial  bool
	Rows     []Row
}

// Delta measures the interval between two adjacent snapshots of one host. An
// incomplete interval keeps its bounds so the consumer can draw the gap.
func Delta(prev, cur Snapshot) (Interval, bool) {
	iv := Interval{
		From:     prev.CapturedAt,
		To:       cur.CapturedAt,
		Duration: cur.CapturedAt.Sub(prev.CapturedAt),
	}

	if iv.Duration <= 0 || !statio.SameEpoch(prev.Meta().epoch(), cur.Meta().epoch()) {
		return iv, false
	}

	base := make(map[string]Row, len(prev.Rows))
	for _, r := range prev.Rows {
		base[r.LockType] = r
	}

	rows := make([]Row, 0, len(cur.Rows))

	for _, c := range cur.Rows {
		b, ok := base[c.LockType]
		if !ok {
			continue
		}

		// A counter falls only through a reset stats_reset did not show.
		if c.Waits < b.Waits || c.WaitTimeMs < b.WaitTimeMs || c.FastpathExceeded < b.FastpathExceeded {
			return iv, false
		}

		rows = append(rows, Row{
			LockType:         c.LockType,
			Waits:            c.Waits - b.Waits,
			WaitTimeMs:       c.WaitTimeMs - b.WaitTimeMs,
			FastpathExceeded: c.FastpathExceeded - b.FastpathExceeded,
		})
	}

	iv.Complete = true
	iv.Rows = rows

	return iv, true
}

// PlanBuckets lays the series out from capture headers alone.
func PlanBuckets(metas []Meta, points int) statio.Plan {
	epochs := make([]statio.Meta, len(metas))
	for i, m := range metas {
		epochs[i] = m.epoch()
	}

	return statio.PlanBuckets(epochs, points)
}

// Assemble fills a plan with the captures it asked for. A span whose ends turn
// out not to be comparable is dropped from its bucket along with its duration.
func Assemble(p statio.Plan, snaps []Snapshot) []Bucket {
	byTime := make(map[int64]Snapshot, len(snaps))
	for _, s := range snaps {
		byTime[s.CapturedAt.UnixNano()] = s
	}

	out := make([]Bucket, 0, len(p.Buckets))

	for _, bp := range p.Buckets {
		b := Bucket{From: bp.From, To: bp.To, Duration: bp.Duration, Partial: bp.Partial}
		acc := map[string]Row{}

		for _, s := range bp.Spans {
			iv, ok := spanDelta(byTime, s)
			if !ok {
				b.Partial = true
				b.Duration -= s.To.Sub(s.From)

				continue
			}

			for _, r := range iv.Rows {
				a := acc[r.LockType]
				a.LockType = r.LockType
				a.Waits += r.Waits
				a.WaitTimeMs += r.WaitTimeMs
				a.FastpathExceeded += r.FastpathExceeded
				acc[r.LockType] = a
			}
		}

		b.Rows = sortedRows(acc)

		out = append(out, b)
	}

	return out
}

func spanDelta(byTime map[int64]Snapshot, s statio.Span) (Interval, bool) {
	from, ok := byTime[s.From.UnixNano()]
	if !ok {
		return Interval{}, false
	}

	to, ok := byTime[s.To.UnixNano()]
	if !ok {
		return Interval{}, false
	}

	return Delta(from, to)
}

func sortedRows(acc map[string]Row) []Row {
	out := make([]Row, 0, len(acc))
	for _, r := range acc {
		out = append(out, r)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].LockType < out[j].LockType })

	return out
}
