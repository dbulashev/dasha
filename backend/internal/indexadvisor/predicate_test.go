package indexadvisor

import (
	"slices"
	"testing"
)

// The inputs are what pg_get_expr renders for indpred, parentheses and all.
func TestNullPredicateColumns(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want []string
	}{
		{name: "not partial", expr: "", want: nil},
		{name: "single test", expr: "(deleted_at IS NULL)", want: []string{"deleted_at"}},
		{
			name: "conjunction is sorted",
			expr: "((responsible IS NULL) AND (deleted_at IS NULL))",
			want: []string{"deleted_at", "responsible"},
		},
		{name: "quoted identifier", expr: `("deletedAt" IS NULL)`, want: []string{"deletedAt"}},
		{name: "comparison", expr: "(status = 'open'::text)", want: nil},
		{name: "mixed with a comparison", expr: "((deleted_at IS NULL) AND (status_id = 1))", want: nil},
		{name: "is not null", expr: "(deleted_at IS NOT NULL)", want: nil},
		{name: "disjunction", expr: "((deleted_at IS NULL) OR (status_id = 1))", want: nil},
		{name: "expression", expr: "((lower(email) IS NULL))", want: nil},
		{name: "cast", expr: "((deleted_at)::date IS NULL)", want: nil},
		{name: "cast without parentheses", expr: "(deleted_at::date IS NULL)", want: nil},
		{name: "and inside a quoted name is not a separator", expr: `("a AND b" IS NULL)`, want: []string{"a AND b"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NullPredicateColumns(tc.expr); !slices.Equal(got, tc.want) {
				t.Errorf("NullPredicateColumns(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}
