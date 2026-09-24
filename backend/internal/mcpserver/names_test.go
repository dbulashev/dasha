package mcpserver

import (
	"slices"
	"testing"
)

func TestTokensMatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		query, cand string
		want        bool
	}{
		{"ACME prod", "acme-prod", true},
		{"acme_prod", "acme.prod", true},
		{"prod acme", "acme-prod", true},
		{"pro", "acme-prod", true},
		{"acme stage", "acme-prod", false},
		{"", "acme-prod", false},
	} {
		if got := tokensMatch(nameTokens(tc.query), nameTokens(tc.cand)); got != tc.want {
			t.Errorf("tokensMatch(%q, %q) = %v, want %v", tc.query, tc.cand, got, tc.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"acme prd", "acme prod", 1},
		{"база", "базы", 1},
	} {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSimilarNames_Order(t *testing.T) {
	t.Parallel()

	cands := []string{"prod-acme", "billing", "acme_prod_replica", "acme-prod-dr", "acme-prod"}

	got := similarNames("acme prod", cands, maxSuggestions)
	want := []string{"acme-prod", "acme-prod-dr", "acme_prod_replica", "prod-acme"}

	if !slices.Equal(got, want) {
		t.Errorf("similarNames = %v, want %v", got, want)
	}
}

func TestSimilarNames_DistanceOnlyWithoutMatches(t *testing.T) {
	t.Parallel()

	if got := similarNames("billing", []string{"biling", "billing-main"}, maxSuggestions); !slices.Equal(got, []string{"billing-main"}) {
		t.Errorf("a token match must suppress the distance tier, got %v", got)
	}

	if got := similarNames("acme-prd", []string{"acme-prod", "billing"}, maxSuggestions); !slices.Equal(got, []string{"acme-prod"}) {
		t.Errorf("distance tier = %v, want [acme-prod]", got)
	}
}

func TestSimilarNames_Limit(t *testing.T) {
	t.Parallel()

	cands := []string{"pg-1", "pg-2", "pg-3", "pg-4", "pg-5", "pg-6", "pg-7"}

	if got := similarNames("pg", cands, maxSuggestions); len(got) != maxSuggestions {
		t.Errorf("got %d names, want %d", len(got), maxSuggestions)
	}
}
