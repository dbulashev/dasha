package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

func TestCasbinEnforcer_Policies(t *testing.T) {
	enforcer, err := NewCasbinEnforcer()
	if err != nil {
		t.Fatalf("NewCasbinEnforcer: %v", err)
	}

	tests := []struct {
		role, path, method string
		want               bool
	}{
		{"admin", "/api/clusters", "GET", true},
		{"admin", "/api/clusters", "POST", true},
		{"admin", "/api/clusters", "DELETE", true},
		{"viewer", "/api/clusters", "GET", true},
		{"viewer", "/api/clusters", "POST", false},
		{"viewer", "/api/clusters", "DELETE", false},
		{"viewer", "/api/settings/analyze", "GET", true},
		{"unknown", "/api/clusters", "GET", false},
	}

	for _, tt := range tests {
		t.Run(tt.role+"_"+tt.method+"_"+tt.path, func(t *testing.T) {
			got, err := enforcer.Enforce(tt.role, tt.path, tt.method)
			if err != nil {
				t.Fatalf("Enforce error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Enforce(%q, %q, %q) = %v, want %v", tt.role, tt.path, tt.method, got, tt.want)
			}
		})
	}
}

func TestCasbinMiddleware_Noop(t *testing.T) {
	cfg := config.AuthConfig{Mode: config.AuthModeNone}
	mw := NewCasbinMiddleware(cfg, nil, zap.NewNop())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	called := false
	err := mw(func(_ echo.Context) error {
		called = true
		return nil
	})(c)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestCasbinMiddleware_ViewerAllowedGET(t *testing.T) {
	enforcer, _ := NewCasbinEnforcer()
	cfg := config.AuthConfig{Mode: config.AuthModeToken}
	mw := NewCasbinMiddleware(cfg, enforcer, zap.NewNop())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	SetUser(c, &UserContext{Name: "test", Role: "viewer", AuthMethod: MethodToken})

	called := false
	err := mw(func(_ echo.Context) error {
		called = true
		return nil
	})(c)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestCasbinMiddleware_ViewerDeniedPOST(t *testing.T) {
	enforcer, _ := NewCasbinEnforcer()
	cfg := config.AuthConfig{Mode: config.AuthModeToken}
	mw := NewCasbinMiddleware(cfg, enforcer, zap.NewNop())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	SetUser(c, &UserContext{Name: "test", Role: "viewer", AuthMethod: MethodToken})

	err := mw(func(_ echo.Context) error { return nil })(c)

	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T", err)
	}
	if he.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", he.Code)
	}
}

func TestCasbinMiddleware_AdminAllowedPOST(t *testing.T) {
	enforcer, _ := NewCasbinEnforcer()
	cfg := config.AuthConfig{Mode: config.AuthModeToken}
	mw := NewCasbinMiddleware(cfg, enforcer, zap.NewNop())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	SetUser(c, &UserContext{Name: "admin-user", Role: "admin", AuthMethod: MethodToken})

	called := false
	err := mw(func(_ echo.Context) error {
		called = true
		return nil
	})(c)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestCasbinMiddleware_NoUser(t *testing.T) {
	enforcer, _ := NewCasbinEnforcer()
	cfg := config.AuthConfig{Mode: config.AuthModeToken}
	mw := NewCasbinMiddleware(cfg, enforcer, zap.NewNop())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := mw(func(_ echo.Context) error { return nil })(c)

	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T", err)
	}
	if he.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", he.Code)
	}
}
