package victorialogs

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// lowerOutsideFold maps a lowercase rune to the runes that lowercase to it but
// lie outside its case-fold orbit, such as U+0130 for 'i': (?i) misses them.
var lowerOutsideFold = sync.OnceValue(func() map[rune][]rune {
	out := map[rune][]rune{}

	for x := rune(0); x <= unicode.MaxRune; x++ {
		l := unicode.ToLower(x)
		if l != x && !inFoldOrbit(l, x) {
			out[l] = append(out[l], x)
		}
	}

	return out
})

func inFoldOrbit(r, x rune) bool {
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f == x {
			return true
		}
	}

	return false
}

// substringPattern is a regexp matching every value in which containsFold of
// the logs service finds s.
func substringPattern(s string) string {
	var b strings.Builder

	b.WriteString("(?i)")

	for _, c := range s {
		l := unicode.ToLower(c)

		extra := lowerOutsideFold()[l]
		if len(extra) == 0 {
			b.WriteString(regexp.QuoteMeta(string(c)))

			continue
		}

		b.WriteByte('[')
		b.WriteRune(l)

		for _, x := range extra {
			b.WriteRune(x)
		}

		b.WriteByte(']')
	}

	return b.String()
}
