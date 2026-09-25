package mcpserver

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const (
	healthTrendMaxPoints   = 200
	healthTrendFloorPoints = 12
	healthTrendMaxDips     = 10

	// categoryDipPoints is how far a category must sit below its reference to
	// count as having dragged the score down.
	categoryDipPoints = 5.0
	// categoryRefPercentile of a category over the window is its reference.
	categoryRefPercentile = 0.9

	healthTrendNext = "get_health_recommendations for the dip window, then health_details(rule_id); " +
		"points=N for the series"
)

type healthTrendWindow struct {
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	StepSeconds   int       `json:"step_seconds"`
	PeriodSeconds int       `json:"period_seconds"`
	PointsTotal   int       `json:"points_total"`
}

type healthTrendScore struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Avg  float64 `json:"avg"`
	Last float64 `json:"last"`
}

// healthTrendDip is a run of consecutive dip points, reported at its deepest one.
type healthTrendDip struct {
	Start                   time.Time `json:"start"`
	End                     time.Time `json:"end"`
	Points                  int       `json:"points"`
	Time                    time.Time `json:"time"`
	Value                   float64   `json:"value"`
	Baseline                float64   `json:"baseline"`
	Drop                    float64   `json:"drop"`
	WorstCategory           string    `json:"worst_category,omitempty"`
	CategoriesBelowBaseline []string  `json:"categories_below_baseline,omitempty"`
}

type healthTrendPeriod struct {
	Start         time.Time `json:"start"`
	Min           float64   `json:"min"`
	WorstCategory string    `json:"worst_category,omitempty"`
}

type healthTrendBaseline struct {
	Available bool     `json:"available"`
	Min       *float64 `json:"min,omitempty"`
	Avg       *float64 `json:"avg,omitempty"`
}

type healthTrendPoint struct {
	Time       time.Time          `json:"time"`
	Score      float64            `json:"score"`
	Baseline   *float64           `json:"baseline,omitempty"`
	LatencyMs  float64            `json:"latency_ms"`
	Categories map[string]float64 `json:"categories"`
}

type healthTrendResult struct {
	Window                 healthTrendWindow   `json:"window"`
	Score                  *healthTrendScore   `json:"score,omitempty"`
	Dips                   []healthTrendDip    `json:"dips"`
	DipsTotal              int                 `json:"dips_total"`
	Periods                []healthTrendPeriod `json:"periods"`
	Baseline               healthTrendBaseline `json:"baseline"`
	CategoryRefUnavailable bool                `json:"category_reference_unavailable,omitempty"`
	Points                 []healthTrendPoint  `json:"points,omitempty"`
	Next                   string              `json:"next"`

	series     []apiclient.HealthScoreHistoryPoint
	baselineAt map[time.Time]float64
}

func healthTrend(ctx context.Context, c *DashaClient, a healthTrendArgs) (any, error) {
	span, step := trendWindow(a.Range)
	to := time.Now()
	from := to.Add(-span)

	h, err := c.HealthTrend(ctx, a.Cluster, a.Instance, from, to, step)
	if err != nil {
		return nil, err
	}

	return buildHealthTrend(h, from, to, step, periodFor(span), a.Points), nil
}

