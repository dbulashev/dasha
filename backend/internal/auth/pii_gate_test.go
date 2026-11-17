package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
)

type accessLog struct{ logs *observer.ObservedLogs }

type accessEntry struct {
	decision, principal, method, operation, cluster, database, ip string
}

func (l accessLog) records() []accessEntry {
	var out []accessEntry

	for _, e := range l.logs.FilterMessage("pii_catalog access").All() {
		f := e.ContextMap()
		if f["audit"] != true {
			continue
		}

		out = append(out, accessEntry{
			decision: f["decision"].(string), principal: f["principal"].(string), method: f["auth_method"].(string),
			operation: f["operation"].(string), cluster: f["cluster_name"].(string), database: f["database"].(string),
			ip: f["remote_ip"].(string),
		})
	}

	return out
}

func newAccessLog() (*zap.Logger, accessLog) {
	core, logs := observer.New(zap.InfoLevel)

	return zap.New(core), accessLog{logs: logs}
}

type fakePATResolver map[string]*UserContext

func (f fakePATResolver) ResolveToken(_ context.Context, presented string) (*UserContext, bool) {
	u, ok := f[presented]

	return u, ok
}

const (
	classifierSecret = "secret-classifier"
	adminSecret      = "secret-admin"
	patSecret        = "dasha_pat_x"
)

var gateTokens = []config.AuthToken{
	{Name: "pii-classifier", Token: classifierSecret, Role: config.RoleViewer},
	{Name: "ops-admin", Token: adminSecret, Role: config.RoleAdmin},
}

type gateEnv struct {
	e       *echo.Echo
	audit   accessLog
	reached *bool
}

// newGateEnv wires the gate the way http.New does, behind a real auth
// middleware, so c.Path() and the user are set as in production.
func newGateEnv(t *testing.T, mode config.AuthMode, enabled bool, allowed []string) gateEnv {
	t.Helper()

	cfg := config.AuthConfig{Mode: mode, Tokens: gateTokens} //nolint:exhaustruct
	resolver := fakePATResolver{
		patSecret: {Name: "pii-classifier", Role: config.RoleAdmin, AuthMethod: MethodPAT},
	}
	authMW := NewAuthMiddleware(cfg, nil, nil, resolver, zap.NewNop())

	logger, audit := newAccessLog()
	gate := NewPIICatalogGate(config.PIICatalogConfig{Enabled: enabled, AllowedTokens: allowed}, logger) //nolint:exhaustruct

	reached := new(bool)
	ok := func(c echo.Context) error {
		*reached = true

		return c.String(http.StatusOK, "ok")
	}

	e := echo.New()
	e.Use(gate.WrapAuth(authMW))
	e.Use(gate.Middleware)
	e.GET("/api/pii-catalog/status", ok)
	e.GET("/api/pii-catalog/pairs", ok)
	e.GET("/api/clusters", ok)

	return gateEnv{e: e, audit: audit, reached: reached}
}

func (g gateEnv) do(path string, setup func(*http.Request)) *httptest.ResponseRecorder {
	*g.reached = false
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "10.1.2.3:5555"

	if setup != nil {
		setup(req)
	}

	rec := httptest.NewRecorder()
	g.e.ServeHTTP(rec, req)

	return rec
}

func withKey(key string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("X-API-Key", key) }
}

