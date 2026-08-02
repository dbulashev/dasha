package http

import (
	"testing"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/schemalint"
)

func lintFindings() []schemalint.Finding {
	return []schemalint.Finding{
		{Code: schemalint.CodeSequenceExhaustion, Level: schemalint.LevelError, Schema: "public", Object: "orders_id_seq"}, //nolint:exhaustruct
		{Code: schemalint.CodeNoPrimaryKey, Level: schemalint.LevelError, Schema: "Analytics", Object: "events"},           //nolint:exhaustruct
		{Code: schemalint.CodeUnloggedRelation, Level: schemalint.LevelWarning, Schema: "public", Object: "cache"},         //nolint:exhaustruct
	}
}

func TestFilterSchemaLintFindings_UnfilteredKeepsEverything(t *testing.T) {
	t.Parallel()

	findings := lintFindings()

	got := filterSchemaLintFindings(findings, serverhttp.GetSchemaLintParams{}) //nolint:exhaustruct
	if len(got) != len(findings) {
		t.Fatalf("no filters must keep every finding, got %d of %d", len(got), len(findings))
	}
}

func TestFilterSchemaLintFindings_LevelAndCodeBothApply(t *testing.T) {
	t.Parallel()

	level := serverhttp.GetSchemaLintParamsLevel(schemalint.LevelError)

	byLevel := filterSchemaLintFindings(lintFindings(), serverhttp.GetSchemaLintParams{Level: &level}) //nolint:exhaustruct
	if len(byLevel) != 2 {
		t.Fatalf("level=error must keep both errors, got %d", len(byLevel))
	}

	codes := []string{schemalint.CodeNoPrimaryKey}

	byCode := filterSchemaLintFindings(lintFindings(), serverhttp.GetSchemaLintParams{Code: &codes}) //nolint:exhaustruct
	if len(byCode) != 1 || byCode[0].Code != schemalint.CodeNoPrimaryKey {
		t.Fatalf("code filter must keep only that check, got %+v", byCode)
	}

	// Together they narrow further: the other error-level finding drops out.
	both := filterSchemaLintFindings(lintFindings(), serverhttp.GetSchemaLintParams{Level: &level, Code: &codes}) //nolint:exhaustruct
	if len(both) != 1 || both[0].Code != schemalint.CodeNoPrimaryKey {
		t.Fatalf("level and code must both apply, got %+v", both)
	}
}

func TestFilterSchemaLintFindings_SubstringIsCaseInsensitiveAndTrimmed(t *testing.T) {
	t.Parallel()

	schema := "  ANALY  "

	got := filterSchemaLintFindings(lintFindings(), serverhttp.GetSchemaLintParams{Schema: &schema}) //nolint:exhaustruct
	if len(got) != 1 || got[0].Schema != "Analytics" {
		t.Fatalf("schema filter must match a trimmed substring case-insensitively, got %+v", got)
	}

	object := "SEQ"

	got = filterSchemaLintFindings(lintFindings(), serverhttp.GetSchemaLintParams{Object: &object}) //nolint:exhaustruct
	if len(got) != 1 || got[0].Object != "orders_id_seq" {
		t.Fatalf("object filter must match case-insensitively, got %+v", got)
	}
}

func TestPageOf_ReturnsTheRequestedWindow(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3, 4, 5}

	if got := pageOf(items, 2, 2); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("limit 2 offset 2 must return the third and fourth item, got %v", got)
	}

	if got := pageOf(items, 10, 3); len(got) != 2 {
		t.Fatalf("a limit past the end must return what is left, got %v", got)
	}
}

// A page past the end is still an array on the wire — a client iterating the
// findings must not have to handle null.
func TestPageOf_PastTheEndIsEmptyNotNil(t *testing.T) {
	t.Parallel()

	got := pageOf([]int{1, 2}, 10, 5)
	if got == nil {
		t.Fatal("a page past the end must not be nil")
	}

	if len(got) != 0 {
		t.Fatalf("a page past the end must be empty, got %v", got)
	}
}

func TestSchemaLintSkip_RequiredPrivilegeOnlyWhenTheCheckWasRefused(t *testing.T) {
	t.Parallel()

	refused := schemaLintSkip(schemalint.Skip{ //nolint:exhaustruct
		Code:   schemalint.CodeSequenceExhaustion,
		Reason: schemalint.SkipInsufficientPrivileges,
		Count:  3,
	})

	if refused.RequiredPrivilege == nil || *refused.RequiredPrivilege == "" {
		t.Fatal("a refused check must say which grant it needs")
	}

	if refused.Count == nil || *refused.Count != 3 {
		t.Fatalf("the count of unreadable objects must travel as a number, got %v", refused.Count)
	}

	failed := schemaLintSkip(schemalint.Skip{ //nolint:exhaustruct
		Code:   schemalint.CodeSequenceExhaustion,
		Reason: schemalint.SkipError,
		Detail: "timeout",
	})

	if failed.RequiredPrivilege != nil {
		t.Fatalf("a grant cannot fix a check that failed, got %q", *failed.RequiredPrivilege)
	}

	if failed.Detail == nil || *failed.Detail != "timeout" {
		t.Fatalf("the technical detail must be carried through, got %v", failed.Detail)
	}
}

func TestSchemaLintParams_AbsentWhenTheFindingQuotesNothing(t *testing.T) {
	t.Parallel()

	if _, ok := schemaLintParams(schemalint.Params{}); ok { //nolint:exhaustruct
		t.Fatal("params must stay absent when the finding sets no field")
	}

	usedPct := 96.5

	got, ok := schemaLintParams(schemalint.Params{UsedPct: &usedPct, OwnedColumnType: "integer"}) //nolint:exhaustruct
	if !ok {
		t.Fatal("params must be reported once a field is set")
	}

	if got.UsedPct == nil || *got.UsedPct != usedPct {
		t.Fatalf("used_pct must be carried through, got %v", got.UsedPct)
	}

	if got.OwnedColumnType == nil || *got.OwnedColumnType != "integer" {
		t.Fatalf("owned_column_type must be carried through, got %v", got.OwnedColumnType)
	}
}
