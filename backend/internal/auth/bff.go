package auth

import (
	"context"
	_ "embed"
	"html/template"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

//go:embed oidc_unavailable.html
var oidcUnavailableHTMLRaw string

var oidcUnavailableTmpl = template.Must(template.New("oidc_unavailable").Parse(oidcUnavailableHTMLRaw))

const defaultOIDCUnavailableMessage = "Single Sign-On is misconfigured. Please contact your administrator."

// loginRecordTimeout bounds the sign-in audit write. The session is already
// established when it runs, so a stalled storage backend (exhausted pool, hung
// connection) must not hold up the user's redirect for an audit row we are
// willing to lose anyway.
const loginRecordTimeout = 2 * time.Second

func RegisterBFFRoutes(e *echo.Echo, provider *OIDCProvider, sm *SessionManager, rec LoginRecorder, logger *zap.Logger) {
	basePath := ""
	if sm != nil {
		basePath = sm.basePath
	}
	e.GET("/auth/login", loginHandler(provider, sm, logger, basePath))
	e.GET("/auth/callback", callbackHandler(provider, sm, rec, logger, basePath))
	e.POST("/auth/logout", logoutHandler(sm, provider, logger))
	e.GET("/auth/me", meHandler(sm, provider))
}

// renderOIDCUnavailable renders the apology page with an optional retry link.
func renderOIDCUnavailable(c echo.Context, basePath string, status int, message string, showRetry bool) error {
	if message == "" {
		message = defaultOIDCUnavailableMessage
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(status)

	return oidcUnavailableTmpl.Execute(c.Response().Writer, map[string]any{
		"Title":     "Authentication is unavailable",
		"Message":   message,
		"ShowRetry": showRetry,
		"LogoURL":   basePath + "/dasha-appology.png",
		"LoginURL":  basePath + "/auth/login",
	})
}

func renderAccessDenied(c echo.Context, basePath string) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusForbidden)

	return oidcUnavailableTmpl.Execute(c.Response().Writer, map[string]any{
		"Title":     "Access denied",
		"Message":   "Your account is not in any group that grants access to Dasha. Please contact your administrator.",
		"ShowRetry": false,
		"LogoURL":   basePath + "/dasha-appology.png",
	})
}

func loginHandler(provider *OIDCProvider, sm *SessionManager, logger *zap.Logger, basePath string) echo.HandlerFunc {
	return func(c echo.Context) error {
		if sm == nil || provider == nil || !provider.Ready() {
			logger.Warn("SSO login requested but OIDC provider is not ready (discovery pending or failed)")
			return renderOIDCUnavailable(c, basePath, http.StatusServiceUnavailable, "Single Sign-On is starting up or the identity provider is unreachable. Please try again shortly.", true)
		}

		state, err := sm.SetStateCookie(c)
		if err != nil {
			logger.Error("failed to generate state", zap.Error(err))
			return renderOIDCUnavailable(c, basePath, http.StatusInternalServerError, "Failed to start login: state cookie could not be generated.", true)
		}

		return c.Redirect(http.StatusFound, provider.AuthCodeURL(state))
	}
}

