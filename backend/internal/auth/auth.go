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
	errRateLimitExceed  = echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
	errHTTPSRequired    = echo.NewHTTPError(http.StatusForbidden, "HTTPS required")
	errOIDCUnavailable  = echo.NewHTTPError(http.StatusServiceUnavailable, "OIDC provider not available")
	errInvalidState     = echo.NewHTTPError(http.StatusBadRequest, "invalid state")
	errNoActiveSession  = echo.NewHTTPError(http.StatusUnauthorized, "no active session")
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

func NewMiddlewares(ctx context.Context, cfg config.AuthConfig, resolver PATResolver, logger *zap.Logger) (*Middlewares, error) {
	if cfg.Mode != config.AuthModeNone && cfg.Mode != "" && !cfg.RequireHTTPS {
		logger.Warn(
			"auth enabled without require_https — credentials may be transmitted in plaintext",
			zap.String("auth_mode", string(cfg.Mode)),
		)
	}

	var (
		oidcProvider *OIDCProvider
		sessionMgr   *SessionManager
	)

	if cfg.Mode == config.AuthModeOIDC {
		if len(cfg.CookieSecret) < minSecretLen {
			logger.Warn("auth.cookie_secret is unset or too short — session cookies are keyed with an ephemeral random key that changes on every restart and differs per replica; users get logged out on restarts/scale-out. Set a stable cookie_secret (>=32 chars), ideally via cookie_secret_from_env.")
		}

		oidcProvider = NewOIDCProvider(ctx, *cfg.OIDC, logger)
		sessionMgr = NewSessionManager(cfg, logger)
	}

	enforcer, err := NewCasbinEnforcer()
	if err != nil {
		return nil, fmt.Errorf("casbin enforcer: %w", err)
	}

	rl := NewRateLimiter(cfg, logger)

	return &Middlewares{
		RequireHTTPS:   requireHTTPSMiddleware(cfg.RequireHTTPS),
		RateLimit:      rl.Middleware,
		Auth:           NewAuthMiddleware(cfg, oidcProvider, sessionMgr, resolver, logger),
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
	MethodPAT   Method = "pat" // personal access token (user-minted)
)

type UserContext struct {
	Name       string
	Email      string
	Role       string
	AuthMethod Method
}

const userContextKey = "auth_user"

type ctxUserKey struct{}

func GetUser(c echo.Context) *UserContext {
	u, ok := c.Get(userContextKey).(*UserContext)
	if !ok {
		return nil
	}

	return u
}

// UserFromContext returns the authenticated user from a stdlib context.
// Populated by SetUser so strict-server handlers (which receive context.Context,
// not echo.Context) can still identify the caller.
func UserFromContext(ctx context.Context) *UserContext {
	u, _ := ctx.Value(ctxUserKey{}).(*UserContext)
	return u
}

func SetUser(c echo.Context, u *UserContext) {
	c.Set(userContextKey, u)

	req := c.Request()
	c.SetRequest(req.WithContext(context.WithValue(req.Context(), ctxUserKey{}, u)))
}

// PATResolver resolves a presented X-API-Key to a user via a personal access
// token, checked after the static config tokens. A nil resolver disables PAT
// auth (e.g. when snapshot storage is not configured). Returns ok=false for an
// unknown/expired/revoked token, or on a backend error (auth fails closed).
type PATResolver interface {
	ResolveToken(ctx context.Context, presented string) (*UserContext, bool)
}

// LoginRecorder records a successful interactive sign-in, so operators can see
// who has access and when they last used it. A nil recorder disables the audit
// (storage not configured). Implementations must be safe to fail: login never
// depends on the record being written.
type LoginRecorder interface {
	RecordLogin(ctx context.Context, subject, name, role string) error
}
