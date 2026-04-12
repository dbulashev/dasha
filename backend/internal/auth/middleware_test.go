package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

func TestNoopMiddleware(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mw := NewAuthMiddleware(config.AuthConfig{Mode: config.AuthModeNone}, nil, nil, zap.NewNop())

	called := false
	handler := func(c echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Fatal("handler was not called")
	}
}

func TestNoopMiddlewareEmptyMode(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mw := NewAuthMiddleware(config.AuthConfig{}, nil, nil, zap.NewNop())

	called := false
	handler := func(c echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Fatal("handler was not called")
	}
}

func TestTokenMiddleware_ValidToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	req.Header.Set("X-API-Key", "test-token-123")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	cfg := config.AuthConfig{
		Mode: config.AuthModeToken,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "test-token-123", Role: "viewer"},
		},
	}
	mw := NewAuthMiddleware(cfg, nil, nil, zap.NewNop())

	var gotUser *UserContext

	handler := func(c echo.Context) error {
		gotUser = GetUser(c)
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotUser == nil {
		t.Fatal("user context not set")
	}

	if gotUser.Name != "test" {
		t.Errorf("expected name 'test', got %q", gotUser.Name)
	}

	if gotUser.Role != "viewer" {
		t.Errorf("expected role 'viewer', got %q", gotUser.Role)
	}

	if gotUser.AuthMethod != MethodToken {
		t.Errorf("expected auth method 'token', got %q", gotUser.AuthMethod)
	}
}

func TestTokenMiddleware_MissingToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	cfg := config.AuthConfig{
		Mode: config.AuthModeToken,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "test-token-123", Role: "viewer"},
		},
	}
	mw := NewAuthMiddleware(cfg, nil, nil, zap.NewNop())

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)

	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T", err)
	}

	if he.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", he.Code)
	}
}

func TestTokenMiddleware_InvalidToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	req.Header.Set("X-API-Key", "wrong-token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	cfg := config.AuthConfig{
		Mode: config.AuthModeToken,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "test-token-123", Role: "viewer"},
		},
	}
	mw := NewAuthMiddleware(cfg, nil, nil, zap.NewNop())

	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)

	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T", err)
	}

	if he.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", he.Code)
	}
}

func TestSkipAuth(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/info", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	cfg := config.AuthConfig{
		Mode: config.AuthModeToken,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "test-token-123", Role: "viewer"},
		},
	}
	mw := SkipAuth(NewAuthMiddleware(cfg, nil, nil, zap.NewNop()), "/api/auth/info")

	called := false
	handler := func(c echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Fatal("handler should have been called (path skipped)")
	}
}

func TestSkipAuthPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	cfg := config.AuthConfig{
		Mode: config.AuthModeToken,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "test-token-123", Role: "viewer"},
		},
	}
	mw := SkipAuthPrefix(NewAuthMiddleware(cfg, nil, nil, zap.NewNop()), "/auth/")

	called := false
	handler := func(c echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	}

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Fatal("handler should have been called (prefix skipped)")
	}
}

func TestExtractRoleFromClaims(t *testing.T) {
	tests := []struct {
		name        string
		claims      map[string]any
		claimPath   string
		roleMapping map[string]string
		want        string
	}{
		{
			name:      "keycloak admin",
			claims:    map[string]any{"realm_access": map[string]any{"roles": []any{"admin", "viewer"}}},
			claimPath: "realm_access.roles",
			want:      "admin",
		},
		{
			name:      "keycloak viewer",
			claims:    map[string]any{"realm_access": map[string]any{"roles": []any{"viewer"}}},
			claimPath: "realm_access.roles",
			want:      "viewer",
		},
		{
			name:      "missing claim",
			claims:    map[string]any{},
			claimPath: "realm_access.roles",
			want:      "viewer",
		},
		{
			name:      "no known role",
			claims:    map[string]any{"realm_access": map[string]any{"roles": []any{"unknown"}}},
			claimPath: "realm_access.roles",
			want:      "viewer",
		},
		{
			name:        "mapping: corporate group to admin",
			claims:      map[string]any{"groups": []any{"dba_team", "dev_team"}},
			claimPath:   "groups",
			roleMapping: map[string]string{"dba_team": "admin", "dev_team": "viewer"},
			want:        "admin",
		},
		{
			name:        "mapping: corporate group to viewer",
			claims:      map[string]any{"groups": []any{"dev_team"}},
			claimPath:   "groups",
			roleMapping: map[string]string{"dba_team": "admin", "dev_team": "viewer"},
			want:        "viewer",
		},
		{
			name:        "mapping: no matching group",
			claims:      map[string]any{"groups": []any{"sales"}},
			claimPath:   "groups",
			roleMapping: map[string]string{"dba_team": "admin", "dev_team": "viewer"},
			want:        "viewer",
		},
		{
			name:        "mapping: admin wins over viewer",
			claims:      map[string]any{"groups": []any{"dev_team", "dba_team"}},
			claimPath:   "groups",
			roleMapping: map[string]string{"dba_team": "admin", "dev_team": "viewer"},
			want:        "admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRoleFromClaims(tt.claims, tt.claimPath, tt.roleMapping)
			if got != tt.want {
				t.Errorf("extractRoleFromClaims() = %q, want %q", got, tt.want)
			}
		})
	}
}
