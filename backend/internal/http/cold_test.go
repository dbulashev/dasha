package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

func TestColdStatusToAPI(t *testing.T) {
	captured := time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)
	observed := captured.Add(-3*24*time.Hour - time.Hour)

	available := coldStatusToAPI(hotobjects.ColdSet{ //nolint:exhaustruct
		Status: hotobjects.ColdAvailable,
		Tables: []hotobjects.ColdTable{{Schema: "public", Table: "a"}, {Schema: "public", Table: "b"}}, //nolint:exhaustruct
	}, 7)
	assert.Equal(t, serverhttp.ColdTablesStatusStatus("available"), available.Status)
	assert.Equal(t, 7, available.WindowDays)
	require.NotNil(t, available.Count)
	assert.Equal(t, 2, *available.Count)
	assert.Nil(t, available.ObservedDays)

	warming := coldStatusToAPI(hotobjects.ColdSet{Status: hotobjects.ColdWarmingUp, CapturedAt: captured, ObservedSince: &observed}, 7) //nolint:exhaustruct
	require.NotNil(t, warming.ObservedDays)
	assert.Equal(t, 3, *warming.ObservedDays)
	assert.Nil(t, warming.Count)

	stale := coldStatusToAPI(hotobjects.ColdSet{Status: hotobjects.ColdStale}, 7) //nolint:exhaustruct
	assert.Nil(t, stale.Count)
	assert.Nil(t, stale.ObservedDays)
}

func TestNoWritesDays(t *testing.T) {
	captured := time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)
	set := hotobjects.ColdSet{ //nolint:exhaustruct
		CapturedAt: captured,
		Tables:     []hotobjects.ColdTable{{Schema: "public", Table: "a", QuietSince: captured.Add(-41 * 24 * time.Hour)}}, //nolint:exhaustruct
	}

	one, zero, two := 1, 0, 2

	require.NotNil(t, noWritesDays(set, &one))
	assert.Equal(t, 41, *noWritesDays(set, &one))
	assert.Nil(t, noWritesDays(set, nil))
	assert.Nil(t, noWritesDays(set, &zero))
	assert.Nil(t, noWritesDays(set, &two))
}
