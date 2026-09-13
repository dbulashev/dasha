package insights

import (
	"slices"
	"strings"

	"github.com/dbulashev/dasha/internal/config"
)

// Category codes. They are stable strings: the locale files, MCP and the API
// key on them.
const (
	CategoryDeadlock        = "deadlock"
	CategoryLockWait        = "lock_wait"
	CategoryConnectionLimit = "connection_limit"
	CategoryAuthentication  = "authentication"
	CategoryCanceled        = "canceled"
	CategoryPlan            = "plan"
	CategorySlowQuery       = "slow_query"
	CategoryCheckpoint      = "checkpoint"
	CategoryAutovacuum      = "autovacuum"
	CategoryTempFile        = "temp_file"
	CategoryConnection      = "connection"
	CategoryError           = "error"
	CategoryOther           = "other"
)

// Record is what classification reads from a log record. SQLState is empty
// when the stream does not carry the field.
type Record struct {
	Stream   string
	Severity string
	SQLState string
	Text     string
}

// Category is one entry of the registry. Stream restricts it to one stream;
// empty matches every stream.
type Category struct {
	Code   string
	Stream string
	Match  func(Record) bool
}

// registry is matched in order and the first match wins: the SQLSTATE first,
// then what the record says about itself, then the message text. Message texts
// are the English ones; a server with localized lc_messages lands in other.
var registry = []Category{
	{Code: CategoryDeadlock, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return stateOr(r, "40P01", "deadlock detected")
	}},
	{Code: CategoryLockWait, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return r.SQLState == "55P03" ||
			hasAnyPrefix(r.Text, "canceling statement due to lock timeout", "could not obtain lock on") ||
			(strings.HasPrefix(r.Text, "process ") &&
				containsAny(r.Text, " still waiting for ", " acquired ", " avoided deadlock ", " detected deadlock "))
	}},
	{Code: CategoryConnectionLimit, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return r.SQLState == "53300" ||
			hasAnyPrefix(r.Text, "sorry, too many clients already", "remaining connection slots are reserved")
	}},
	{Code: CategoryAuthentication, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return strings.HasPrefix(r.SQLState, "28") ||
			hasAnyPrefix(r.Text, "password authentication failed", "no pg_hba.conf entry")
	}},
	{Code: CategoryCanceled, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return stateOr(r, "57014", "canceling statement due to")
	}},
	{Code: CategoryPlan, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		_, ok := Detect(r.Text, "")

		return ok
	}},
	{Code: CategorySlowQuery, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return isStatementDuration(r.Text)
	}},
	{Code: CategoryCheckpoint, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return hasAnyPrefix(r.Text,
			"checkpoint starting:", "checkpoint complete:",
			"restartpoint starting:", "restartpoint complete:",
			"checkpoints are occurring too frequently", "recovery restart point at")
	}},
	{Code: CategoryAutovacuum, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return hasAnyPrefix(r.Text,
			"automatic vacuum of table", "automatic aggressive vacuum", "automatic analyze of table",
			"skipping vacuum of", "skipping analyze of")
	}},
	{Code: CategoryTempFile, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return strings.HasPrefix(r.Text, "temporary file: ")
	}},
	{Code: CategoryConnection, Stream: config.LogStreamPostgreSQL, Match: func(r Record) bool {
		return hasAnyPrefix(r.Text,
			"connection received:", "connection authenticated:", "connection authorized:",
			"replication connection authorized:", "connection ready:", "disconnection:")
	}},
	{Code: CategoryError, Match: func(r Record) bool {
		return isErrorState(r.SQLState) || isErrorSeverity(r.Severity)
	}},
}

// Classify returns the code of the first category the record matches, or
// CategoryOther.
func Classify(r Record) string {
	for _, c := range registry {
		if c.Stream != "" && c.Stream != r.Stream {
			continue
		}

		if c.Match(r) {
			return c.Code
		}
	}

	return CategoryOther
}

// Categories returns the registry in matching order, CategoryOther last.
func Categories() []Category {
	return append(slices.Clone(registry), Category{
		Code:  CategoryOther,
		Match: func(Record) bool { return true },
	})
}

// stateOr trusts the SQLSTATE when the stream carries one, and falls back to
// the message only when it does not.
func stateOr(r Record, state, prefix string) bool {
	if r.SQLState != "" {
		return r.SQLState == state
	}

	return strings.HasPrefix(r.Text, prefix)
}

// isErrorState: classes 00, 01 and 02 are success, warning and no data.
func isErrorState(state string) bool {
	if len(state) != 5 {
		return false
	}

	switch state[:2] {
	case "00", "01", "02":
		return false
	default:
		return true
	}
}

func isErrorSeverity(s string) bool {
	switch strings.ToUpper(s) {
	case "ERROR", "FATAL", "PANIC":
		return true
	default:
		return false
	}
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}

	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}
