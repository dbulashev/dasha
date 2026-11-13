// Стенд маршрутизации для контейнерного smoke-теста, без БД и внешнего IdP.
package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
	dashahttp "github.com/dbulashev/dasha/internal/http"
)

func main() {
	prefix, err := config.NormalizeBasePath(os.Getenv("DASHA_BASE_PATH"))
	if err != nil {
		log.Fatal(err)
	}
	api := &dashahttp.API{Echo: echo.New()}
	api.WithBasePath(prefix)
	cfg := config.AuthConfig{Mode: config.AuthModeToken, Tokens: []config.AuthToken{ //nolint:exhaustruct
		{Name: "viewer", Token: "local-test-viewer", Role: config.RoleViewer},
		{Name: "admin", Token: "local-test-admin", Role: config.RoleAdmin},
	}}
	enforcer, err := auth.NewCasbinEnforcer()
	if err != nil {
		log.Fatal(err)
	}
	public := func(mw echo.MiddlewareFunc) echo.MiddlewareFunc {
		return auth.SkipAuth(auth.SkipAuthPrefix(mw, "/auth/"), "/api/auth/info")
	}
	api.Echo.Use(public(auth.NewAuthMiddleware(cfg, nil, nil, nil, zap.NewNop())))
	api.Echo.Use(public(auth.NewCasbinMiddleware(cfg, enforcer, zap.NewNop())))
	response := func(c echo.Context) error {
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, 4096))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{
			"path": c.Request().URL.Path, "query": c.Request().URL.RawQuery,
			"uri":    c.Request().RequestURI,
			"method": c.Request().Method, "body": string(body),
			"proto": c.Request().Header.Get("X-Forwarded-Proto"),
		})
	}
	api.Echo.GET("/api/auth/info", response)
	api.Echo.GET("/api/private", response)
	api.Echo.POST("/api/private", response)
	api.Echo.POST("/api/activity/profile", response)
	api.Echo.GET("/auth/login", func(c echo.Context) error {
		return c.Redirect(http.StatusFound, prefix+"/auth/callback?"+c.Request().URL.RawQuery)
	})
	server := &http.Server{ //nolint:exhaustruct
		Addr: ":8000", Handler: api.Echo, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