func periodFor(span time.Duration) time.Duration {
	switch {
	case span <= 24*time.Hour:
		return time.Hour
	case span <= 7*24*time.Hour:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func buildHealthTrend(
	h *apiclient.HealthScoreHistory, from, to time.Time, step int, period time.Duration, points int,
) *healthTrendResult {
	series := h.Points

	baselineAt := make(map[time.Time]float64, len(h.Baseline))
	for _, b := range h.Baseline {
		baselineAt[b.Time.UTC()] = b.Value
	}

	refs := categoryReferences(series, h.Dips)

	out := &healthTrendResult{ //nolint:exhaustruct
		Window: healthTrendWindow{
			From: from.UTC(), To: to.UTC(), StepSeconds: step,
			PeriodSeconds: int(period / time.Second), PointsTotal: len(series),
		},
		Score:      scoreStats(series),
		Periods:    trendPeriods(series, period, refs),
		Baseline:   baselineStats(h),
		Next:       healthTrendNext,
		series:     series,
		baselineAt: baselineAt,
	}

	out.Dips, out.DipsTotal = dipRuns(series, h.Dips, refs)
	out.CategoryRefUnavailable = len(series) > 0 && len(refs) == 0

	if points > 0 {
		out.Points = out.decimate(points)
	}

	return out
}

func scoreStats(series []apiclient.HealthScoreHistoryPoint) *healthTrendScore {
	if len(series) == 0 {
		return nil
	}

	s := healthTrendScore{Min: math.Inf(1), Max: math.Inf(-1), Last: series[len(series)-1].Score} //nolint:exhaustruct
	sum := 0.0

	for _, p := range series {
		s.Min = min(s.Min, p.Score)
		s.Max = max(s.Max, p.Score)
		sum += p.Score
	}

	s.Avg = round1(sum / float64(len(series)))

	return &s
}

func baselineStats(h *apiclient.HealthScoreHistory) healthTrendBaseline {
	out := healthTrendBaseline{Available: h.BaselineAvailable} //nolint:exhaustruct
	if !h.BaselineAvailable || len(h.Baseline) == 0 {
		return out
	}

	lo, sum := math.Inf(1), 0.0
	for _, b := range h.Baseline {
		lo = min(lo, b.Value)
		sum += b.Value
	}

	avg := round1(sum / float64(len(h.Baseline)))
	out.Min, out.Avg = &lo, &avg

	return out
}

// categoryReferences is the per-category reference over the points outside
// dips: the API carries a seasonal baseline for the total score only.
func categoryReferences(
	series []apiclient.HealthScoreHistoryPoint, dips []apiclient.HealthScoreHistoryDip,
) map[string]float64 {
	inDip := make(map[time.Time]bool, len(dips))
	for _, d := range dips {
		inDip[d.Time.UTC()] = true
	}

	vals := map[string][]float64{}

	for _, p := range series {
		if inDip[p.Time.UTC()] {
			continue
		}

		for k, v := range p.Categories {
			vals[k] = append(vals[k], v)
		}
	}

	out := make(map[string]float64, len(vals))
	for k, v := range vals {
		slices.Sort(v)
		out[k] = v[int(math.Ceil(categoryRefPercentile*float64(len(v))))-1]
	}

	return out
}

// categoriesBelow lists the categories of p more than categoryDipPoints under
// their reference, deepest first.
func categoriesBelow(p apiclient.HealthScoreHistoryPoint, refs map[string]float64) []string {
	type drop struct {
		name string
		by   float64
	}

	var drops []drop

	for k, v := range p.Categories {
		if by := refs[k] - v; by > categoryDipPoints {
			drops = append(drops, drop{k, by})
		}
	}

	slices.SortFunc(drops, func(a, b drop) int {
		return cmp.Or(cmp.Compare(b.by, a.by), cmp.Compare(a.name, b.name))
	})

	out := make([]string, len(drops))
	for i, d := range drops {
		out[i] = d.name
	}

	return out
}

func worstCategory(p apiclient.HealthScoreHistoryPoint, refs map[string]float64) string {
	if below := categoriesBelow(p, refs); len(below) > 0 {
		return below[0]
	}

	return ""
}

func trendPeriods(
	series []apiclient.HealthScoreHistoryPoint, period time.Duration, refs map[string]float64,
) []healthTrendPeriod {
	out := []healthTrendPeriod{}

	var lowest apiclient.HealthScoreHistoryPoint

	flush := func() {
		out[len(out)-1].Min = lowest.Score
		out[len(out)-1].WorstCategory = worstCategory(lowest, refs)
	}

	for _, p := range series {
		start := p.Time.UTC().Truncate(period)

		if len(out) == 0 || !out[len(out)-1].Start.Equal(start) {
			if len(out) > 0 {
				flush()
			}

			out = append(out, healthTrendPeriod{Start: start}) //nolint:exhaustruct
			lowest = p
		} else if p.Score < lowest.Score {
			lowest = p
		}
	}

	if len(out) > 0 {
		flush()
	}

	return out
}

// dipRuns folds the per-point dips of consecutive points into runs and keeps
// the healthTrendMaxDips deepest, in time order. It returns the run count.
func dipRuns(
	series []apiclient.HealthScoreHistoryPoint, dips []apiclient.HealthScoreHistoryDip, refs map[string]float64,
) ([]healthTrendDip, int) {
	index := make(map[time.Time]int, len(series))
	for i, p := range series {
		index[p.Time.UTC()] = i
	}

	sorted := slices.Clone(dips)
	slices.SortFunc(sorted, func(a, b apiclient.HealthScoreHistoryDip) int { return a.Time.Compare(b.Time) })

	var (
		runs    []healthTrendDip
		deepest []int
		prev    = -2
	)

	for _, d := range sorted {
		i, ok := index[d.Time.UTC()]
		if !ok {
			continue
		}

		if i != prev+1 || len(runs) == 0 {
			runs = append(runs, healthTrendDip{Start: d.Time.UTC()}) //nolint:exhaustruct
			deepest = append(deepest, i)
		}

		r := &runs[len(runs)-1]
		r.End = d.Time.UTC()
		r.Points++

		if r.Points == 1 || d.Drop > r.Drop {
			r.Time, r.Value, r.Baseline, r.Drop = d.Time.UTC(), d.Value, d.Baseline, d.Drop
			deepest[len(deepest)-1] = i
		}

		prev = i
	}

	for k := range runs {
		p := series[deepest[k]]
		runs[k].CategoriesBelowBaseline = categoriesBelow(p, refs)
		runs[k].WorstCategory = worstCategory(p, refs)
	}

	total := len(runs)
	if total > healthTrendMaxDips {
		slices.SortFunc(runs, func(a, b healthTrendDip) int { return cmp.Compare(b.Drop, a.Drop) })
		runs = runs[:healthTrendMaxDips]
		slices.SortFunc(runs, func(a, b healthTrendDip) int { return a.Start.Compare(b.Start) })
	}

	if runs == nil {
		runs = []healthTrendDip{}
	}

	return runs, total
}

// decimate splits the series into n buckets and keeps the lowest-score point of
// each.
func (r *healthTrendResult) decimate(n int) []healthTrendPoint {
	total := len(r.series)
	n = min(n, total)
	out := make([]healthTrendPoint, 0, n)

	for b := range n {
		lo, hi := b*total/n, (b+1)*total/n
		best := r.series[lo]

		for _, p := range r.series[lo+1 : hi] {
			if p.Score < best.Score {
				best = p
			}
		}

		pt := healthTrendPoint{ //nolint:exhaustruct
			Time: best.Time.UTC(), Score: best.Score, LatencyMs: best.LatencyMs, Categories: best.Categories,
		}

		if v, ok := r.baselineAt[best.Time.UTC()]; ok {
			pt.Baseline = &v
		}

		out = append(out, pt)
	}

	return out
}

func (r *healthTrendResult) shrink() (shapedResult, string, bool) {
	if r.Points == nil {
		return nil, "a shorter range", false
	}

	n := len(r.Points) / 2
	if n < healthTrendFloorPoints {
		return nil, "fewer points, or a shorter range", false
	}

	next := *r
	next.Points = r.decimate(n)

	return &next, "points=" + strconv.Itoa(n), true
}

func (r *healthTrendResult) note() *shapeNote {
	total := r.Window.PointsTotal
	full := "points=" + strconv.Itoa(min(total, healthTrendMaxPoints))

	switch {
	case r.Points == nil && total > 0:
		return &shapeNote{ //nolint:exhaustruct
			Reason: shapeDefaultView, Folded: "per-point series", Total: total, Full: full,
		}
	case r.Points != nil && len(r.Points) < total:
		return &shapeNote{ //nolint:exhaustruct
			Reason: shapeDefaultView,
			Folded: "series decimated to " + strconv.Itoa(len(r.Points)) + " buckets, lowest point of each",
			Total:  total,
			Full:   full,
		}
	default:
		return nil
	}
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
