// Package insights reads meaning out of log records: which of them carry an
// auto_explain plan, which event category every record belongs to, and what
// the plans and categories of one window add up to. It holds pure functions
// over records and reaches no log source.
package insights

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// PlanRecord is the part of an auto_explain record the plan parser needs.
type PlanRecord struct {
	DurationMs float64
	Body       string
	QueryID    int64
	HasQueryID bool
}

const (
	durationPrefix = "duration: "
	maxDurationMs  = float64(100 * 365 * 24 * time.Hour / time.Millisecond)
)

// Detect recognizes an auto_explain record: "duration: <ms> ms  plan:" and the
// plan body after it. queryID is the log field: the body carries none before
// PostgreSQL 16 or without log_verbose.
func Detect(text, queryID string) (PlanRecord, bool) {
	ms, rest, ok := splitDuration(text)
	if !ok {
		return PlanRecord{}, false
	}

	body, ok := strings.CutPrefix(rest, "  plan:")
	if !ok {
		return PlanRecord{}, false
	}

	rec := PlanRecord{DurationMs: ms, Body: strings.TrimLeft(body, "\r\n")}
	rec.QueryID, rec.HasQueryID = ParseQueryID(queryID)

	return rec, true
}

// ParseQueryID reads a query id field. Zero is what PostgreSQL writes when
// compute_query_id is off, so it counts as no id; negative ids are ordinary.
func ParseQueryID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}

	return id, true
}

// isStatementDuration recognizes the records of log_min_duration_statement and
// log_duration: a bare duration, or one followed by the statement, execute,
// parse or bind step.
func isStatementDuration(text string) bool {
	_, rest, ok := splitDuration(text)
	if !ok {
		return false
	}

	if rest == "" {
		return true
	}

	after, ok := strings.CutPrefix(rest, "  ")
	if !ok {
		return false
	}

	for _, step := range []string{"statement: ", "execute ", "parse ", "bind "} {
		if strings.HasPrefix(after, step) {
			return true
		}
	}

	return false
}

func splitDuration(text string) (float64, string, bool) {
	s, ok := strings.CutPrefix(text, durationPrefix)
	if !ok {
		return 0, "", false
	}

	num, rest, ok := strings.Cut(s, " ms")
	if !ok {
		return 0, "", false
	}

	ms, err := strconv.ParseFloat(num, 64)
	if err != nil || math.IsNaN(ms) || ms < 0 || ms > maxDurationMs {
		return 0, "", false
	}

	return ms, rest, true
}
