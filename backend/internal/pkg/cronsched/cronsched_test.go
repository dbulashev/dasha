package cronsched

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDue(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 7, 19, h, m, 0, 0, time.UTC)
	}

	daily, err := Parse("0 3 * * *")
	require.NoError(t, err)

	assert.False(t, Due(daily, at(2, 0), at(2, 59)))
	assert.True(t, Due(daily, at(2, 0), at(3, 0)))
	assert.True(t, Due(daily, at(2, 0), at(15, 0)), "missed firings stay due until taken")

	every5, err := Parse("*/5 * * * *")
	require.NoError(t, err)

	assert.False(t, Due(every5, at(12, 5), at(12, 9)))
	assert.True(t, Due(every5, at(12, 5), at(12, 10)))

	assert.False(t, Due(nil, at(2, 0), at(2, 0).Add(FallbackInterval-time.Second)))
	assert.True(t, Due(nil, at(2, 0), at(2, 0).Add(FallbackInterval)))
}

func TestParse_Timezone(t *testing.T) {
	utcDaily, err := Parse("0 3 * * *")
	require.NoError(t, err)

	msk := time.FixedZone("MSK", 3*3600)
	lastMsk := time.Date(2026, 7, 19, 5, 0, 0, 0, msk) // 02:00 UTC

	next := utcDaily.Next(lastMsk)
	assert.Equal(t, time.Date(2026, 7, 19, 3, 0, 0, 0, time.UTC), next.UTC())

	mskDaily, err := Parse("CRON_TZ=Europe/Moscow 0 3 * * *")
	require.NoError(t, err)

	next = mskDaily.Next(time.Date(2026, 7, 18, 23, 0, 0, 0, time.UTC))
	assert.Equal(t, time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC), next.UTC())
}

func TestParse_Invalid(t *testing.T) {
	for _, spec := range []string{"", "bad", "0 3 * *", "61 * * * *", "CRON_TZ=Nowhere/Land 0 3 * * *"} {
		_, err := Parse(spec)
		assert.Error(t, err, spec)
	}
}
