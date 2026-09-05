package logs

import (
	"regexp"
	"strings"
)

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

// Variable parts of a log line that must be masked so structurally identical
// messages group together during deduplication. Order matters: quoted literals,
// LSNs and hex are masked before bare numbers (an LSN like 2E/28E36B88 would
// otherwise leave letter residue after digit masking and split the group).
var (
	reQuoted = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	reLSN    = regexp.MustCompile(`\b[0-9a-fA-F]+/[0-9a-fA-F]+\b`)
	reHex    = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

// maskVariables collapses whitespace and replaces variable tokens (quoted
// literals, WAL LSNs, hex and numbers) with the given placeholder.
func maskVariables(s, placeholder string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = reQuoted.ReplaceAllString(s, placeholder)
	s = reLSN.ReplaceAllString(s, placeholder)
	s = reHex.ReplaceAllString(s, placeholder)
	s = reNumber.ReplaceAllString(s, placeholder)

	return s
}

// normalize builds the dedup grouping key: structurally identical messages —
// e.g. "login time: 656 microseconds" and "login time: 698 microseconds", or
// "connection from 10.0.0.1:5432" — collapse to a single template.
func normalize(s string) string {
	return maskVariables(s, "?")
}

// displayPlaceholder marks masked variable tokens in the text shown for a
// dedup group, so the row reads as a template rather than the concrete values
// of one arbitrary member record.
const displayPlaceholder = "<*>"

func displayTemplate(s string) string {
	return maskVariables(s, displayPlaceholder)
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
