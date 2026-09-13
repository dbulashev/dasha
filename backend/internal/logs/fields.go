package logs

import (
	"strings"
)

// severityRank ranks severities by importance for picking a representative one
// in a dedup group. Handles both postgresql and pooler spellings (case-insensitive).
func severityRank(s string) int {
	switch strings.ToUpper(s) {
	case "PANIC":
		return 9
	case "FATAL":
		return 8
	case "ERROR":
		return 7
	case "WARNING":
		return 6
	case "NOTICE":
		return 5
	case "LOG":
		return 4
	case "INFO":
		return 3
	case "DEBUG":
		return 2
	case "NOISE":
		return 1
	default:
		return 0
	}
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
