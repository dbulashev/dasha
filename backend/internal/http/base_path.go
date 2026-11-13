package http

import (
	"strings"

	"github.com/labstack/echo/v4"
)

// WithBasePath принимает внешний префикс до маршрутизации и проверок RBAC.
// Внутренние URL остаются доступны для probes и прокси, уже снявшего префикс.
func (api *API) WithBasePath(basePath string) {
	if basePath == "" {
		return
	}
	api.Echo.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			u := c.Request().URL
			if strings.HasPrefix(u.Path, basePath+"/") {
				u.Path = strings.TrimPrefix(u.Path, basePath)
				if u.RawPath != "" {
					u.RawPath = strings.TrimPrefix(u.RawPath, basePath)
				}
			}
			return next(c)
		}
	})
}
