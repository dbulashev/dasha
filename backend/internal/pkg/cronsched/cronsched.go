package cronsched

import (
	"errors"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Parse pins a spec without TZ= / CRON_TZ= to UTC: robfig/cron would use the
// container's time.Local.
func Parse(spec string) (cron.Schedule, error) {
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		spec = "CRON_TZ=UTC " + spec
	} else if !strings.Contains(spec, " ") {
		// robfig/cron v3.0.1 panics slicing a timezone with no following space.
		return nil, errors.New("timezone without schedule expression")
	}

	return cron.ParseStandard(spec)
}

const FallbackInterval = 24 * time.Hour

// Due: a missed firing stays due until taken; a nil sched (unparsable
// expression) is due every FallbackInterval; a schedule that never fires
// (e.g. "0 0 31 2 *", Next returns zero) is never due.
func Due(sched cron.Schedule, last, now time.Time) bool {
	if sched == nil {
		return now.Sub(last) >= FallbackInterval
	}

	next := sched.Next(last)

	return !next.IsZero() && !now.Before(next)
}

const (
	gapResolution = time.Hour
	// Covers a leap-year cycle, so "29 Feb" style schedules are measured too.
	gapHorizon = 4*365*24*time.Hour + 24*time.Hour
)

// HasGap reports whether sched can go d or longer without firing, to within
// gapResolution (a gap slightly shorter than d may also be reported). A
// schedule that never fires has no gap.
func HasGap(sched cron.Schedule, d time.Duration) bool {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(gapHorizon)

	for p := start; p.Before(end); {
		next := sched.Next(p)
		if next.IsZero() {
			return p.After(start)
		}

		if next.Sub(p) > d-gapResolution {
			return true
		}

		// Stepping to the next firing, or by at most gapResolution, leaves
		// every gap with a probe point within gapResolution of its start.
		p = p.Add(gapResolution)
		if next.After(p) {
			p = next
		}
	}

	return false
}