func TestPIICatalogGate_Decisions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		mode       config.AuthMode
		disabled   bool
		allowed    []string
		key        string
		wantStatus int
		wantDec    string
		wantPrinc  string
		wantMethod string
	}{
		{"listed static viewer token", config.AuthModeToken, false, []string{"pii-classifier"}, classifierSecret,
			http.StatusOK, "allowed", "pii-classifier", "token"},
		{"listed static token under oidc", config.AuthModeOIDC, false, []string{"pii-classifier"}, classifierSecret,
			http.StatusOK, "allowed", "pii-classifier", "token"},
		{"unlisted static admin token", config.AuthModeToken, false, []string{"pii-classifier"}, adminSecret,
			http.StatusForbidden, "denied", "ops-admin", "token"},
		{"admin PAT named like a listed token", config.AuthModeOIDC, false, []string{"pii-classifier"}, patSecret,
			http.StatusForbidden, "denied", "pii-classifier", "pat"},
		{"empty allowlist", config.AuthModeToken, false, nil, classifierSecret,
			http.StatusForbidden, "denied", "pii-classifier", "token"},
		{"auth mode none", config.AuthModeNone, false, []string{"pii-classifier"}, classifierSecret,
			http.StatusForbidden, "denied", "", "none"},
		{"empty name in allowlist, anonymous caller", config.AuthModeNone, false, []string{""}, "",
			http.StatusForbidden, "denied", "", "none"},
		{"listed token, feature disabled", config.AuthModeToken, true, []string{"pii-classifier"}, classifierSecret,
			http.StatusForbidden, "denied", "pii-classifier", "token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := newGateEnv(t, tc.mode, !tc.disabled, tc.allowed)
			rec := env.do("/api/pii-catalog/status?cluster_name=c1", withKey(tc.key))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if *env.reached != (tc.wantStatus == http.StatusOK) {
				t.Fatalf("handler reached = %v", *env.reached)
			}

			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q", got)
			}

			recs := env.audit.records()
			if len(recs) != 1 {
				t.Fatalf("audit records = %d, want 1", len(recs))
			}

			want := accessEntry{
				decision: tc.wantDec, principal: tc.wantPrinc, method: tc.wantMethod,
				operation: "getPiiCatalogStatus", cluster: "c1", ip: "10.1.2.3",
			}
			if recs[0] != want {
				t.Errorf("record = %+v, want %+v", recs[0], want)
			}
		})
	}
}

func TestPIICatalogGate_OIDCSessionRefused(t *testing.T) {
	t.Parallel()

	logger, audit := newAccessLog()
	gate := NewPIICatalogGate(config.PIICatalogConfig{Enabled: true, AllowedTokens: []string{"alice"}}, logger) //nolint:exhaustruct

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			SetUser(c, &UserContext{Name: "alice", Role: config.RoleAdmin, AuthMethod: MethodOIDC}) //nolint:exhaustruct

			return next(c)
		}
	})
	e.Use(gate.Middleware)
	e.GET("/api/pii-catalog/pairs", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pii-catalog/pairs", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	if recs := audit.records(); len(recs) != 1 || recs[0].decision != "denied" || recs[0].method != "oidc" {
		t.Fatalf("audit = %+v", recs)
	}
}

func TestPIICatalogGate_UnauthenticatedIsAudited(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "wrong"} {
		env := newGateEnv(t, config.AuthModeToken, true, []string{"pii-classifier"})

		rec := env.do("/api/pii-catalog/unknown", withKey(key))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("key %q: status = %d, want 401", key, rec.Code)
		}

		recs := env.audit.records()
		if len(recs) != 1 || recs[0].decision != "denied" || recs[0].principal != "" {
			t.Fatalf("key %q: audit = %+v", key, recs)
		}

		if recs[0].operation != "/api/pii-catalog/unknown" {
			t.Errorf("unrouted path recorded as %q", recs[0].operation)
		}
	}
}

func TestPIICatalogGate_OtherPathsUntouched(t *testing.T) {
	t.Parallel()

	env := newGateEnv(t, config.AuthModeToken, true, nil)

	rec := env.do("/api/clusters", withKey(adminSecret))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if rec.Header().Get("Cache-Control") != "" || len(env.audit.records()) != 0 {
		t.Fatal("gate acted on a path outside the PII catalog")
	}

	rec = env.do("/api/clusters", withKey("wrong"))
	if rec.Code != http.StatusUnauthorized || len(env.audit.records()) != 0 {
		t.Fatalf("status = %d, audit = %d; want 401 and no audit", rec.Code, len(env.audit.records()))
	}
}

func TestAuditText(t *testing.T) {
	t.Parallel()

	if got := auditText("\xffd"); got != "?d" {
		t.Fatalf("auditText = %q", got)
	}
}
