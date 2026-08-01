package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dbulashev/dasha/internal/schemalint"
)

func TestClassifySchemaLintError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want schemalint.SkipReason
	}{
		{
			name: "denied read is a privilege problem, not a fault",
			err:  &pgconn.PgError{Code: insufficientPrivilegeCode},
			want: schemalint.SkipInsufficientPrivileges,
		},
		{
			name: "missing catalog means the server does not support the check",
			err:  &pgconn.PgError{Code: undefinedTableCode},
			want: schemalint.SkipUnsupportedVersion,
		},
		{
			name: "missing column, same conclusion",
			err:  &pgconn.PgError{Code: undefinedColumnCode},
			want: schemalint.SkipUnsupportedVersion,
		},
		{
			name: "missing function, same conclusion",
			err:  &pgconn.PgError{Code: undefinedFunctionCode},
			want: schemalint.SkipUnsupportedVersion,
		},
		{
			name: "a deadline is a genuine failure of the check",
			err:  context.DeadlineExceeded,
			want: schemalint.SkipError,
		},
		{
			name: "anything else is an error",
			err:  errors.New("boom"),
			want: schemalint.SkipError,
		},
	}

	for _, tt := range tests {
		if got := classifySchemaLintError(tt.err); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestSkipDetail_CarriesNoUserData(t *testing.T) {
	// A server message may quote object contents; only the SQLSTATE goes out.
	pgErr := &pgconn.PgError{Code: "42501", Message: "permission denied for sequence secret_seq"}

	if got := skipDetail(pgErr); got != "SQLSTATE 42501" {
		t.Errorf("got %q, want the bare SQLSTATE", got)
	}

	if got := skipDetail(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("got %q, want timeout", got)
	}

	if got := skipDetail(errors.New("boom")); got != "query failed" {
		t.Errorf("got %q, want a generic phrase", got)
	}
}

func TestMergeSchemaLintInputs_OnlyMergesRows(t *testing.T) {
	dst := schemalint.Inputs{
		ServerVersionNum: 160000,
		Sequences:        []schemalint.SequenceRow{{Object: "a"}},
		Skipped:          []schemalint.Skip{{Code: "x"}},
	}

	mergeSchemaLintInputs(&dst, schemalint.Inputs{
		ServerVersionNum: 999,
		Sequences:        []schemalint.SequenceRow{{Object: "b"}},
		Unlogged:         []schemalint.UnloggedRow{{Object: "t"}},
		Skipped:          []schemalint.Skip{{Code: "y"}},
		Truncated:        true,
	})

	if len(dst.Sequences) != 2 || len(dst.Unlogged) != 1 {
		t.Errorf("rows must be appended, got %d sequences and %d unlogged", len(dst.Sequences), len(dst.Unlogged))
	}

	// Skips, version and truncation are decided by the caller: a merged skip
	// would let a failed query add its rows AND its "did not run" note.
	if len(dst.Skipped) != 1 || dst.ServerVersionNum != 160000 || dst.Truncated {
		t.Errorf("only row slices may be merged, got %+v", dst.Skipped)
	}
}

func TestAllChecksFailed(t *testing.T) {
	tests := []struct {
		name   string
		report schemalint.Report
		want   bool
	}{
		{
			name:   "every check errored — nothing was learned",
			report: schemalint.Report{Skipped: []schemalint.Skip{{Reason: schemalint.SkipError}}},
			want:   true,
		},
		{
			name: "a check reported what it could not see — that is a result",
			report: schemalint.Report{Skipped: []schemalint.Skip{
				{Reason: schemalint.SkipError},
				{Reason: schemalint.SkipInsufficientPrivileges},
			}},
			want: false,
		},
		{
			name: "findings present",
			report: schemalint.Report{
				Findings: []schemalint.Finding{{Code: schemalint.CodeNoPrimaryKey}},
				Skipped:  []schemalint.Skip{{Reason: schemalint.SkipError}},
			},
			want: false,
		},
		{
			name:   "nothing failed",
			report: schemalint.Report{Skipped: []schemalint.Skip{{Reason: schemalint.SkipDisabled}}},
			want:   false,
		},
	}

	for _, tt := range tests {
		if got := allChecksFailed(tt.report); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPlanCollapses(t *testing.T) {
	if !planCollapses([]string{schemalint.CodeNoPrimaryKey}) {
		t.Error("relation checks roll up to the partition root and need the roots map")
	}

	if planCollapses([]string{schemalint.CodeSequenceExhaustion}) {
		t.Error("sequences are addressed individually — no need to query partition roots")
	}

	if planCollapses(nil) {
		t.Error("no codes, no roots query")
	}
}
