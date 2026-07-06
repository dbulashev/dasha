package logs

import (
	"strings"

	"github.com/dbulashev/dasha/internal/discovery/yandex"
)

// promotedFields maps abstract roles to the concrete message-map keys that
// carry them for a given service type.
type promotedFields struct {
	severity string
	text     string
	host     string
	database string
	user     string
}

// serviceFields co-locates everything Dasha knows about one service type's log
// schema: promoted columns, keys to mask, the native-filter severity field and
// the accepted severity values with their casing. Yandex does not publish a
// column list; these were confirmed empirically (postgresql: 26 csvlog columns
// + hostname; pooler/Odyssey: 9 columns). See plans/yandex-log-search-design.md §3.3.1.
type serviceFields struct {
	promoted promotedFields
	// mask lists the free-text keys whose values must be passed through
	// sanitize.SQL() before leaving the backend.
	mask []string
	// severityFilterField is the native-filter field name for severity.
	severityFilterField string
	// severityAllow is the set of accepted severity values, in the casing the
	// Yandex API expects (postgresql UPPER, pooler lower).
	severityAllow map[string]struct{}
	// normalizeSeverity converts a user-supplied severity to that casing.
	normalizeSeverity func(string) string
}

var postgresqlFields = serviceFields{
	promoted: promotedFields{
		severity: "error_severity",
		text:     "message",
		host:     "hostname",
		database: "database_name",
		user:     "user_name",
	},
	mask:                []string{"message", "query", "internal_query", "detail", "hint", "context"},
	severityFilterField: "message.error_severity",
	severityAllow: map[string]struct{}{
		"DEBUG": {}, "LOG": {}, "INFO": {}, "NOTICE": {},
		"WARNING": {}, "ERROR": {}, "FATAL": {}, "PANIC": {},
	},
	normalizeSeverity: strings.ToUpper,
}

var poolerFields = serviceFields{
	promoted: promotedFields{
		severity: "level",
		text:     "text",
		host:     "hostname",
		database: "db",
		user:     "user",
	},
	mask:                []string{"text"},
	severityFilterField: "message.level",
	severityAllow: map[string]struct{}{
		"debug": {}, "info": {}, "warning": {}, "error": {}, "fatal": {},
	},
	normalizeSeverity: strings.ToLower,
}

func fieldsFor(st yandex.ServiceType) serviceFields {
	if st == yandex.ServicePooler {
		return poolerFields
	}

	return postgresqlFields
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
