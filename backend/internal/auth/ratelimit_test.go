package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	cfg := config.AuthConfig{Mode: config.AuthModeNone}
	rl := NewRateLimiter(cfg, zap.NewNop())
	defer rl.Stop()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	called := false

	err := rl.Middleware(func(_ echo.Context) error {
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

func TestRateLimitMiddleware_AllowsBurst(t *testing.T) {
	cfg := config.AuthConfig{
		Mode: config.AuthModeNone,
		RateLimit: &config.RateLimitConfig{
			RequestsPerSecond: 100,
			Burst:             5,
		},
	}
	rl := NewRateLimiter(cfg, zap.NewNop())
	defer rl.Stop()

	e := echo.New()

	// Should allow up to burst size.
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := rl.Middleware(func(_ echo.Context) error { return nil })(c)
		if err != nil {
			t.Fatalf("request %d should be allowed: %v", i, err)
		}
	}
}

func TestRateLimitMiddleware_Rejects(t *testing.T) {
	cfg := config.AuthConfig{
		Mode: config.AuthModeNone,
		RateLimit: &config.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             2,
		},
	}
	rl := NewRateLimiter(cfg, zap.NewNop())
	defer rl.Stop()

	e := echo.New()

	// Exhaust burst.
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		rl.Middleware(func(_ echo.Context) error { return nil })(c)
	}

	// Next request should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := rl.Middleware(func(_ echo.Context) error { return nil })(c)

	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("expected echo.HTTPError, got %T (%v)", err, err)
	}

	if he.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", he.Code)
	}
}

func TestRateLimitMiddleware_PerIdentity(t *testing.T) {
	cfg := config.AuthConfig{
		Mode: config.AuthModeNone,
		RateLimit: &config.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             1,
		},
	}
	rl := NewRateLimiter(cfg, zap.NewNop())
	defer rl.Stop()

	e := echo.New()

	// First IP exhausts burst.
	req1 := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	c1 := e.NewContext(req1, httptest.NewRecorder())
	rl.Middleware(func(_ echo.Context) error { return nil })(c1)

	// Second IP should still be allowed.
	req2 := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	c2 := e.NewContext(req2, httptest.NewRecorder())

	err := rl.Middleware(func(_ echo.Context) error { return nil })(c2)
	if err != nil {
		t.Fatalf("different IP should be allowed: %v", err)
	}
}

func TestRateLimitKey_User(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	SetUser(c, &UserContext{Name: "alice", Role: "admin", AuthMethod: MethodOIDC})

	key := rateLimitKey(c)
	if key != "user:alice" {
		t.Errorf("expected user:alice, got %q", key)
	}
}

func TestRateLimitKey_IP(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	c := e.NewContext(req, httptest.NewRecorder())

	key := rateLimitKey(c)
	if key != "ip:192.168.1.1" {
		t.Errorf("expected ip:192.168.1.1, got %q", key)
	}
}
