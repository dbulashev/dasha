package auth

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const PIICatalogPathPrefix = "/api/pii-catalog/"

// Query values are client-controlled; the log keeps a bounded prefix.
const piiAuditMaxValue = 256

var piiOperations = map[string]string{
	"/api/pii-catalog/status": "getPiiCatalogStatus",
	"/api/pii-catalog/pairs":  "getPiiCatalogPairs",
}

type piiGateError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PIICatalogGate admits to /api/pii-catalog/* only static tokens
// (auth.tokens) whose name is in the allowed list, and logs every access with
// audit=true. Role is not consulted: PATs and sessions are refused even for
// admin.
type PIICatalogGate struct {
	allowed map[string]struct{}
	logger  *zap.Logger
}

func NewPIICatalogGate(allowed []string, logger *zap.Logger) *PIICatalogGate {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}

	return &PIICatalogGate{allowed: set, logger: logger}
}

// Middleware runs after authentication. In auth.mode none no user is ever set
// and everything is refused.
func (g *PIICatalogGate) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if !isPIICatalogRequest(c) {
			return next(c)
		}

		c.Response().Header().Set("Cache-Control", "no-store")

		user := GetUser(c)

		if _, listed := g.allowed[piiTokenName(user)]; !listed {
			g.log(c, user, false)

			return c.JSON(http.StatusForbidden, piiGateError{
				Code:    "forbidden",
				Message: "the PII catalog is open only to static tokens listed in pii_catalog.allowed_tokens",
			})
		}

		g.log(c, user, true)

		return next(c)
	}
}

// WrapAuth logs, as anonymous denials, the requests to the catalog that the
// authentication middleware itself rejects (no key, unknown key): those never
// reach Middleware.
func (g *PIICatalogGate) WrapAuth(authMW echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		authed := authMW(next)

		return func(c echo.Context) error {
			if !isPIICatalogRequest(c) {
				return authed(c)
			}

			err := authed(c)

			var he *echo.HTTPError
			if errors.As(err, &he) && he.Code == http.StatusUnauthorized && GetUser(c) == nil {
				c.Response().Header().Set("Cache-Control", "no-store")
				g.log(c, nil, false)
			}

			return err
		}
	}
}

// Both the route template and the raw path: an unmatched path under the prefix
// has no template but must still be refused and logged like the rest.
func isPIICatalogRequest(c echo.Context) bool {
	return strings.HasPrefix(c.Path(), PIICatalogPathPrefix) ||
		strings.HasPrefix(c.Request().URL.Path, PIICatalogPathPrefix)
}

// Empty unless the caller is a static config token: a PAT or an OIDC subject
// could share a name with an allowed token.
func piiTokenName(user *UserContext) string {
	if user == nil || user.AuthMethod != MethodToken || user.Name == "" {
		return ""
	}

	return user.Name
}

func (g *PIICatalogGate) log(c echo.Context, user *UserContext, allowed bool) {
	operation := piiOperations[c.Path()]
	if operation == "" {
		operation = auditText(c.Request().URL.Path)
	}

	principal, method := "", "none"
	if user != nil {
		principal, method = user.Name, string(user.AuthMethod)
	}

	decision, write := "denied", g.logger.Warn
	if allowed {
		decision, write = "allowed", g.logger.Info
	}

	write("pii_catalog access",
		zap.Bool("audit", true),
		zap.String("decision", decision),
		zap.String("principal", principal),
		zap.String("auth_method", method),
		zap.String("operation", operation),
		zap.String("cluster_name", auditText(c.QueryParam("cluster_name"))),
		zap.String("database", auditText(c.QueryParam("database"))),
		zap.String("remote_ip", c.RealIP()),
	)
}

func auditText(s string) string {
	return truncateUTF8(strings.ToValidUTF8(s, "�"), piiAuditMaxValue)
}

// s must be valid UTF-8: only a rune cut at the boundary is dropped.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	s = s[:maxBytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}

	return s
}
