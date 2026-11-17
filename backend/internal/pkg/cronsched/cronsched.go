// Package cronsched parses the 5-field cron expressions used by Dasha's
// schedules (autosnapshot hot/io captures, the PII catalog) and decides whether
// a schedule is due.
package cronsched

import (
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Parse parses the cron spec pinned to UTC unless the user names a zone
// explicitly. robfig/cron defaults a spec without a TZ prefix to the process's
// time.Local — 03:00 would then mean whatever zone the backend container
// happens to run in. UTC is the deterministic default; a local-time schedule is
// spelled "CRON_TZ=Europe/Moscow 0 3 * * *".
func Parse(spec string) (cron.Schedule, error) {
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		spec = "CRON_TZ=UTC " + spec
	}

	return cron.ParseStandard(spec)
}

// Due reports whether sched has fired between last and now. A firing missed
// while nothing ran stays due until taken.
func Due(sched cron.Schedule, last, now time.Time) bool {
	return !now.Before(sched.Next(last))
}
