package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
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
			assertMapped(t, mapDatabaseError(tt.err), tt.wantCode, tt.err)
		})
	}
}

// assertMapped checks one mapping result: wantCode 0 means the error is none of
// the handler's business and must pass through unmapped.
func assertMapped(t *testing.T, got *echo.HTTPError, wantCode int, src error) {
	t.Helper()

	if wantCode == 0 {
		if got != nil {
			t.Fatalf("expected no mapping, got %d", got.Code)
		}

		return
	}

	if got == nil {
		t.Fatalf("expected %d, got no mapping", wantCode)
	}

	if got.Code != wantCode {
		t.Errorf("expected %d, got %d", wantCode, got.Code)
	}

	// echo.HTTPError unwraps to Internal, so the original error must still be
	// reachable — the handler logs it.
	if !errors.Is(got, src) {
		t.Error("original error should stay reachable for the log")
	}
}

func TestExtendDeadlines(t *testing.T) {
	t.Parallel()

	e := echo.New()
	e.Use(extendDeadlines("/slow", 5*time.Second, zap.NewNop()))

	slow := func(c echo.Context) error {
		select {
		case <-time.After(300 * time.Millisecond):
			return c.String(http.StatusOK, "ok")
		case <-c.Request().Context().Done():
			return c.String(http.StatusOK, "cancelled")
		}
	}
	e.GET("/slow", slow)
	e.GET("/other", slow)

	srv := httptest.NewUnstartedServer(e)
	srv.Config.ReadTimeout = 100 * time.Millisecond
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/slow")
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "ok", string(body), "the read deadline must not cancel the request")

	resp, err = srv.Client().Get(srv.URL + "/other")
	if err == nil {
		_ = resp.Body.Close()
	}

	assert.Error(t, err, "other routes keep the server-wide WriteTimeout")
}

func TestWritePIIError(t *testing.T) {
	t.Parallel()

	bindErr := echo.NewHTTPError(http.StatusBadRequest, "Invalid format for parameter database: bad")
	sqlErr := fmt.Errorf("GetPiiCatalogPairs | %w",
		&pgconn.PgError{Code: "42P01", Message: "relation \"pii_catalog_pair\" does not exist"}) //nolint:exhaustruct
	timeoutErr := fmt.Errorf("GetPiiCatalogStatus | %w",
		&pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}) //nolint:exhaustruct

	for _, tc := range []struct {
		name, path string
		err        error
		wantStatus int // 0: left to the default handler
		wantCode   string
	}{
		{"binding error", "/api/pii-catalog/pairs", bindErr, http.StatusBadRequest, "bad_request"},
		{"storage timeout", "/api/pii-catalog/status", timeoutErr, http.StatusGatewayTimeout, "timeout"},
		{"internal error", "/api/pii-catalog/pairs", sqlErr, http.StatusInternalServerError, "internal"},
		{"other path", "/api/clusters", bindErr, 0, ""},
		{"error before the handler", "/api/pii-catalog/pairs", echo.NewHTTPError(http.StatusNotFound), 0, ""},
		{"client gone", "/api/pii-catalog/pairs", fmt.Errorf("x | %w", context.Canceled), 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, tc.path, nil), rec)

			require.Equal(t, tc.wantStatus != 0, writePIIError(tc.err, c, zap.NewNop()))

			if tc.wantStatus == 0 {
				return
			}

			assert.Equal(t, tc.wantStatus, rec.Code)

			var body serverhttp.PiiCatalogError
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tc.wantCode, string(body.Code))
			assert.NotContains(t, rec.Body.String(), "pii_catalog_pair", "the cause must stay in the log")
		})
	}

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/pii-catalog/pairs", nil), rec)
	require.True(t, writePIIError(bindErr, c, zap.NewNop()))
	assert.JSONEq(t, `{"code":"bad_request","message":"Invalid format for parameter database: bad"}`, rec.Body.String())
}

func TestNew_PIICatalogRateLimitedIsAudited(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	pass := func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	tokenAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth.SetUser(c, &auth.UserContext{Name: "classifier", AuthMethod: auth.MethodToken}) //nolint:exhaustruct

			return next(c)
		}
	}

	rl := auth.NewRateLimiter(config.AuthConfig{ //nolint:exhaustruct
		RateLimit: &config.RateLimitConfig{RequestsPerSecond: 0.001, Burst: 1},
	}, zap.NewNop())
	t.Cleanup(rl.Stop)

	gate := auth.NewPIICatalogGate(config.PIICatalogConfig{ //nolint:exhaustruct
		Enabled: true, AllowedTokens: []string{"classifier"},
	}, logger)

	api := New(newPIIHandlers(&fakePIIStore{}), nil, pass, rl.Middleware, pass, tokenAuth, pass, gate, logger) //nolint:exhaustruct

	for i, want := range []int{http.StatusOK, http.StatusTooManyRequests} {
		rec := httptest.NewRecorder()
		api.Echo.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pii-catalog/status", nil))

		require.Equal(t, want, rec.Code, "request %d", i)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "request %d", i)
	}

	assert.Equal(t, 2, logs.FilterMessage("pii_catalog access").FilterField(zap.Bool("audit", true)).Len())
}
