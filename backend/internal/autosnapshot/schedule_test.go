package autosnapshot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

func TestCronDue(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 7, 19, h, m, 0, 0, time.UTC)
	}

	daily, err := cronsched.Parse("0 3 * * *")
	require.NoError(t, err)

	// Last capture 02:00 UTC → next firing 03:00 UTC.
	assert.False(t, cronDue(daily, at(2, 0), at(2, 59)))
	assert.True(t, cronDue(daily, at(2, 0), at(3, 0)))
	assert.True(t, cronDue(daily, at(2, 0), at(15, 0)), "missed firings stay due until taken")

	every5, err := cronsched.Parse("*/5 * * * *")
	require.NoError(t, err)

	assert.False(t, cronDue(every5, at(12, 5), at(12, 9)))
	assert.True(t, cronDue(every5, at(12, 5), at(12, 10)))

	// nil schedule = daily fallback for an unparsable expression.
	assert.False(t, cronDue(nil, at(2, 0), at(2, 0).Add(23*time.Hour)))
	assert.True(t, cronDue(nil, at(2, 0), at(2, 0).Add(24*time.Hour)))
}

func TestDueForCapture(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 7, 19, h, m, 0, 0, time.UTC)
	}

	sched, err := cronsched.Parse("*/5 * * * *")
	require.NoError(t, err)

	d := &Daemon{} //nolint:exhaustruct
	attempts := map[string]time.Time{}
	last := map[string]time.Time{"c1/db1": at(3, 0)}

	// A stored snapshot still governs.
	assert.False(t, d.dueForCapture(attempts, sched, last, "c1/db1", at(3, 2)))
	assert.True(t, d.dueForCapture(attempts, sched, last, "c1/db1", at(3, 5)))

	// Without one the first attempt fires, and the next waits for the schedule
	// instead of repeating on every tick.
	assert.True(t, d.dueForCapture(attempts, sched, last, "c1/db2", at(3, 6)))
	assert.False(t, d.dueForCapture(attempts, sched, last, "c1/db2", at(3, 7)))
	assert.True(t, d.dueForCapture(attempts, sched, last, "c1/db2", at(3, 10)))
}
