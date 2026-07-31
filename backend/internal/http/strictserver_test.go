package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapDatabaseError(t *testing.T) {
	t.Parallel()

	// Both cases arrive wrapped in the repository's error chain, which is the
	// only form the error handler ever sees.
	lockErr := fmt.Errorf("GetTablesDescribe | scan | %w",
		&pgconn.PgError{Code: "55P03", Message: "canceled on lock timeout"}) //nolint:exhaustruct
	timeoutErr := fmt.Errorf("GetTablesDescribe | getTablesDescribeMetadata | scan | %w",
		context.DeadlineExceeded)
	statementTimeoutErr := fmt.Errorf("getQueriesReport | %w",
		&pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}) //nolint:exhaustruct

	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{name: "lock timeout is 423", err: lockErr, wantCode: http.StatusLocked},
		{name: "deadline is 504", err: timeoutErr, wantCode: http.StatusGatewayTimeout},
		{name: "statement_timeout is 504", err: statementTimeoutErr, wantCode: http.StatusGatewayTimeout},
		{name: "client disconnect is not mapped", err: context.Canceled, wantCode: 0},
		{name: "other database error is not mapped", err: errors.New("syntax error"), wantCode: 0},
		{
			name: "unrelated SQLSTATE is not mapped",
			err: fmt.Errorf("scan | %w",
				&pgconn.PgError{Code: "42P01", Message: "relation does not exist"}), //nolint:exhaustruct
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := mapDatabaseError(tt.err)

			if tt.wantCode == 0 {
				if got != nil {
					t.Fatalf("expected no mapping, got %d", got.Code)
				}

				return
			}

			if got == nil {
				t.Fatalf("expected %d, got no mapping", tt.wantCode)
			}

			if got.Code != tt.wantCode {
				t.Errorf("expected %d, got %d", tt.wantCode, got.Code)
			}

			// echo.HTTPError unwraps to Internal, so the original error must
			// still be reachable — the handler logs it.
			if !errors.Is(got, tt.err) {
				t.Error("original error should stay reachable for the log")
			}
		})
	}
}
