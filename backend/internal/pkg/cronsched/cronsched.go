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
