// Package pattern folds log messages into templates: the variable parts of a
// line are masked so structurally identical messages compare equal.
package pattern

import (
	"regexp"
	"strings"
)

// Order matters: quoted literals, LSNs and hex are masked before bare numbers
// (an LSN like 2E/28E36B88 would otherwise leave letter residue after digit
// masking and split the group).
var (
	reQuoted = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	reLSN    = regexp.MustCompile(`\b[0-9a-fA-F]+/[0-9a-fA-F]+\b`)
	reHex    = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

// Placeholder marks a masked token in the text shown for a template, so the
// row reads as a template rather than the values of one arbitrary record.
const Placeholder = "<*>"

func mask(s, placeholder string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = reQuoted.ReplaceAllString(s, placeholder)
	s = reLSN.ReplaceAllString(s, placeholder)
	s = reHex.ReplaceAllString(s, placeholder)
	s = reNumber.ReplaceAllString(s, placeholder)

	return s
}

// Key is the grouping key: "login time: 656 microseconds" and "login time:
// 698 microseconds" share one.
func Key(s string) string {
	return mask(s, "?")
}

// Display is the template as shown to the user.
func Display(s string) string {
	return mask(s, Placeholder)
}
