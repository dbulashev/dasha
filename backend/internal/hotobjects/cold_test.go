package hotobjects

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestColdStatusOf(t *testing.T) {
	window := 7 * 24 * time.Hour
	captured := ts(0).Add(30 * 24 * time.Hour)

	assert.Equal(t, ColdStandby, ColdStatusOf(captured, nil, window))
	assert.Equal(t, ColdWarmingUp, ColdStatusOf(captured, ptr(captured.Add(-window+time.Second)), window))
	assert.Equal(t, ColdAvailable, ColdStatusOf(captured, ptr(captured.Add(-window)), window))
}

func TestColdSetsFor(t *testing.T) {
	sets := ColdSets{ //nolint:exhaustruct
		WindowDays: 7,
		ByDatabase: map[string]ColdSet{"app": {Database: "app", Status: ColdAvailable}}, //nolint:exhaustruct
	}

	assert.Equal(t, ColdAvailable, sets.For("app").Status)
	assert.Equal(t, ColdStale, sets.For("other").Status)
	assert.Equal(t, "other", sets.For("other").Database)

	sets.Status = ColdDisabled
	assert.Equal(t, ColdDisabled, sets.For("app").Status)
}
