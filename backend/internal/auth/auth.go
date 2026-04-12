package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

var (
	errUnauthorized     = echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
	errForbidden        = echo.NewHTTPError(http.StatusForbidden, "forbidden")
	errInternalError    = echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	errRateLimitExceed  = echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
	errHTTPSRequired    = echo.NewHTTPError(http.StatusForbidden, "HTTPS required")
	errOIDCUnavailable  = echo.NewHTTPError(http.StatusServiceUnavailable, "OIDC provider not available")
	errInvalidState     = echo.NewHTTPError(http.StatusBadRequest, "invalid state")
	errNoActiveSession  = echo.NewHTTPError(http.StatusUnauthorized, "no active session")
	errTokenExchange    = echo.NewHTTPError(http.StatusUnauthorized, "token exchange failed")
	errNoIDToken        = echo.NewHTTPError(http.StatusUnauthorized, "no id_token in response")
	errInvalidIDToken   = echo.NewHTTPError(http.StatusUnauthorized, "invalid id_token")
	errParseClaims      = echo.NewHTTPError(http.StatusInternalServerError, "failed to parse claims")
	errSessionError     = echo.NewHTTPError(http.StatusInternalServerError, "session error")
	errAuthorizationErr = echo.NewHTTPError(http.StatusInternalServerError, "authorization error")
)

type Middlewares struct {
	RequireHTTPS echo.MiddlewareFunc
	RateLimit    echo.MiddlewareFunc
	Auth         echo.MiddlewareFunc
	Casbin       echo.MiddlewareFunc

	OIDCProvider   *OIDCProvider
	SessionManager *SessionManager

	rateLimiter *RateLimiter
}

func (m *Middlewares) Stop() {
	if m.rateLimiter != nil {
		m.rateLimiter.Stop()
	}
}

func NewMiddlewares(ctx context.Context, cfg config.AuthConfig, logger *zap.Logger) (*Middlewares, error) {
	var (
		oidcProvider *OIDCProvider
		sessionMgr   *SessionManager
	)

	if cfg.Mode == config.AuthModeOIDC {
		var err error

		oidcProvider, err = NewOIDCProvider(ctx, *cfg.OIDC, logger)
		if err != nil {
			logger.Warn("OIDC provider initialization failed (will retry on first request)", zap.Error(err))
		}

		sessionMgr = NewSessionManager(cfg)
	}

	enforcer, err := NewCasbinEnforcer()
	if err != nil {
		return nil, fmt.Errorf("casbin enforcer: %w", err)
	}

	rl := NewRateLimiter(cfg, logger)

	return &Middlewares{
		RequireHTTPS:   requireHTTPSMiddleware(cfg.RequireHTTPS),
		RateLimit:      rl.Middleware,
		Auth:           NewAuthMiddleware(cfg, oidcProvider, sessionMgr, logger),
		Casbin:         NewCasbinMiddleware(cfg, enforcer, logger),
		OIDCProvider:   oidcProvider,
		SessionManager: sessionMgr,
		rateLimiter:    rl,
	}, nil
}

type Method string

const (
	MethodToken Method = "token"
	MethodOIDC  Method = "oidc"
)

type UserContext struct {
	Name       string
	Email      string
	Role       string
	AuthMethod Method
}

const userContextKey = "auth_user"

func GetUser(c echo.Context) *UserContext {
	u, ok := c.Get(userContextKey).(*UserContext)
	if !ok {
		return nil
	}

	return u
}

func SetUser(c echo.Context, u *UserContext) {
	c.Set(userContextKey, u)
}
