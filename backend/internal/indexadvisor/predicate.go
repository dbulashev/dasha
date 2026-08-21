package indexadvisor

import (
	"slices"
	"sort"
	"strings"
)

const isNullSuffix = " IS NULL"

// NullPredicateColumns reads an index predicate as pg_get_expr renders it and
// returns the columns of "a IS NULL AND b IS NULL", sorted; nil for anything else.
func NullPredicateColumns(expr string) []string {
	parts := splitConjuncts(trimParens(expr))
	if len(parts) == 0 {
		return nil
	}

	out := make([]string, 0, len(parts))

	for _, part := range parts {
		name, ok := isNullColumn(part)
		if !ok {
			return nil
		}

		out = append(out, name)
	}

	sort.Strings(out)

	return slices.Compact(out)
}

func isNullColumn(expr string) (string, bool) {
	expr = trimParens(expr)

	name, found := strings.CutSuffix(expr, isNullSuffix)
	if !found {
		return "", false
	}

	return unquoteIdent(strings.TrimSpace(name))
}

// trimParens removes the parentheses pg_get_expr wraps a whole expression in.
func trimParens(expr string) string {
	expr = strings.TrimSpace(expr)

	for strings.HasPrefix(expr, "(") && matchingParen(expr) == len(expr)-1 {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}

	return expr
}

func matchingParen(expr string) int {
	depth, quoted := 0, false

	for i, r := range expr {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func splitConjuncts(expr string) []string {
	const sep = " AND "

	var (
		out          []string
		depth        int
		quoted       bool
		start        int
		runes        = []rune(expr)
		sepRunes     = []rune(sep)
		sepLen       = len(sepRunes)
		matchesSepAt = func(i int) bool {
			return i+sepLen <= len(runes) && string(runes[i:i+sepLen]) == sep
		}
	)

	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '"':
			quoted = !quoted
		case quoted:
		case runes[i] == '(':
			depth++
		case runes[i] == ')':
			depth--
		case depth == 0 && matchesSepAt(i):
			out = append(out, string(runes[start:i]))
			i += sepLen - 1
			start = i + 1
		}
	}

	if start >= len(runes) {
		return nil
	}

	return append(out, string(runes[start:]))
}

// unquoteIdent accepts a bare or double-quoted column name; anything else is an
// expression, not a column.
func unquoteIdent(s string) (string, bool) {
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && len(s) > 1 {
		inner := s[1 : len(s)-1]
		if strings.Contains(strings.ReplaceAll(inner, `""`, ""), `"`) {
			return "", false
		}

		return strings.ReplaceAll(inner, `""`, `"`), true
	}

	if s == "" || strings.ContainsAny(s, " ()\"'.,:") {
		return "", false
	}

	return s, true
}
