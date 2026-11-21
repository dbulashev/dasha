package sanitize

import (
	"strings"
	"unicode/utf8"
)

// TruncateUTF8 replaces invalid UTF-8 with "?" and cuts s to at most maxBytes
// without splitting a rune.
func TruncateUTF8(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) <= maxBytes {
		return s
	}

	s = s[:maxBytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}

	return s
}
