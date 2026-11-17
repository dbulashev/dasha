package cronsched

import (
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Parse pins a spec without TZ= / CRON_TZ= to UTC: robfig/cron would use the
// container's time.Local.
func Parse(spec string) (cron.Schedule, error) {
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		spec = "CRON_TZ=UTC " + spec
	}

	return cron.ParseStandard(spec)
}

const FallbackInterval = 24 * time.Hour

// Due: a missed firing stays due until taken; a nil sched (unparsable
// expression) is due every FallbackInterval.
func Due(sched cron.Schedule, last, now time.Time) bool {
	if sched == nil {
		return now.Sub(last) >= FallbackInterval
	}

	return !now.Before(sched.Next(last))
}
