package autosnapshot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHotDue(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 7, 19, h, m, 0, 0, time.UTC)
	}

	daily, err := ParseHotSchedule("0 3 * * *")
	require.NoError(t, err)

	// Last capture 02:00 UTC → next firing 03:00 UTC.
	assert.False(t, hotDue(daily, at(2, 0), at(2, 59)))
	assert.True(t, hotDue(daily, at(2, 0), at(3, 0)))
	assert.True(t, hotDue(daily, at(2, 0), at(15, 0)), "missed firings stay due until taken")

	every5, err := ParseHotSchedule("*/5 * * * *")
	require.NoError(t, err)

	assert.False(t, hotDue(every5, at(12, 5), at(12, 9)))
	assert.True(t, hotDue(every5, at(12, 5), at(12, 10)))

	// nil schedule = daily fallback for an unparsable expression.
	assert.False(t, hotDue(nil, at(2, 0), at(2, 0).Add(23*time.Hour)))
	assert.True(t, hotDue(nil, at(2, 0), at(2, 0).Add(24*time.Hour)))
}

func TestParseHotSchedule_Timezone(t *testing.T) {
	// Bare specs are pinned to UTC regardless of the process's local zone:
	// "0 3 * * *" fires at 03:00 UTC even when last is expressed in another zone.
	utcDaily, err := ParseHotSchedule("0 3 * * *")
	require.NoError(t, err)

	msk := time.FixedZone("MSK", 3*3600)
	lastMsk := time.Date(2026, 7, 19, 5, 0, 0, 0, msk) // 02:00 UTC

	next := utcDaily.Next(lastMsk)
	assert.Equal(t, time.Date(2026, 7, 19, 3, 0, 0, 0, time.UTC), next.UTC())

	// An explicit CRON_TZ prefix is passed through untouched: 03:00 Moscow
	// time is 00:00 UTC.
	mskDaily, err := ParseHotSchedule("CRON_TZ=Europe/Moscow 0 3 * * *")
	require.NoError(t, err)

	next = mskDaily.Next(time.Date(2026, 7, 18, 23, 0, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC), next.UTC())
}
