package mcpserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
)

var trendT0 = time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

func trendCats(storage float64) map[string]float64 {
	return map[string]float64{"storage": storage, "maintenance": 90, "locks": 95}
}

// trendFixture is 24h at 5-minute steps, score 92 everywhere except a dip at
// 09:00–09:10 where storage falls to 40 and the score to 41.
func trendFixture() *apiclient.HealthScoreHistory {
	h := &apiclient.HealthScoreHistory{BaselineAvailable: true} //nolint:exhaustruct

	for i := range 288 {
		t := trendT0.Add(time.Duration(i) * 5 * time.Minute)
		score, storage := 92.0, 90.0

		switch t.Sub(trendT0) {
		case 9 * time.Hour:
			score, storage = 60, 70
		case 9*time.Hour + 5*time.Minute:
			score, storage = 41, 40
		case 9*time.Hour + 10*time.Minute:
			score, storage = 70, 75
		}

		h.Points = append(h.Points, apiclient.HealthScoreHistoryPoint{Time: t, Score: score, Categories: trendCats(storage)}) //nolint:exhaustruct
		h.Baseline = append(h.Baseline, apiclient.HealthScoreHistoryBaseline{Time: t, Value: 92})

		if score < 80 {
			h.Dips = append(h.Dips, apiclient.HealthScoreHistoryDip{Time: t, Value: score, Baseline: 92, Drop: 92 - score})
		}
	}

	return h
}

func buildFixtureTrend(points int) *healthTrendResult {
	return buildHealthTrend(trendFixture(), trendT0, trendT0.Add(24*time.Hour), 300, time.Hour, points)
}

func TestCategoryReferences_LongDegradation(t *testing.T) {
	t.Parallel()

	series := make([]apiclient.HealthScoreHistoryPoint, 0, 24)
	for i := range 24 {
		storage := 90.0
		if i >= 6 && i < 23 {
			storage = 40
		}

		series = append(series, apiclient.HealthScoreHistoryPoint{Categories: trendCats(storage)}) //nolint:exhaustruct
	}

	refs := categoryReferences(series)
	if refs["storage"] != 90 {
		t.Errorf("storage reference = %v, want its healthy level 90", refs["storage"])
	}

	if got := categoriesBelow(series[10], refs); !slices.Equal(got, []string{"storage"}) {
		t.Errorf("categories below = %v", got)
	}
}

func TestHealthTrend_SummaryFindsDip(t *testing.T) {
	t.Parallel()

	r := buildFixtureTrend(0)

	if r.Points != nil {
		t.Fatalf("default view must not carry the series, got %d points", len(r.Points))
	}

	if r.Score == nil || r.Score.Min != 41 || r.Score.Max != 92 || r.Score.Last != 92 {
		t.Errorf("score = %+v", r.Score)
	}

	if r.DipsTotal != 1 || len(r.Dips) != 1 {
		t.Fatalf("consecutive dip points must fold into one run, got %d (%+v)", r.DipsTotal, r.Dips)
	}

	d := r.Dips[0]
	if d.Points != 3 || d.Value != 41 || !d.Time.Equal(trendT0.Add(9*time.Hour+5*time.Minute)) {
		t.Errorf("dip must report its deepest point: %+v", d)
	}

	if !d.Start.Equal(trendT0.Add(9*time.Hour)) || !d.End.Equal(trendT0.Add(9*time.Hour+10*time.Minute)) {
		t.Errorf("dip span = %s..%s", d.Start, d.End)
	}

	if d.WorstCategory != "storage" || !slices.Equal(d.CategoriesBelowBaseline, []string{"storage"}) {
		t.Errorf("categories below baseline = %v, worst = %q", d.CategoriesBelowBaseline, d.WorstCategory)
	}

	if len(r.Periods) != 24 {
		t.Fatalf("periods = %d, want 24 hourly", len(r.Periods))
	}

	for i, p := range r.Periods {
		want, worst := 92.0, ""
		if i == 9 {
			want, worst = 41, "storage"
		}

		if p.Min != want || p.WorstCategory != worst {
			t.Errorf("period %d = %+v, want min %v worst %q", i, p, want, worst)
		}
	}

	if !r.Baseline.Available || r.Baseline.Min == nil || *r.Baseline.Min != 92 {
		t.Errorf("baseline = %+v", r.Baseline)
	}

	n := r.note()
	if n == nil || n.Reason != shapeDefaultView || n.Total != 288 || n.Full != "points=200" {
		t.Errorf("note = %+v", n)
	}
}

func TestHealthTrend_SeparateRunsAndCap(t *testing.T) {
	t.Parallel()

	h := trendFixture()
	h.Dips = nil

	for i := range 15 {
		p := h.Points[i*10]
		h.Dips = append(h.Dips, apiclient.HealthScoreHistoryDip{Time: p.Time, Value: 50, Baseline: 92, Drop: float64(20 + i)})
	}

	r := buildHealthTrend(h, trendT0, trendT0.Add(24*time.Hour), 300, time.Hour, 0)

	if r.DipsTotal != 15 || len(r.Dips) != healthTrendMaxDips {
		t.Fatalf("dips = %d of %d, want %d of 15", len(r.Dips), r.DipsTotal, healthTrendMaxDips)
	}

	if r.Dips[0].Drop != 25 {
		t.Errorf("the shallowest runs must be dropped first, first kept drop = %v", r.Dips[0].Drop)
	}

	if !slices.IsSortedFunc(r.Dips, func(a, b healthTrendDip) int { return a.Start.Compare(b.Start) }) {
		t.Errorf("kept dips must stay in time order")
	}
}

