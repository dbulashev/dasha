package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

func NewAuthMiddleware(
	cfg config.AuthConfig,
	provider *OIDCProvider,
	sm *SessionManager,
	logger *zap.Logger,
) echo.MiddlewareFunc {
	switch cfg.Mode {
	case config.AuthModeToken:
		return tokenMiddleware(cfg.Tokens, logger)
	case config.AuthModeOIDC:
		return oidcMiddleware(cfg.Tokens, provider, sm, logger)
	default:
		return noopMiddleware()
	}
}

func noopMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return next
	}
}

func requireHTTPSMiddleware(enabled bool) echo.MiddlewareFunc {
	if !enabled {
		return noopMiddleware()
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !isSecureRequest(c) {
				return errHTTPSRequired
			}

			return next(c)
		}
	}
}

func tokenMiddleware(tokens []config.AuthToken, logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			apiKey := c.Request().Header.Get("X-API-Key")
			if apiKey == "" {
				return errUnauthorized
			}

			user := validateToken(tokens, apiKey)
			if user == nil {
				return errUnauthorized
			}

			logger.Debug("authenticated via API key", zap.String("token_name", user.Name))
			SetUser(c, user)

			return next(c)
		}
	}
}

func oidcMiddleware(
	tokens []config.AuthToken,
	provider *OIDCProvider,
	sm *SessionManager,
	logger *zap.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if apiKey := c.Request().Header.Get("X-API-Key"); apiKey != "" {
				user := validateToken(tokens, apiKey)
				if user == nil {
					return errUnauthorized
				}

				logger.Debug("authenticated via API key", zap.String("token_name", user.Name))
				SetUser(c, user)

				return next(c)
			}

			if provider == nil || sm == nil {
				return errOIDCUnavailable
			}

			user, err := sm.ValidateAndRefresh(c, provider)
			if err != nil {
				if isAPIRequest(c) {
					return errUnauthorized
				}

				return c.Redirect(http.StatusFound, "/auth/login")
			}

			logger.Debug("authenticated via OIDC session", zap.String("user", user.Name))
			SetUser(c, user)

			return next(c)
		}
	}
}

func validateToken(tokens []config.AuthToken, apiKey string) *UserContext {
	for i := range tokens {
		if tokens[i].Token != "" && subtle.ConstantTimeCompare([]byte(apiKey), []byte(tokens[i].Token)) == 1 {
			return &UserContext{
				Name:       tokens[i].Name,
				Role:       tokens[i].Role,
				AuthMethod: MethodToken,
			}
		}
	}

	return nil
}

func isAPIRequest(c echo.Context) bool {
	return strings.HasPrefix(c.Request().URL.Path, "/api/")
}

func SkipAuth(mw echo.MiddlewareFunc, paths ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			for _, p := range paths {
				if c.Request().URL.Path == p {
					return next(c)
				}
			}

			return mw(next)(c)
		}
	}
}

func SkipAuthPrefix(mw echo.MiddlewareFunc, prefixes ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			for _, prefix := range prefixes {
				if strings.HasPrefix(path, prefix) {
					return next(c)
				}
			}

			return mw(next)(c)
		}
	}
}
