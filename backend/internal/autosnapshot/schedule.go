package autosnapshot

import (
	"time"

	"github.com/robfig/cron/v3"

	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

// dueForCapture reports whether one sweep target is due, recording the attempt
// in attempts. The target is judged by the newer of its stored snapshot and its
// last attempt — one that keeps failing to store (an unreachable host, a server
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
	if a, tried := attempts[key]; tried && (!ok || a.After(t)) {
		t, ok = a, true
	}

	if ok && !cronsched.Due(sched, t, now) {
		return false
	}

	attempts[key] = now

	return true
}