func TestHealthTrend_DecimateKeepsBucketMinimum(t *testing.T) {
	t.Parallel()

	r := buildFixtureTrend(24)

	if len(r.Points) != 24 {
		t.Fatalf("points = %d, want 24", len(r.Points))
	}

	p := r.Points[9]
	if p.Score != 41 || !p.Time.Equal(trendT0.Add(9*time.Hour+5*time.Minute)) {
		t.Errorf("bucket 9 = %+v, want the 41 point", p)
	}

	if p.Categories["storage"] != 40 || p.Baseline == nil || *p.Baseline != 92 {
		t.Errorf("decimated point must carry its categories and baseline: %+v", p)
	}

	if !r.Points[0].Time.Equal(trendT0) || !r.Points[23].Time.Equal(trendT0.Add(23*time.Hour)) {
		t.Errorf("decimation must cover the whole window: %s..%s", r.Points[0].Time, r.Points[23].Time)
	}
}

func TestHealthTrend_PointsAboveSeriesReturnsAll(t *testing.T) {
	t.Parallel()

	h := trendFixture()
	h.Points = h.Points[:10]

	r := buildHealthTrend(h, trendT0, trendT0.Add(time.Hour), 300, time.Hour, 200)

	if len(r.Points) != 10 || r.note() != nil {
		t.Errorf("points = %d, note = %+v; want the full series and no note", len(r.Points), r.note())
	}
}

func TestHealthTrend_ShrinkHalvesPointsNotWindow(t *testing.T) {
	t.Parallel()

	r := buildFixtureTrend(200)
	next, step, ok := r.shrink()

	if !ok || step != "points=100" {
		t.Fatalf("shrink = %q, %v", step, ok)
	}

	s, _ := next.(*healthTrendResult)
	if len(s.Points) != 100 {
		t.Fatalf("points = %d, want 100", len(s.Points))
	}

	if !s.Points[0].Time.Equal(trendT0) || s.Points[99].Time.Before(trendT0.Add(23*time.Hour)) {
		t.Errorf("shrink must keep the window: %s..%s", s.Points[0].Time, s.Points[99].Time)
	}

	if !slices.ContainsFunc(s.Points, func(p healthTrendPoint) bool { return p.Score == 41 }) {
		t.Errorf("shrink lost the dip minimum")
	}

	if len(r.Points) != 200 {
		t.Errorf("shrink must not mutate the receiver")
	}
}

func TestHealthTrend_ShrinkStops(t *testing.T) {
	t.Parallel()

	if _, hint, ok := buildFixtureTrend(0).shrink(); ok || hint == "" {
		t.Errorf("the summary has nothing to shrink: ok=%v hint=%q", ok, hint)
	}

	if _, hint, ok := buildFixtureTrend(healthTrendFloorPoints + 1).shrink(); ok || !strings.Contains(hint, "points") {
		t.Errorf("shrink below the floor: ok=%v hint=%q", ok, hint)
	}
}

func TestHealthTrend_BudgetShrinksThroughRender(t *testing.T) {
	t.Parallel()

	ctx, rec := budgetCtx(24 << 10)

	res, _, _ := renderResult(ctx, buildFixtureTrend(200), nil)
	if res.IsError {
		t.Fatalf("refused: %s", contentText(res.Content[0]))
	}

	if len(rec.steps) == 0 {
		t.Fatalf("a 200-point series over a 24 KB budget must shrink")
	}

	var body healthTrendResult
	if err := json.Unmarshal([]byte(contentText(res.Content[len(res.Content)-1])), &body); err != nil {
		t.Fatal(err)
	}

	if body.Window.PointsTotal != 288 || len(body.Points) >= 200 {
		t.Errorf("window = %+v, points = %d", body.Window, len(body.Points))
	}

	if !strings.Contains(contentText(res.Content[0]), `"reason":"budget"`) {
		t.Errorf("note = %s", contentText(res.Content[0]))
	}
}

func TestHealthTrend_Empty(t *testing.T) {
	t.Parallel()

	h := &apiclient.HealthScoreHistory{} //nolint:exhaustruct
	r := buildHealthTrend(h, trendT0, trendT0.Add(24*time.Hour), 300, time.Hour, 50)

	if r.Score != nil || len(r.Periods) != 0 || len(r.Dips) != 0 || len(r.Points) != 0 || r.note() != nil {
		t.Errorf("empty series = %+v", r)
	}

	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), `"dips":[]`) || !strings.Contains(string(b), `"periods":[]`) {
		t.Errorf("empty series must still encode arrays: %s %v", b, err)
	}
}
