package metrics

import (
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/health"
)

func TestBuildHistory(t *testing.T) {
	healthy := func(at time.Time) Signals { return NewSignals(at) } // empty -> ~100

	bad := func(at time.Time) Signals {
		s := NewSignals(at)
		s.Set(SigChecksumFailRate, 1) // floor -> ~30

		return s
	}

	// Three healthy Sundays (baseline bucket) + one bad Sunday in the display range.
	sigs := []Signals{
		healthy(weeksLater(sun00, 0)),
		healthy(weeksLater(sun00, 1)),
		healthy(weeksLater(sun00, 2)),
		bad(weeksLater(sun00, 3)),
	}

	from := weeksLater(sun00, 3).Add(-time.Hour)
	to := weeksLater(sun00, 3).Add(time.Hour)

	h := buildHistory(sigs, from, to, 3, 10, health.DefaultWeights())

	if len(h.Points) != 1 {
		t.Fatalf("expected 1 displayed point in [from,to], got %d", len(h.Points))
	}

	if !h.BaselineEnough {
		t.Error("expected baseline available (>= minPoints)")
	}

	if h.Points[0].Score > 30 {
		t.Errorf("displayed point should be floored (~30), got %v", h.Points[0].Score)
	}

	if len(h.Dips) != 1 {
		t.Fatalf("expected 1 dip (score below seasonal baseline), got %d", len(h.Dips))
	}

	if h.Points[0].Categories == nil {
		t.Error("expected per-category scores on the point")
	}
}
