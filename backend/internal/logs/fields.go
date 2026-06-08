package logs

import (
	"strings"

	"github.com/dbulashev/dasha/internal/discovery/yandex"
)

// promotedFields maps abstract roles to the concrete message-map keys that
// carry them for a given service type. Yandex does not publish a column list;
// these were confirmed empirically (postgresql: 26 csvlog columns + hostname;
// pooler/Odyssey: 9 columns). See plans/yandex-log-search-design.md §3.3.1.
type promotedFields struct {
	severity string
	text     string
	host     string
	database string
	user     string
}

func promotedFor(st yandex.ServiceType) promotedFields {
	if st == yandex.ServicePooler {
		return promotedFields{
			severity: "level",
			text:     "text",
			host:     "hostname",
			database: "db",
			user:     "user",
		}
	}

	return promotedFields{
		severity: "error_severity",
		text:     "message",
		host:     "hostname",
		database: "database_name",
		user:     "user_name",
	}
}

// maskFor lists the free-text message keys whose values must be passed through
// sanitize.SQL() before leaving the backend, per service type.
func maskFor(st yandex.ServiceType) []string {
	if st == yandex.ServicePooler {
		return []string{"text"}
	}

	return []string{"message", "query", "internal_query", "detail", "hint", "context"}
}

// severityFilterField is the native-filter field name for severity.
func severityFilterField(st yandex.ServiceType) string {
	if st == yandex.ServicePooler {
		return "message.level"
	}

	return "message.error_severity"
}

// severityAllowlist is the set of accepted severity values per service type,
// in the casing the Yandex API expects (postgresql UPPER, pooler lower).
func severityAllowlist(st yandex.ServiceType) map[string]struct{} {
	if st == yandex.ServicePooler {
		return map[string]struct{}{
			"debug": {}, "info": {}, "warning": {}, "error": {}, "fatal": {},
		}
	}

	return map[string]struct{}{
		"DEBUG": {}, "LOG": {}, "INFO": {}, "NOTICE": {},
		"WARNING": {}, "ERROR": {}, "FATAL": {}, "PANIC": {},
	}
}

// normalizeSeverityCase converts a user-supplied severity to the casing the
// service expects (postgresql UPPER, pooler lower).
func normalizeSeverityCase(st yandex.ServiceType, s string) string {
	if st == yandex.ServicePooler {
		return strings.ToLower(s)
	}

	return strings.ToUpper(s)
}

// severityRank ranks severities by importance for picking a representative one
// in a dedup group. Handles both postgresql and pooler spellings (case-insensitive).
func severityRank(s string) int {
	switch strings.ToUpper(s) {
	case "PANIC":
		return 8
	case "FATAL":
		return 7
	case "ERROR":
		return 6
	case "WARNING":
		return 5
	case "NOTICE":
		return 4
	case "LOG":
		return 3
	case "INFO":
		return 2
	case "DEBUG":
		return 1
	default:
		return 0
	}
}

// normalize collapses whitespace runs and trims, for grouping near-identical
// messages during deduplication.
func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
