package metrics

import (
	"slices"
	"sort"
	"time"
)

// Baseline holds a seasonal reference: the median value per hour-of-week bucket
// (0..167, Sunday 00:00 = 0), computed over a history window. It lets the trend
// flag "this Monday 09:00 is worse than usual Monday 09:00s" rather than a flat
// average that ignores daily/weekly shape.
type Baseline struct {
	ByHourOfWeek [168]float64
	Count        [168]int
	Enough       bool // history is long enough for the seasonal model to be meaningful
}

// hourOfWeek maps a timestamp to its 0..167 bucket (UTC, Sunday-based).
func hourOfWeek(t time.Time) int {
	u := t.UTC()

	return int(u.Weekday())*24 + u.Hour()
}

// BuildBaseline buckets points by hour-of-week and takes the median per bucket.
// Enough is true once at least minPoints samples are available overall; below
// that the caller should treat the baseline as unavailable (degrade to none).
func BuildBaseline(points []SeriesPoint, minPoints int) Baseline {
	var (
		b       Baseline
		buckets [168][]float64
	)

	b.Enough = len(points) >= minPoints && minPoints > 0

	for _, p := range points {
		h := hourOfWeek(p.Time)
		buckets[h] = append(buckets[h], p.Value)
	}

	for h := range buckets {
		if len(buckets[h]) == 0 {
			continue
		}

		b.Count[h] = len(buckets[h])
		b.ByHourOfWeek[h] = median(buckets[h])
	}

	return b
}

// Value returns the seasonal baseline for time t and whether it is available
// (enough history and a populated bucket for t's hour-of-week).
func (b Baseline) Value(t time.Time) (float64, bool) {
	if !b.Enough {
		return 0, false
	}

	h := hourOfWeek(t)
	if b.Count[h] == 0 {
		return 0, false
	}

	return b.ByHourOfWeek[h], true
}

// median returns the median of vals (does not mutate the input).
func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}

	s := slices.Clone(vals)
	sort.Float64s(s)

	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}

	return (s[n/2-1] + s[n/2]) / 2
}
