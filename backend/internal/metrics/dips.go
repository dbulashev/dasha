package metrics

import "time"

// Dip is a point where a series deviates from its seasonal baseline beyond the
// configured threshold — a "this dropped below normal at T" annotation.
type Dip struct {
	Time     time.Time `json:"time"`
	Value    float64   `json:"value"`
	Baseline float64   `json:"baseline"`
	Drop     float64   `json:"drop"` // baseline - value (score points)
}

// DetectScoreDips flags points whose score fell below its seasonal baseline by
// more than minDrop points. Returns nil when the baseline is unavailable or the
// threshold is non-positive.
func DetectScoreDips(points []SeriesPoint, b Baseline, minDrop float64) []Dip {
	if !b.Enough || minDrop <= 0 {
		return nil
	}

	var out []Dip

	for _, p := range points {
		base, ok := b.Value(p.Time)
		if !ok {
			continue
		}

		if drop := base - p.Value; drop > minDrop {
			out = append(out, Dip{Time: p.Time, Value: p.Value, Baseline: base, Drop: drop})
		}
	}

	return out
}
