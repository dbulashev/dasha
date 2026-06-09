package metrics

import (
	"testing"
	"time"
)

// sun00 is Sunday 2023-01-01 00:00 UTC -> hour-of-week bucket 0.
var sun00 = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

func weeksLater(base time.Time, n int) time.Time {
	return base.AddDate(0, 0, 7*n)
}

func TestHourOfWeek(t *testing.T) {
	if got := hourOfWeek(sun00); got != 0 {
		t.Errorf("Sunday 00:00 should be bucket 0, got %d", got)
	}

	// Monday 09:00 = weekday 1 * 24 + 9 = 33.
	mon09 := time.Date(2023, 1, 2, 9, 0, 0, 0, time.UTC)
	if got := hourOfWeek(mon09); got != 33 {
		t.Errorf("Monday 09:00 should be bucket 33, got %d", got)
	}
}

func TestBuildBaseline_MedianPerBucket(t *testing.T) {
	points := []SeriesPoint{
		{Time: weeksLater(sun00, 0), Value: 30},
		{Time: weeksLater(sun00, 1), Value: 10},
		{Time: weeksLater(sun00, 2), Value: 20},
	}

	b := BuildBaseline(points, 3)
	if !b.Enough {
		t.Fatal("expected Enough with >= minPoints samples")
	}

	v, ok := b.Value(weeksLater(sun00, 5)) // another Sunday 00:00
	if !ok || v != 20 {
		t.Errorf("baseline median for bucket 0 should be 20, got v=%v ok=%v", v, ok)
	}

	// Empty bucket (Sunday 01:00) -> unavailable.
	if _, ok := b.Value(sun00.Add(time.Hour)); ok {
		t.Error("empty bucket should yield no baseline")
	}
}

func TestBuildBaseline_NotEnoughHistory(t *testing.T) {
	points := []SeriesPoint{{Time: sun00, Value: 50}}

	b := BuildBaseline(points, 100) // require far more than we have
	if b.Enough {
		t.Error("baseline should not be Enough below minPoints")
	}

	if _, ok := b.Value(sun00); ok {
		t.Error("baseline must be unavailable when not Enough")
	}
}

func TestDetectScoreDips(t *testing.T) {
	b := BuildBaseline([]SeriesPoint{
		{Time: weeksLater(sun00, 0), Value: 90},
		{Time: weeksLater(sun00, 1), Value: 90},
		{Time: weeksLater(sun00, 2), Value: 90},
	}, 3)

	points := []SeriesPoint{
		{Time: weeksLater(sun00, 3), Value: 70}, // drop 20 > 10 -> dip
		{Time: weeksLater(sun00, 4), Value: 85}, // drop 5 < 10 -> no dip
	}

	dips := DetectScoreDips(points, b, 10)
	if len(dips) != 1 {
		t.Fatalf("expected 1 dip, got %d", len(dips))
	}

	if dips[0].Value != 70 || dips[0].Baseline != 90 {
		t.Errorf("unexpected dip: %+v", dips[0])
	}
}

func TestDetectScoreDips_NoBaseline(t *testing.T) {
	b := BuildBaseline([]SeriesPoint{{Time: sun00, Value: 90}}, 100) // not Enough

	if d := DetectScoreDips([]SeriesPoint{{Time: sun00, Value: 10}}, b, 10); d != nil {
		t.Errorf("no dips expected without a baseline, got %v", d)
	}
}
