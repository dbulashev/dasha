package http

import "time"

const (
	defaultHistoryPoints = 200
	maxHistoryPoints     = 1000

	// The bodies of a window are read per bucket, but its capture headers are
	// read whole — hence a cap on how long a window may be.
	maxHistoryWindow = 31 * 24 * time.Hour
)

// historyWindow bounds the requested period. An inverted one collapses to empty
// rather than being read as "everything".
func historyWindow(from, to time.Time) (time.Time, time.Time) {
	if to.Before(from) {
		return to, to
	}

	if to.Sub(from) > maxHistoryWindow {
		from = to.Add(-maxHistoryWindow)
	}

	return from, to
}

func historyPoints(p *int) int {
	if p == nil || *p <= 0 {
		return defaultHistoryPoints
	}

	if *p > maxHistoryPoints {
		return maxHistoryPoints
	}

	return *p
}