func callbackHandler(provider *OIDCProvider, sm *SessionManager, rec LoginRecorder, logger *zap.Logger, basePath string) echo.HandlerFunc {
	return func(c echo.Context) error {
		if sm == nil || provider == nil || !provider.Ready() {
			logger.Warn("OIDC callback received but provider is not ready")
			return renderOIDCUnavailable(c, basePath, http.StatusServiceUnavailable, "", false)
		}

		if err := sm.ValidateStateCookie(c); err != nil {
			logger.Warn("invalid OAuth state, redirecting to login", zap.Error(err))
			return c.Redirect(http.StatusFound, sm.basePath+"/auth/login")
		}

		oauth2Token, err := sm.ExchangeCode(c, provider)
		if err != nil {
			logger.Error("token exchange failed", zap.Error(err))
			return renderOIDCUnavailable(c, basePath, http.StatusBadGateway, "Token exchange with the identity provider failed. Please try logging in again.", true)
		}

		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			logger.Error("OIDC token response did not include id_token")
			return renderOIDCUnavailable(c, basePath, http.StatusBadGateway, "Identity provider response did not include an id_token.", true)
		}

		idToken, err := provider.VerifyIDToken(c.Request().Context(), rawIDToken)
		if err != nil {
			logger.Error("ID token verification failed", zap.Error(err))
			return renderOIDCUnavailable(c, basePath, http.StatusUnauthorized, "Identity provider returned an invalid id_token.", true)
		}

		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			logger.Error("failed to parse claims", zap.Error(err))
			return renderOIDCUnavailable(c, basePath, http.StatusInternalServerError, "Failed to parse identity claims from the id_token.", true)
		}

		name, _ := claims["preferred_username"].(string)
		email, _ := claims["email"].(string)
		role, ok := provider.ExtractRole(claims)
		if !ok {
			logger.Warn("OIDC login denied: no role_mapping entry matches the user's claims",
				zap.String("user", name),
				zap.String("email", email),
			)

			if err := provider.RevokeRefreshToken(c.Request().Context(), oauth2Token.RefreshToken); err != nil {
				logger.Warn("failed to revoke refresh token", zap.Error(err))
			}

			return renderAccessDenied(c, basePath)
		}

		if err := sm.SetSession(c, &SessionData{
			RefreshToken: oauth2Token.RefreshToken,
			IDToken:      rawIDToken,
			ExpiresAt:    oauth2Token.Expiry.Unix(),
			UserName:     name,
			UserEmail:    email,
			UserRole:     role,
		}); err != nil {
			logger.Error("failed to set session", zap.Error(err))
			return renderOIDCUnavailable(c, basePath, http.StatusInternalServerError, "Failed to create session after successful authentication.", true)
		}

		recordLogin(c.Request().Context(), rec, email, name, role, logger)

		logger.Info("user authenticated via OIDC",
			zap.String("user", name),
			zap.String("role", role),
		)

		return c.Redirect(http.StatusFound, sm.basePath+"/")
	}
}

// recordLogin stamps the sign-in in the user audit table. The session is already
// established at this point, so a storage failure — including the write running
// past loginRecordTimeout — is logged and swallowed rather than turned into a
// failed login. Email keys the row (it is what api_tokens.subject stores); an IdP
// that issues no email claim gets no audit row rather than a row under a
// non-unique key that could collide with another principal.
func recordLogin(ctx context.Context, rec LoginRecorder, email, name, role string, logger *zap.Logger) {
	if rec == nil || email == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, loginRecordTimeout)
	defer cancel()

	if err := rec.RecordLogin(ctx, email, name, role); err != nil {
		logger.Warn("failed to record login", zap.String("user", name), zap.Error(err))
	}
}

func logoutHandler(sm *SessionManager, provider *OIDCProvider, logger *zap.Logger) echo.HandlerFunc {
	return func(c echo.Context) error {
		session, err := sm.GetSession(c)
		if err != nil {
			return errNoActiveSession
		}

		sm.ClearSession(c)

		var logoutURL string

		if provider != nil {
			if err := provider.RevokeRefreshToken(c.Request().Context(), session.RefreshToken); err != nil {
				logger.Warn("failed to revoke refresh token", zap.Error(err))
			}

			logoutURL = provider.LogoutURL(session.IDToken)
		}

		return c.JSON(http.StatusOK, map[string]string{
			"status":     "ok",
			"logout_url": logoutURL,
		})
	}
}

func meHandler(sm *SessionManager, provider *OIDCProvider) echo.HandlerFunc {
	return func(c echo.Context) error {
		if provider == nil {
			return errUnauthorized
		}

		user, err := sm.ValidateAndRefresh(c, provider)
		if err != nil {
			return errUnauthorized
		}

		return c.JSON(http.StatusOK, map[string]string{
			"name":  user.Name,
			"email": user.Email,
			"role":  user.Role,
		})
	}
}
