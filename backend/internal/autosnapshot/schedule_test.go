package autosnapshot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

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
