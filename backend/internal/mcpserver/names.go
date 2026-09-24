package mcpserver

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

func nameTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
}

// tokensMatch reports whether every query token is a substring of some
// candidate token, regardless of order.
func tokensMatch(query, cand []string) bool {
	if len(query) == 0 {
		return false
	}

	for _, q := range query {
		if !slices.ContainsFunc(cand, func(c string) bool { return strings.Contains(c, q) }) {
			return false
		}
	}

	return true
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		cur[0] = i

		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}

		prev, cur = cur, prev
	}

	return prev[len(rb)]
}

const (
	tierExact = iota
	tierPrefix
	tierTokens
)

// similarNames orders candidates by exact, prefix and token match against
// given; edit distance ranks them only when none of those matched.
func similarNames(given string, cands []string, limit int) []string {
	type ranked struct {
		name string
		tier int
		dist int
	}

	q := nameTokens(given)
	qs := strings.Join(q, " ")

	var hits []ranked

	for _, c := range cands {
		ct := nameTokens(c)
		cs := strings.Join(ct, " ")

		switch {
		case qs == "":
		case cs == qs:
			hits = append(hits, ranked{c, tierExact, 0})
		case strings.HasPrefix(cs, qs):
			hits = append(hits, ranked{c, tierPrefix, len(cs)})
		case tokensMatch(q, ct):
			hits = append(hits, ranked{c, tierTokens, len(cs)})
		}
	}

	if len(hits) == 0 && qs != "" {
		maxDist := max(2, len([]rune(qs))/3)

		for _, c := range cands {
			if d := levenshtein(qs, strings.Join(nameTokens(c), " ")); d <= maxDist {
				hits = append(hits, ranked{c, 0, d})
			}
		}
	}

	slices.SortFunc(hits, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(a.tier, b.tier), cmp.Compare(a.dist, b.dist), cmp.Compare(a.name, b.name))
	})

	out := make([]string, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.name)
	}

	return out
}
