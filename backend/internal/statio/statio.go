// Package statio is the domain model of the I/O section: raw cumulative
// pg_stat_io slices captured per host, the deltas between adjacent captures,
// and their aggregation into a bounded number of chart points.
package statio

import (
	"errors"
	"maps"
	"sort"
	"time"
)

// MinVersionNum is the first PostgreSQL version that has pg_stat_io.
const MinVersionNum = 160000

// ErrUnsupportedVersion marks a host older than MinVersionNum, which has no
// pg_stat_io at all — a deployment fact, not a failure.
var ErrUnsupportedVersion = errors.New("pg_stat_io requires PostgreSQL 16 or newer")

// Key identifies one cell of the pg_stat_io matrix. A grouped series leaves the
// dimensions it was summed over empty.
type Key struct {
	BackendType string `json:"backend_type,omitempty"`
	Object      string `json:"object,omitempty"`
	Context     string `json:"context,omitempty"`
}

// Row is one matrix cell. Values are counts, plus times in milliseconds when
// track_io_timing is on.
type Row struct {
	Key
	Values map[string]int64
}

// Snapshot is one host's raw cumulative pg_stat_io slice.
type Snapshot struct {
	CapturedAt time.Time
	VersionNum int
	// OpBytes is the fixed operation size PG 16/17 report per row; PG 18+ carry
	// byte counters instead and leave this nil.
	OpBytes       *int
	TrackIOTiming bool
	StatsReset    *time.Time
	Rows          []Row
}

// Interval is the delta between two adjacent snapshots of one host.
type Interval struct {
	From     time.Time
	To       time.Time
	Duration time.Duration
	// Complete is false when the epoch broke between the two captures: the
	// bounds still describe the gap, but no values are carried.
	Complete bool
	Rows     []Row
}

// Bucket is a run of adjacent intervals summed into one point of a series.
type Bucket struct {
	From time.Time
	To   time.Time
	// Duration sums only the complete intervals — the denominator of any rate
	// derived from Rows.
	Duration time.Duration
	// Partial marks a bucket that swallowed at least one incomplete interval:
	// the consumer draws a gap, not a zero.
	Partial bool
	Rows    []Row
}

// byteCounters maps an operation counter to the byte counter derived from it on
// PG 16/17, where pg_stat_io reports a single op_bytes instead.
var byteCounters = map[string]string{
	"reads":   "read_bytes",
	"writes":  "write_bytes",
	"extends": "extend_bytes",
}

// Normalized fills in the byte counters PG 18 reports natively, so nothing above
// this layer has to know the server version.
func (s Snapshot) Normalized() Snapshot {
	if s.OpBytes == nil || *s.OpBytes <= 0 {
		return s
	}

	op := int64(*s.OpBytes)
	rows := make([]Row, len(s.Rows))

	for i, r := range s.Rows {
		vals := make(map[string]int64, len(r.Values)+len(byteCounters))
		maps.Copy(vals, r.Values)

		for src, dst := range byteCounters {
			if _, ok := vals[dst]; ok {
				continue
			}

			if v, ok := vals[src]; ok {
				vals[dst] = v * op
			}
		}

		rows[i] = Row{Key: r.Key, Values: vals}
	}

	s.Rows = rows

	return s
}

// Delta measures the interval between two adjacent snapshots of one host. The
// returned bool is the interval's completeness; an incomplete interval still
// carries its bounds so the consumer can draw the gap.
func Delta(prev, cur Snapshot) (Interval, bool) {
	iv := Interval{
		From:     prev.CapturedAt,
		To:       cur.CapturedAt,
		Duration: cur.CapturedAt.Sub(prev.CapturedAt),
	}

	if iv.Duration <= 0 || !sameEpoch(prev, cur) {
		return iv, false
	}

	base := make(map[Key]map[string]int64, len(prev.Rows))
	for _, r := range prev.Normalized().Rows {
		base[r.Key] = r.Values
	}

	rows := make([]Row, 0, len(cur.Rows))

	for _, c := range cur.Normalized().Rows {
		b, ok := base[c.Key]
		if !ok {
			// The row has no baseline in this interval (a new backend type
			// after a restart, a WAL row after an upgrade) — skip the row, not
			// the interval.
			continue
		}

		d := make(map[string]int64, len(c.Values))

		for name, v := range c.Values {
			prevVal, ok := b[name]
			if !ok {
				continue
			}

			// A counter can only fall through a reset that the stats_reset
			// comparison missed; nothing in the interval is trustworthy then.
			if v < prevVal {
				iv.Rows = nil

				return iv, false
			}

			d[name] = v - prevVal
		}

		rows = append(rows, Row{Key: c.Key, Values: d})
	}

	iv.Complete = true
	iv.Rows = rows

	return iv, true
}

