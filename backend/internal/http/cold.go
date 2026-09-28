package http

import (
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/hotobjects"
)

func wholeDays(d time.Duration) int {
	return int(d / (24 * time.Hour))
}

func coldStatusToAPI(set hotobjects.ColdSet, windowDays int) serverhttp.ColdTablesStatus {
	out := serverhttp.ColdTablesStatus{
		Status:     serverhttp.ColdTablesStatusStatus(set.Status),
		WindowDays: windowDays,
	}

	switch set.Status {
	case hotobjects.ColdAvailable:
		n := len(set.Tables)
		out.Count = &n
	case hotobjects.ColdWarmingUp:
		if set.ObservedSince != nil {
			d := wholeDays(set.CapturedAt.Sub(*set.ObservedSince))
			out.ObservedDays = &d
		}
	}

	return out
}

// noWritesDays resolves a 1-based index into the set the query was given.
func noWritesDays(set hotobjects.ColdSet, idx *int) *int {
	if idx == nil || *idx < 1 || *idx > len(set.Tables) {
		return nil
	}

	d := wholeDays(set.CapturedAt.Sub(set.Tables[*idx-1].QuietSince))

	return &d
}
