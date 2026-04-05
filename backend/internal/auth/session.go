package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"github.com/dbulashev/dasha/internal/config"
)

const (
	cookieName      = "dasha_session"
	defaultMaxAge   = 86400 // 24 hours
	cookieKeyLen    = 32
	minSecretLen    = 32
	stateCookieName = "oauth_state"
	stateMaxAge     = 300 // 5 minutes
	stateLen        = 32
)

type SessionData struct {
	RefreshToken string `json:"rt"`
	IDToken      string `json:"idt"`
	ExpiresAt    int64  `json:"exp"`
	UserName     string `json:"name"`
	UserEmail    string `json:"email"`
	UserRole     string `json:"role"`
}

type jsonSerializer struct{}

func (j jsonSerializer) Serialize(src any) ([]byte, error) {
	return json.Marshal(src) //nolint:wrapcheck
}

func (j jsonSerializer) Deserialize(src []byte, dst any) error {
	return json.Unmarshal(src, dst) //nolint:wrapcheck
}

type SessionManager struct {
	sc     *securecookie.SecureCookie
	maxAge int
}

func NewSessionManager(cfg config.AuthConfig) *SessionManager {
	maxAge := cfg.CookieMaxAge
	if maxAge == 0 {
		maxAge = defaultMaxAge
	}

	var hashKey, blockKey []byte

	if len(cfg.CookieSecret) >= minSecretLen {
		h1 := sha256.Sum256([]byte("hash:" + cfg.CookieSecret))
		h2 := sha256.Sum256([]byte("block:" + cfg.CookieSecret))
		hashKey = h1[:]
		blockKey = h2[:]
	} else {
		hashKey = securecookie.GenerateRandomKey(cookieKeyLen)
		blockKey = securecookie.GenerateRandomKey(cookieKeyLen)
	}

	sc := securecookie.New(hashKey, blockKey)
	sc.MaxAge(maxAge)
	sc.SetSerializer(jsonSerializer{})

	return &SessionManager{sc: sc, maxAge: maxAge}
}

const maxCookieSize = 4000 // browsers silently drop cookies > 4096 bytes

func (sm *SessionManager) SetSession(c echo.Context, data *SessionData) error {
	encoded, err := sm.sc.Encode(cookieName, data)
	if err != nil {
		return fmt.Errorf("encode session | %w", err)
	}

	if len(encoded) > maxCookieSize && data.IDToken != "" {
		data.IDToken = ""

		encoded, err = sm.sc.Encode(cookieName, data)
		if err != nil {
			return fmt.Errorf("encode session without id_token | %w", err)
		}
	}

	// G124: Secure is set dynamically via isSecureRequest
	c.SetCookie(&http.Cookie{ //nolint:exhaustruct,gosec:disable G124
		Name:     cookieName,
		Value:    encoded,
		Path:     "/",
		MaxAge:   sm.maxAge,
		HttpOnly: true,
		Secure:   isSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

func (sm *SessionManager) GetSession(c echo.Context) (*SessionData, error) {
	cookie, err := c.Cookie(cookieName)
	if err != nil {
		return nil, fmt.Errorf("read session cookie | %w", err)
	}

	var data SessionData

	err = sm.sc.Decode(cookieName, cookie.Value, &data)
	if err != nil {
		return nil, fmt.Errorf("decode session | %w", err)
	}

	return &data, nil
}

// G124: Secure is set dynamically via isSecureRequest
func (sm *SessionManager) ClearSession(c echo.Context) {
	c.SetCookie(&http.Cookie{ //nolint:exhaustruct,gosec:disable G124
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

func (sm *SessionManager) ValidateAndRefresh(c echo.Context, provider *OIDCProvider) (*UserContext, error) {
	session, err := sm.GetSession(c)
	if err != nil {
		return nil, err
	}

	if time.Now().Unix() > session.ExpiresAt && session.RefreshToken != "" {
		tokenSource := provider.TokenSource(c.Request().Context(), session.RefreshToken)

		newToken, err := tokenSource.Token()
		if err != nil {
			sm.ClearSession(c)
			return nil, fmt.Errorf("token refresh failed | %w", err)
		}

		if newToken.RefreshToken != "" {
			session.RefreshToken = newToken.RefreshToken
		}

		session.ExpiresAt = newToken.Expiry.Unix()

		if rawID, ok := newToken.Extra("id_token").(string); ok {
			session.IDToken = rawID

			if idToken, err := provider.VerifyIDToken(c.Request().Context(), rawID); err == nil {
				var claims map[string]any
				if err := idToken.Claims(&claims); err == nil {
					session.UserRole = provider.ExtractRole(claims)
				}
			}
		}

		if err := sm.SetSession(c, session); err != nil {
			return nil, fmt.Errorf("update session after refresh | %w", err)
		}
	}

	return &UserContext{
		Name:       session.UserName,
		Email:      session.UserEmail,
		Role:       session.UserRole,
		AuthMethod: MethodOIDC,
	}, nil
}

func (sm *SessionManager) SetStateCookie(c echo.Context) (string, error) {
	state, err := generateRandomString(stateLen)
	if err != nil {
		return "", fmt.Errorf("generate state | %w", err)
	}

	// G124: Secure is set dynamically via isSecureRequest
	c.SetCookie(&http.Cookie{ //nolint:exhaustruct,gosec:disable G124
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   stateMaxAge,
		HttpOnly: true,
		Secure:   isSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})

	return state, nil
}

func (sm *SessionManager) ValidateStateCookie(c echo.Context) error {
	cookie, err := c.Cookie(stateCookieName)
	if err != nil {
		return fmt.Errorf("missing state cookie | %w", err)
	}

	queryState := c.QueryParam("state")
	if cookie.Value == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(queryState)) != 1 {
		return fmt.Errorf("state mismatch") //nolint:goerr113
	}

	// G124: Secure is set dynamically via isSecureRequest
	c.SetCookie(&http.Cookie{ //nolint:exhaustruct,gosec:disable G124
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

func (sm *SessionManager) ExchangeCode(c echo.Context, provider *OIDCProvider) (*oauth2.Token, error) {
	code := c.QueryParam("code")
	if code == "" {
		return nil, fmt.Errorf("missing authorization code") //nolint:goerr113
	}

	token, err := provider.OAuth2Cfg.Exchange(c.Request().Context(), code)
	if err != nil {
		return nil, fmt.Errorf("token exchange | %w", err)
	}

	return token, nil
}

func isSecureRequest(c echo.Context) bool {
	if c.Scheme() == "https" {
		return true
	}

	return c.Request().Header.Get("X-Forwarded-Proto") == "https"
}

func generateRandomString(nBytes int) (string, error) {
	b := make([]byte, nBytes)

	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("rand.Read | %w", err)
	}

	return hex.EncodeToString(b), nil
}
