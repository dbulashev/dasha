package metrics

import (
	"context"
	"time"

	"github.com/dbulashev/dasha/internal/health"
)

// ScorePoint is one timestamp of the score trend.
type ScorePoint struct {
	Time       time.Time          `json:"time"`
	Score      float64            `json:"score"`
	Categories map[string]float64 `json:"categories"` // category name -> score
	LatencyMs  float64            `json:"latency_ms"`
}

// History is the trend payload: the displayed score series, the seasonal
// baseline aligned to it, and detected dips.
type History struct {
	Points         []ScorePoint  `json:"points"`
	Baseline       []SeriesPoint `json:"baseline"`
	Dips           []Dip         `json:"dips"`
	BaselineEnough bool          `json:"baseline_available"`
}

// History computes the score trend for [from, to]. The seasonal baseline is
// built over the configured (longer) history window ending at `to`, so dips on
// the displayed range are judged against the full seasonal shape.
func (s *Service) History(
	ctx context.Context,
	cluster, instance string,
	from, to time.Time,
	step time.Duration,
	w health.Weights,
) (History, error) {
	if step <= 0 {
		step = 5 * time.Minute
	}

	histStart := to.Add(-s.cfg.Baseline.Window)
	if from.Before(histStart) {
		histStart = from
	}

	sigs, err := s.Collector().Range(ctx, cluster, instance, Range{Start: histStart, End: to, Step: step})
	if err != nil {
		return History{}, err
	}

	// Ceiling division, clamped to at least one point — otherwise a step coarser
	// than min_history would yield 0 and Enough could never become true.
	minPoints := max(1, int((s.cfg.Baseline.MinHistory+step-1)/step))

	return buildHistory(sigs, from, to, minPoints, s.cfg.Dips.ScorePoints, w), nil
}

// buildHistory is the pure trend computation: score each timestamp, build the
// seasonal baseline over the full series, then slice the displayed [from, to]
// window and detect dips against the baseline.
func buildHistory(sigs []Signals, from, to time.Time, minPoints int, scoreDipPts float64, w health.Weights) History {
	// Seasonal baselines over the same window — drive the per-point regression
	// penalties (free here, no extra query).
	latSeries := make([]SeriesPoint, 0, len(sigs))
	seqSeries := make([]SeriesPoint, 0, len(sigs))

	for _, sig := range sigs {
		if lat, ok := sig.Get(SigLatencyMs); ok {
			latSeries = append(latSeries, SeriesPoint{Time: sig.At, Value: lat})
		}

		if v, ok := sig.Get(SigSeqScanRate); ok {
			seqSeries = append(seqSeries, SeriesPoint{Time: sig.At, Value: v})
		}
	}

	latBase := BuildBaseline(latSeries, minPoints)
	seqBase := BuildBaseline(seqSeries, minPoints)

	scoreSeries := make([]SeriesPoint, 0, len(sigs))
	full := make([]ScorePoint, 0, len(sigs))

	for _, sig := range sigs {
		lb, _ := latBase.Value(sig.At)
		sb, _ := seqBase.Value(sig.At)
		res := ScoreFromSignalsBase(sig, w, Baselines{Latency: lb, SeqScan: sb})

		cats := make(map[string]float64, len(res.Categories))
		for _, c := range res.Categories {
			cats[string(c.Name)] = c.Score
		}

		lat, _ := sig.Get(SigLatencyMs)

		full = append(full, ScorePoint{Time: sig.At, Score: res.Score, Categories: cats, LatencyMs: lat})
		scoreSeries = append(scoreSeries, SeriesPoint{Time: sig.At, Value: res.Score})
	}

	base := BuildBaseline(scoreSeries, minPoints)

	disp := make([]ScorePoint, 0, len(full))
	dispScore := make([]SeriesPoint, 0, len(full))
	baseline := make([]SeriesPoint, 0, len(full))

	for i, p := range full {
		if p.Time.Before(from) || p.Time.After(to) {
			continue
		}

		disp = append(disp, p)
		dispScore = append(dispScore, scoreSeries[i])

		if bv, ok := base.Value(p.Time); ok {
			baseline = append(baseline, SeriesPoint{Time: p.Time, Value: bv})
		}
	}

	return History{
		Points:         disp,
		Baseline:       baseline,
		Dips:           DetectScoreDips(dispScore, base, scoreDipPts),
		BaselineEnough: base.Enough,
	}
}
