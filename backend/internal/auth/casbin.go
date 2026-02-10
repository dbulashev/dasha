package auth

import (
	"fmt"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

const casbinModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && keyMatch2(r.obj, p.obj) && r.act == p.act
`

const casbinPolicy = `
p, admin, /api/*, GET
p, admin, /api/*, POST
p, admin, /api/*, PUT
p, admin, /api/*, DELETE
p, viewer, /api/*, GET
`

func NewCasbinEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(casbinModel)
	if err != nil {
		return nil, fmt.Errorf("casbin model: %w", err)
	}

	sa := stringadapter.NewAdapter(casbinPolicy)

	e, err := casbin.NewEnforcer(m, sa)
	if err != nil {
		return nil, fmt.Errorf("casbin enforcer: %w", err)
	}

	return e, nil
}

func NewCasbinMiddleware(cfg config.AuthConfig, enforcer *casbin.Enforcer, logger *zap.Logger) echo.MiddlewareFunc {
	if cfg.Mode == config.AuthModeNone || cfg.Mode == "" || enforcer == nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return next
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return errUnauthorized
			}

			path := c.Request().URL.Path
			method := c.Request().Method

			allowed, err := enforcer.Enforce(user.Role, path, method)
			if err != nil {
				logger.Error("casbin enforce error", zap.Error(err))
				return errAuthorizationErr
			}

			if !allowed {
				logger.Debug("access denied by RBAC",
					zap.String("user", user.Name),
					zap.String("role", user.Role),
					zap.String("path", path),
					zap.String("method", method),
				)

				return errForbidden
			}

			return next(c)
		}
	}
}
