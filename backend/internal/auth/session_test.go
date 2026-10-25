package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/dbulashev/dasha/internal/config"
)

func TestValidateAndRefreshWithoutIDToken(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","refresh_token":"rt2","expires_in":300}`))
	}))
	defer idp.Close()

	tests := []struct {
		name     string
		deny     bool
		wantErr  bool
		wantRole string
	}{
		{name: "deny off keeps the session role", deny: false, wantErr: false, wantRole: config.RoleAdmin},
		{name: "deny on logs the user out", deny: true, wantErr: true, wantRole: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapping := map[string]string{"dba_team": config.RoleAdmin}
			p := &OIDCProvider{ //nolint:exhaustruct
				cfg:         config.OIDCConfig{RoleMapping: mapping, DenyUnmapped: tt.deny}, //nolint:exhaustruct
				roleClaim:   "groups",
				roleMapping: mapping,
				logger:      zap.NewNop(),
			}
			p.state.Store(&oidcState{ //nolint:exhaustruct
				oauth2Cfg: oauth2.Config{ //nolint:exhaustruct
					ClientID: "dasha",
					Endpoint: oauth2.Endpoint{TokenURL: idp.URL}, //nolint:exhaustruct
				},
			})

			sm := NewSessionManager(config.AuthConfig{CookieSecret: "test-secret-at-least-32-bytes-long!!"}, zap.NewNop()) //nolint:exhaustruct
			e := echo.New()

			seed := httptest.NewRecorder()
			if err := sm.SetSession(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), seed), &SessionData{ //nolint:exhaustruct
				RefreshToken: "rt1",
				ExpiresAt:    time.Now().Add(-time.Minute).Unix(),
				UserName:     "alice",
				UserRole:     config.RoleAdmin,
			}); err != nil {
				t.Fatalf("seed session: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
			for _, ck := range seed.Result().Cookies() {
				req.AddCookie(ck)
			}

			rec := httptest.NewRecorder()

			user, err := sm.ValidateAndRefresh(e.NewContext(req, rec), p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateAndRefresh() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				cleared := false

				for _, ck := range rec.Result().Cookies() {
					if ck.Name == cookieName && ck.MaxAge < 0 {
						cleared = true
					}
				}

				if !cleared {
					t.Error("session cookie was not cleared")
				}

				return
			}

			if user.Role != tt.wantRole {
				t.Errorf("role = %q, want %q", user.Role, tt.wantRole)
			}
		})
	}
}
