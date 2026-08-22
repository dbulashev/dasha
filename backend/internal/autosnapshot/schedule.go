package autosnapshot

import (
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ParseCronSchedule parses the cron spec pinned to UTC unless the user names a
// zone explicitly. robfig/cron defaults a spec without a TZ prefix to the
// process's time.Local — 03:00 would then mean whatever zone the backend
// container happens to run in. UTC is the deterministic default; a local-time
// schedule is spelled "CRON_TZ=Europe/Moscow 0 3 * * *".
func ParseCronSchedule(spec string) (cron.Schedule, error) {
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		spec = "CRON_TZ=UTC " + spec
	}

	return cron.ParseStandard(spec)
}

// cronDue reports whether the schedule has fired between the last capture and
// now (nil schedule = the daily fallback an unparsable expression falls back to).
func cronDue(sched cron.Schedule, last, now time.Time) bool {
	if sched == nil {
		return now.Sub(last) >= 24*time.Hour
	}

	return !now.Before(sched.Next(last))
}
