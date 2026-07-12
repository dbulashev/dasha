package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"slices"
)

func NewAuthMiddleware(
	cfg config.AuthConfig,
	provider *OIDCProvider,
	sm *SessionManager,
	resolver PATResolver,
	logger *zap.Logger,
) echo.MiddlewareFunc {
	switch cfg.Mode {
	case config.AuthModeToken:
		return tokenMiddleware(cfg.Tokens, resolver, logger)
	case config.AuthModeOIDC:
		return oidcMiddleware(cfg.Tokens, provider, sm, resolver, logger)
	default:
		return noopMiddleware()
	}
}

// resolveAPIKey authenticates the X-API-Key header: static config tokens first
// (constant-time), then a personal access token via the resolver. Returns nil
// when the header is absent or matches nothing.
func resolveAPIKey(c echo.Context, tokens []config.AuthToken, resolver PATResolver, logger *zap.Logger) *UserContext {
	apiKey := c.Request().Header.Get("X-API-Key")
	if apiKey == "" {
		return nil
	}

	if user := validateToken(tokens, apiKey); user != nil {
		logger.Debug("authenticated via API key", zap.String("token_name", user.Name))

		return user
	}

	if resolver != nil {
		if user, ok := resolver.ResolveToken(c.Request().Context(), apiKey); ok {
			logger.Debug("authenticated via personal access token", zap.String("subject", user.Name))

			return user
		}
	}

	return nil
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

func tokenMiddleware(tokens []config.AuthToken, resolver PATResolver, logger *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := resolveAPIKey(c, tokens, resolver, logger)
			if user == nil {
				return errUnauthorized
			}

			SetUser(c, user)

			return next(c)
		}
	}
}

func oidcMiddleware(
	tokens []config.AuthToken,
	provider *OIDCProvider,
	sm *SessionManager,
	resolver PATResolver,
	logger *zap.Logger,
) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().Header.Get("X-API-Key") != "" {
				user := resolveAPIKey(c, tokens, resolver, logger)
				if user == nil {
					return errUnauthorized
				}

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
			if slices.Contains(paths, c.Request().URL.Path) {
				return next(c)
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
