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
	// The 'wal' object rows (PostgreSQL 18 and newer) time themselves by this
	// setting, not by TrackIOTiming.
	TrackWALIOTiming bool
	StatsReset       *time.Time
	Rows             []Row
}

// Meta is one capture's header: what a read plan needs before any matrix body
// is fetched.
type Meta struct {
	CapturedAt       time.Time
	VersionNum       int
	TrackIOTiming    bool
	TrackWALIOTiming bool
	StatsReset       *time.Time
}

func (s Snapshot) Meta() Meta {
	return Meta{
		CapturedAt:       s.CapturedAt,
		VersionNum:       s.VersionNum,
		TrackIOTiming:    s.TrackIOTiming,
		TrackWALIOTiming: s.TrackWALIOTiming,
		StatsReset:       s.StatsReset,
	}
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
	// Partial marks a bucket that could not measure all of its own span: the
	// consumer draws a gap, not a zero.
	Partial bool
	Rows    []Row
}

// Span is a run of consecutive measurable captures inside one bucket, named by
// its two ends. The counters are cumulative, so the run's total is the
// difference of the ends and the captures between them are never read.
type Span struct {
	From time.Time
	To   time.Time
}

// BucketPlan is one point of a series resolved from capture headers alone.
type BucketPlan struct {
	From     time.Time
	To       time.Time
	Duration time.Duration
	Partial  bool
	Spans    []Span
}

// Plan is the whole series of a history request: its buckets and, through them,
// the only captures that have to be loaded.
type Plan struct {
	Buckets []BucketPlan
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

	if iv.Duration <= 0 || !sameEpoch(prev.Meta(), cur.Meta()) {
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
func sameEpoch(prev, cur Meta) bool {
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

// metaIntervals measures every adjacent pair of a chronological header slice:
// the bounds of each interval and whether it is measurable, without its values.
func metaIntervals(metas []Meta) []Interval {
	if len(metas) < 2 {
		return nil
	}

	out := make([]Interval, 0, len(metas)-1)

	for i := 1; i < len(metas); i++ {
		prev, cur := metas[i-1], metas[i]

		iv := Interval{
			From:     prev.CapturedAt,
			To:       cur.CapturedAt,
			Duration: cur.CapturedAt.Sub(prev.CapturedAt),
		}
		iv.Complete = iv.Duration > 0 && sameEpoch(prev, cur)

		out = append(out, iv)
	}

	return out
}

// groupIntervals splits a chronological interval slice into at most points
// contiguous groups of equal duration; a group with no interval is dropped.
func groupIntervals(intervals []Interval, points int) [][]Interval {
	if len(intervals) == 0 {
		return nil
	}

	if points < 1 {
		points = 1
	}

	if len(intervals) <= points {
		out := make([][]Interval, 0, len(intervals))
		for i := range intervals {
			out = append(out, intervals[i:i+1])
		}

		return out
	}

	start := intervals[0].From
	span := intervals[len(intervals)-1].To.Sub(start)

	if span <= 0 {
		return [][]Interval{intervals}
	}

	width := span / time.Duration(points)
	groups := make([][]Interval, points)

	for _, iv := range intervals {
		// An interval lands in the group holding its midpoint. Its upper bound
		// would sit exactly on a group edge whenever the capture schedule
		// divides the span evenly, and every such interval would fall into the
		// next group; the midpoint also places an over-long interval (a missed
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

	out := make([][]Interval, 0, points)

	for _, g := range groups {
		if len(g) > 0 {
			out = append(out, g)
		}
	}

	return out
}

// PlanBuckets lays out the series of a history request from capture headers
// alone, so a month-wide window costs the same handful of matrix reads as an
// hour-wide one.
func PlanBuckets(metas []Meta, points int) Plan {
	groups := groupIntervals(metaIntervals(metas), points)

	buckets := make([]BucketPlan, 0, len(groups))
	for _, g := range groups {
		buckets = append(buckets, planOf(g))
	}

	return Plan{Buckets: buckets}
}

// planOf folds one group of intervals into a point. Only measurable intervals
// contribute duration; an unmeasurable one ends the current span and is
// reported as Partial rather than diluted into the sum.
func planOf(group []Interval) BucketPlan {
	p := BucketPlan{From: group[0].From, To: group[len(group)-1].To}
	openSpan := -1

	for _, iv := range group {
		if !iv.Complete {
			p.Partial = true
			openSpan = -1

			continue
		}

		p.Duration += iv.Duration

		if openSpan >= 0 {
			p.Spans[openSpan].To = iv.To

			continue
		}

		p.Spans = append(p.Spans, Span{From: iv.From, To: iv.To})
		openSpan = len(p.Spans) - 1
	}

	return p
}

// At lists every capture the plan has to load, chronologically and without
// repeats: neighbouring buckets share the capture on their boundary.
func (p Plan) At() []time.Time {
	out := make([]time.Time, 0, len(p.Buckets)+1)
	seen := make(map[int64]struct{}, len(p.Buckets)+1)

	for _, b := range p.Buckets {
		for _, s := range b.Spans {
			for _, t := range []time.Time{s.From, s.To} {
				if _, ok := seen[t.UnixNano()]; ok {
					continue
				}

				seen[t.UnixNano()] = struct{}{}

				out = append(out, t)
			}
		}
	}

	return out
}

// Assemble fills a plan with the captures it asked for. A span whose ends turn
// out not to be comparable — a reset the headers did not show — is dropped from
// its bucket along with its duration, so the rates the bucket carries stay the
// rates of what it actually measured.
func (p Plan) Assemble(snaps []Snapshot) []Bucket {
	byTime := make(map[int64]Snapshot, len(snaps))
	for _, s := range snaps {
		byTime[s.CapturedAt.UnixNano()] = s
	}

	out := make([]Bucket, 0, len(p.Buckets))

	for _, bp := range p.Buckets {
		b := Bucket{From: bp.From, To: bp.To, Duration: bp.Duration, Partial: bp.Partial}
		acc := map[Key]map[string]int64{}

		for _, s := range bp.Spans {
			iv, ok := spanDelta(byTime, s)
			if !ok {
				b.Partial = true
				b.Duration -= s.To.Sub(s.From)

				continue
			}

			for _, r := range iv.Rows {
				add(acc, r.Key, r.Values)
			}
		}

		b.Rows = sortedRows(acc)

		out = append(out, b)
	}

	return out
}

func spanDelta(byTime map[int64]Snapshot, s Span) (Interval, bool) {
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
