package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

func testOIDCIssuer(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value any) string {
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			response = map[string]any{
				"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize",
				"token_endpoint": issuer.URL + "/token", "jwks_uri": issuer.URL + "/keys",
				"end_session_endpoint":                  issuer.URL + "/logout",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			}
		case "/keys":
			response = map[string]any{"keys": []any{map[string]any{
				"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
			}}}
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid form", http.StatusBadRequest)
				return
			}
			if r.Form.Get("code") != "valid" {
				w.WriteHeader(http.StatusBadRequest)
				response = map[string]string{"error": "invalid_grant"}
				break
			}
			unsigned := encode(map[string]string{"alg": "RS256", "kid": "test"}) + "." +
				encode(map[string]any{
					"iss": issuer.URL, "aud": "dasha-test", "sub": "test-user",
					"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
					"preferred_username": "test-user", "email": "test@example.test", "groups": []string{"sysops"},
				})
			digest := sha256.Sum256([]byte(unsigned))
			signature, signErr := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if signErr != nil {
				http.Error(w, "Signing failed", http.StatusInternalServerError)
				return
			}
			response = map[string]any{
				"access_token": "test-access-token", "token_type": "Bearer", "expires_in": 3600,
				"refresh_token": "test-refresh-token", "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature),
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("test IdP response failed: %v", err)
		}
	}))
	t.Cleanup(issuer.Close)
	return issuer.URL
}

func TestOIDCBasePath(t *testing.T) {
	issuer := testOIDCIssuer(t)
	for _, prefix := range []string{"", "/dasha", "/tools/dasha"} {
		t.Run(prefix, func(t *testing.T) {
			cfg := config.OIDCConfig{ //nolint:exhaustruct
				IssuerURL: issuer, ClientID: "dasha-test", ClientSecret: "local-test-only",
				RedirectURL: "https://dashboard.example.test" + prefix + "/auth/callback",
				RoleClaim:   "groups", RoleMapping: map[string]string{"sysops": "admin"}, DenyUnmapped: true,
			}
			provider := NewOIDCProvider(t.Context(), cfg, prefix, zap.NewNop())
			if !provider.Ready() {
				t.Fatal("test IdP is not ready")
			}
			sm := NewSessionManager(config.AuthConfig{CookieSecret: strings.Repeat("x", 32)}, prefix, zap.NewNop()) //nolint:exhaustruct
			e := echo.New()
			RegisterBFFRoutes(e, provider, sm, nil, zap.NewNop())
			request := func(method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("X-Forwarded-Proto", "https")
				for _, cookie := range cookies {
					req.AddCookie(cookie)
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				for _, cookie := range rec.Result().Cookies() {
					if cookie.Path != prefix+"/" || !cookie.HttpOnly || !cookie.Secure {
						t.Errorf("incorrect cookie scope or security: %s", cookie.Name)
					}
				}
				return rec
			}
			login := request(http.MethodGet, "/auth/login")
			loginURL, err := url.Parse(login.Header().Get("Location"))
			if err != nil || login.Code != http.StatusFound {
				t.Fatalf("Login: %d, %v", login.Code, err)
			}
			if loginURL.Query().Get("redirect_uri") != cfg.RedirectURL {
				t.Fatal("callback lost the prefix")
			}
			state := loginURL.Query().Get("state")
			callback := request(http.MethodGet, "/auth/callback?code=valid&state="+url.QueryEscape(state), login.Result().Cookies()...)
			if callback.Code != http.StatusFound || callback.Header().Get("Location") != prefix+"/" {
				t.Fatalf("Callback: %d, %s", callback.Code, callback.Body.String())
			}
			session := callback.Result().Cookies()
			me := request(http.MethodGet, "/auth/me", session...)
			if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"admin"`) {
				t.Fatalf("session failed: %d, %s", me.Code, me.Body.String())
			}
			logout := request(http.MethodPost, "/auth/logout", session...)
			var body map[string]string
			if err := json.Unmarshal(logout.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			logoutURL, err := url.Parse(body["logout_url"])
			if err != nil || logoutURL.Query().Get("post_logout_redirect_uri") != "https://dashboard.example.test"+prefix+"/" {
				t.Fatalf("logout lost the prefix: %v", body)
			}
			invalid := request(http.MethodGet, "/auth/callback?error=access_denied")
			if invalid.Header().Get("Location") != prefix+"/auth/login" {
				t.Fatal("failed callback escaped the prefix")
			}
			failed := request(http.MethodGet, "/auth/callback?code=invalid&state="+url.QueryEscape(state), login.Result().Cookies()...)
			if failed.Code != http.StatusBadGateway ||
				!strings.Contains(failed.Body.String(), `href="`+prefix+`/auth/login"`) ||
				!strings.Contains(failed.Body.String(), `src="`+prefix+`/dasha-appology.png"`) {
				t.Fatal("error page escaped the prefix")
			}
			expired := httptest.NewRecorder()
			err = sm.SetSession(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), expired),
				&SessionData{ExpiresAt: time.Now().Add(-time.Hour).Unix(), RefreshToken: "invalid"}) //nolint:exhaustruct
			if err != nil {
				t.Fatal(err)
			}
			me = request(http.MethodGet, "/auth/me", expired.Result().Cookies()...)
			if me.Code != http.StatusUnauthorized {
				t.Fatal("expired session remained valid")
			}
		})
	}
}

func TestBFFUnavailableBasePath(t *testing.T) {
	ready := NewOIDCProvider(t.Context(), config.OIDCConfig{ //nolint:exhaustruct
		IssuerURL: testOIDCIssuer(t), ClientID: "dasha-test",
	}, "", zap.NewNop())
	if !ready.Ready() {
		t.Fatal("test IdP is not ready")
	}
	for _, tc := range []struct {
		name     string
		prefix   string
		sessions bool
	}{
		{name: "nil sessions"},
		{name: "root", sessions: true},
		{name: "subpath", prefix: "/tools/dasha", sessions: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sm *SessionManager
			if tc.sessions {
				sm = NewSessionManager(config.AuthConfig{CookieSecret: strings.Repeat("x", 32)}, tc.prefix, zap.NewNop()) //nolint:exhaustruct
			}
			providers := []*OIDCProvider{nil, {}}
			if sm == nil {
				providers = append(providers, ready)
			}
			for _, provider := range providers {
				e := echo.New()
				RegisterBFFRoutes(e, provider, sm, nil, zap.NewNop())
				for _, path := range []string{"/auth/login", "/auth/callback"} {
					rec := httptest.NewRecorder()
					e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
					if rec.Code != http.StatusServiceUnavailable {
						t.Fatalf("%s: status = %d, want 503", path, rec.Code)
					}
					if !strings.Contains(rec.Body.String(), `src="`+tc.prefix+`/dasha-appology.png"`) {
						t.Errorf("%s: error page lost the asset prefix", path)
					}
					if path == "/auth/login" && !strings.Contains(rec.Body.String(), `href="`+tc.prefix+`/auth/login"`) {
						t.Error("login error page lost the retry prefix")
					}
				}
			}
		})
	}
}

func TestAccessDeniedBasePath(t *testing.T) {
	for _, prefix := range []string{"", "/tools/dasha"} {
		t.Run(prefix, func(t *testing.T) {
			e := echo.New()
			rec := httptest.NewRecorder()
			if err := renderAccessDenied(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec), prefix); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `src="`+prefix+`/dasha-appology.png"`) {
				t.Fatalf("access denied page: status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}
