package http

import (
	"strings"

	"github.com/labstack/echo/v4"
)

// WithBasePath принимает внешний префикс до маршрутизации и проверок RBAC.
// Зарегистрированные внутренние URL имеют приоритет: /api и /auth могут быть base.
func (api *API) WithBasePath(basePath string) {
	if basePath == "" {
		return
	}
	api.Echo.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			u := c.Request().URL
			if strings.HasPrefix(u.Path, basePath+"/") && !api.matchesInternalRoute(c) {
				u.Path = strings.TrimPrefix(u.Path, basePath)
				if u.RawPath != "" {
					u.RawPath = strings.TrimPrefix(u.RawPath, basePath)
				}
			}
			return next(c)
		}
	})
}

// Проверка отдельным контекстом не меняет параметры и handler текущего запроса.
func (api *API) matchesInternalRoute(c echo.Context) bool {
	probe := api.Echo.AcquireContext()
	defer api.Echo.ReleaseContext(probe)
	probe.Reset(c.Request(), nil)
	api.Echo.Router().Find(c.Request().Method, c.Request().URL.EscapedPath(), probe)
	return probe.Path() != ""
}
