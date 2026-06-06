package auth

import (
	_ "embed"
	"html/template"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

//go:embed oidc_unavailable.html
var oidcUnavailableHTMLRaw string

var oidcUnavailableTmpl = template.Must(template.New("oidc_unavailable").Parse(oidcUnavailableHTMLRaw))

const defaultOIDCUnavailableMessage = "Single Sign-On is misconfigured. Please contact your administrator."

func RegisterBFFRoutes(e *echo.Echo, provider *OIDCProvider, sm *SessionManager, logger *zap.Logger) {
	e.GET("/auth/login", loginHandler(provider, sm, logger))
	e.GET("/auth/callback", callbackHandler(provider, sm, logger))
	e.POST("/auth/logout", logoutHandler(sm, provider, logger))
	e.GET("/auth/me", meHandler(sm, provider))
}

// renderOIDCUnavailable renders the apology page with a custom message and optional "Try again" link.
// status: HTTP status code to return (typically 503 for misconfig, 502 for IDP errors).
// message: user-facing description; empty falls back to the generic admin-contact text.
// showRetry: when true, includes a link to /auth/login so the user can retry.
func renderOIDCUnavailable(c echo.Context, status int, message string, showRetry bool) error {
	if message == "" {
		message = defaultOIDCUnavailableMessage
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(status)

	return oidcUnavailableTmpl.Execute(c.Response().Writer, map[string]any{
		"Message":   message,
		"ShowRetry": showRetry,
	})
}

func loginHandler(provider *OIDCProvider, sm *SessionManager, logger *zap.Logger) echo.HandlerFunc {
	return func(c echo.Context) error {
		if provider == nil {
			logger.Warn("SSO login requested but OIDC provider is not initialized")
			return renderOIDCUnavailable(c, http.StatusServiceUnavailable, "", false)
		}

		state, err := sm.SetStateCookie(c)
		if err != nil {
			logger.Error("failed to generate state", zap.Error(err))
			return renderOIDCUnavailable(c, http.StatusInternalServerError, "Failed to start login: state cookie could not be generated.", true)
		}

		return c.Redirect(http.StatusFound, provider.OAuth2Cfg.AuthCodeURL(state))
	}
}

func callbackHandler(provider *OIDCProvider, sm *SessionManager, logger *zap.Logger) echo.HandlerFunc {
	return func(c echo.Context) error {
		if provider == nil {
			logger.Warn("OIDC callback received but provider is not initialized")
			return renderOIDCUnavailable(c, http.StatusServiceUnavailable, "", false)
		}

		if err := sm.ValidateStateCookie(c); err != nil {
			logger.Warn("invalid OAuth state, redirecting to login", zap.Error(err))
			return c.Redirect(http.StatusFound, "/auth/login")
		}

		oauth2Token, err := sm.ExchangeCode(c, provider)
		if err != nil {
			logger.Error("token exchange failed", zap.Error(err))
			return renderOIDCUnavailable(c, http.StatusBadGateway, "Token exchange with the identity provider failed. Please try logging in again.", true)
		}

		rawIDToken, ok := oauth2Token.Extra("id_token").(string)
		if !ok {
			logger.Error("OIDC token response did not include id_token")
			return renderOIDCUnavailable(c, http.StatusBadGateway, "Identity provider response did not include an id_token.", true)
		}

		idToken, err := provider.VerifyIDToken(c.Request().Context(), rawIDToken)
		if err != nil {
			logger.Error("ID token verification failed", zap.Error(err))
			return renderOIDCUnavailable(c, http.StatusUnauthorized, "Identity provider returned an invalid id_token.", true)
		}

		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			logger.Error("failed to parse claims", zap.Error(err))
			return renderOIDCUnavailable(c, http.StatusInternalServerError, "Failed to parse identity claims from the id_token.", true)
		}

		name, _ := claims["preferred_username"].(string)
		email, _ := claims["email"].(string)
		role := provider.ExtractRole(claims)

		if err := sm.SetSession(c, &SessionData{
			RefreshToken: oauth2Token.RefreshToken,
			IDToken:      rawIDToken,
			ExpiresAt:    oauth2Token.Expiry.Unix(),
			UserName:     name,
			UserEmail:    email,
			UserRole:     role,
		}); err != nil {
			logger.Error("failed to set session", zap.Error(err))
			return renderOIDCUnavailable(c, http.StatusInternalServerError, "Failed to create session after successful authentication.", true)
		}

		logger.Info("user authenticated via OIDC",
			zap.String("user", name),
			zap.String("role", role),
		)

		return c.Redirect(http.StatusFound, "/")
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
