package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
)

func TestBasePathKeepsAuthentication(t *testing.T) {
	for _, prefix := range []string{"", "/dasha", "/tools/dasha"} {
		t.Run(prefix, func(t *testing.T) {
			api := &API{Echo: echo.New()}
			api.WithBasePath(prefix)
			cfg := config.AuthConfig{Mode: config.AuthModeToken, Tokens: []config.AuthToken{{Name: "test", Token: "local-test-token", Role: config.RoleViewer}}} //nolint:exhaustruct
			api.Echo.Use(auth.NewAuthMiddleware(cfg, nil, nil, nil, zap.NewNop()))
			enforcer, err := auth.NewCasbinEnforcer()
			if err != nil {
				t.Fatal(err)
			}
			api.Echo.Use(auth.NewCasbinMiddleware(cfg, enforcer, zap.NewNop()))
			api.Echo.GET("/api/private", func(c echo.Context) error {
				if c.Request().URL.Path != "/api/private" || c.QueryParam("q") != "a/b" {
					t.Errorf("Префикс изменил маршрут или query: %s", c.Request().URL)
				}
				return c.NoContent(http.StatusOK)
			})
			api.Echo.POST("/api/private", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
			for _, key := range []string{"", "local-test-token"} {
				req := httptest.NewRequest(http.MethodGet, prefix+"/api/private?q=a%2Fb", nil)
				req.Header.Set("X-API-Key", key)
				rec := httptest.NewRecorder()
				api.Echo.ServeHTTP(rec, req)
				want := http.StatusOK
				if key == "" {
					want = http.StatusUnauthorized
				}
				if rec.Code != want {
					t.Fatalf("Статус %d, ожидается %d", rec.Code, want)
				}
			}
			req := httptest.NewRequest(http.MethodPost, prefix+"/api/private", nil)
			req.Header.Set("X-API-Key", "local-test-token")
			rec := httptest.NewRecorder()
			api.Echo.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatal("Префикс позволил viewer обойти RBAC")
			}
		})
	}
}
