package autosnapshot

import (
	"time"

	"github.com/robfig/cron/v3"

	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

// dueForCapture reports whether one sweep target is due, recording the attempt
// in attempts. A target with no stored snapshot is judged by its last attempt
// instead — one that never yields a snapshot (an unreachable host, a server
// without the source view) would otherwise be retried on every daemon tick
// rather than once per schedule tick.
func (d *Daemon) dueForCapture(
	attempts map[string]time.Time,
	sched cron.Schedule,
	last map[string]time.Time,
	key string,
	now time.Time,
) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	t, ok := last[key]
	if !ok {
		t, ok = attempts[key]
	}

	if ok && !cronsched.Due(sched, t, now) {
		return false
	}

	attempts[key] = now

	return true
}