// sameEpoch reports whether the two captures describe one continuous run of the
// same server: a major upgrade changes both the columns and the row set, and
// stats_reset marks a restart or pg_stat_reset_shared('io').
func sameEpoch(prev, cur Snapshot) bool {
	if prev.VersionNum/10000 != cur.VersionNum/10000 {
		return false
	}

	switch {
	case prev.StatsReset == nil && cur.StatsReset == nil:
		return true
	case prev.StatsReset == nil || cur.StatsReset == nil:
		return false
	default:
		return prev.StatsReset.Equal(*cur.StatsReset)
	}
}

// Intervals measures every adjacent pair of a chronological snapshot slice.
func Intervals(snaps []Snapshot) []Interval {
	if len(snaps) < 2 {
		return nil
	}

	out := make([]Interval, 0, len(snaps)-1)

	for i := 1; i < len(snaps); i++ {
		iv, _ := Delta(snaps[i-1], snaps[i])
		out = append(out, iv)
	}

	return out
}

// Bucketize sums adjacent intervals into at most points buckets of equal
// duration. Deltas are additive, so summing is the whole aggregation rule.
func Bucketize(intervals []Interval, points int) []Bucket {
	if len(intervals) == 0 {
		return nil
	}

	if points < 1 {
		points = 1
	}

	if len(intervals) <= points {
		out := make([]Bucket, 0, len(intervals))
		for _, iv := range intervals {
			out = append(out, bucketOf([]Interval{iv}))
		}

		return out
	}

	start := intervals[0].From
	span := intervals[len(intervals)-1].To.Sub(start)

	if span <= 0 {
		return []Bucket{bucketOf(intervals)}
	}

	width := span / time.Duration(points)
	groups := make([][]Interval, points)

	for _, iv := range intervals {
		// An interval lands in the bucket holding its midpoint. Its upper bound
		// would sit exactly on a bucket edge whenever the capture schedule
		// divides the span evenly, and every such interval would fall into the
		// next bucket; the midpoint also places an over-long interval (a missed
		// capture) where it actually spent its time.
		idx := int(iv.From.Add(iv.To.Sub(iv.From)/2).Sub(start) / width)
		if idx >= points {
			idx = points - 1
		}

		if idx < 0 {
			idx = 0
		}

		groups[idx] = append(groups[idx], iv)
	}

	out := make([]Bucket, 0, points)

	for _, g := range groups {
		if len(g) > 0 {
			out = append(out, bucketOf(g))
		}
	}

	return out
}

// bucketOf folds one group of intervals into a point. Only complete intervals
// contribute values and duration; the presence of an incomplete one is reported
// as Partial rather than diluted into the sum.
func bucketOf(group []Interval) Bucket {
	b := Bucket{From: group[0].From, To: group[len(group)-1].To}

	acc := map[Key]map[string]int64{}

	for _, iv := range group {
		if !iv.Complete {
			b.Partial = true

			continue
		}

		b.Duration += iv.Duration

		for _, r := range iv.Rows {
			add(acc, r.Key, r.Values)
		}
	}

	b.Rows = sortedRows(acc)

	return b
}

// Filter narrows the matrix before grouping; an empty field matches everything.
type Filter struct {
	BackendType string
	Object      string
	Context     string
}

func (f Filter) match(k Key) bool {
	if f.BackendType != "" && f.BackendType != k.BackendType {
		return false
	}

	if f.Object != "" && f.Object != k.Object {
		return false
	}

	return f.Context == "" || f.Context == k.Context
}

// GroupBy names the dimensions a series keeps; the rest are summed over.
type GroupBy string

const (
	GroupByContext     GroupBy = "context"
	GroupByBackendType GroupBy = "backend_type"
	GroupByFull        GroupBy = "full"
)

func (g GroupBy) project(k Key) Key {
	switch g {
	case GroupByBackendType:
		return Key{BackendType: k.BackendType}
	case GroupByFull:
		return k
	case GroupByContext:
		return Key{Context: k.Context}
	default:
		return Key{Context: k.Context}
	}
}

// Reduce filters the matrix and sums it down to the requested grouping.
func Reduce(rows []Row, f Filter, by GroupBy) []Row {
	acc := map[Key]map[string]int64{}

	for _, r := range rows {
		if !f.match(r.Key) {
			continue
		}

		add(acc, by.project(r.Key), r.Values)
	}

	return sortedRows(acc)
}

func add(acc map[Key]map[string]int64, k Key, values map[string]int64) {
	dst, ok := acc[k]
	if !ok {
		dst = make(map[string]int64, len(values))
		acc[k] = dst
	}

	for name, v := range values {
		dst[name] += v
	}
}

func sortedRows(acc map[Key]map[string]int64) []Row {
	out := make([]Row, 0, len(acc))
	for k, v := range acc {
		out = append(out, Row{Key: k, Values: v})
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Key, out[j].Key
		if a.BackendType != b.BackendType {
			return a.BackendType < b.BackendType
		}

		if a.Object != b.Object {
			return a.Object < b.Object
		}

		return a.Context < b.Context
	})

	return out
}
