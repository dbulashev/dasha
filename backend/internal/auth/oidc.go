package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/dbulashev/dasha/internal/config"
)

// oidcState holds the data derived from OIDC discovery. It is nil until the
// first successful discovery and is published atomically, so the provider shell
// can exist before the IdP is reachable and become usable later via retry.
type oidcState struct {
	verifier      *oidc.IDTokenVerifier
	oauth2Cfg     oauth2.Config
	endSessionURL string
	revocationURL string
}

// OIDCProvider wraps OIDC discovery behind an atomically-published state, so a
// transient failure at startup (slow/unreachable IdP) does not require a restart:
// discovery is retried in the background until it succeeds.
type OIDCProvider struct {
	cfg           config.OIDCConfig
	roleClaim     string
	roleMapping   map[string]string // corporate group → dasha role
	logger        *zap.Logger
	postLogoutURI string
	state         atomic.Pointer[oidcState]
}

const (
	oidcRetryInterval    = 30 * time.Second
	oidcDiscoveryTimeout = 15 * time.Second
)

// NewOIDCProvider builds the provider and attempts OIDC discovery. It always
// returns a non-nil shell: when discovery fails it keeps retrying in the
// background until the IdP is reachable, so SSO self-heals without a restart.
// ctx (the service lifetime) cancels the retry loop.
func NewOIDCProvider(ctx context.Context, cfg config.OIDCConfig, logger *zap.Logger) *OIDCProvider {
	roleClaim := cfg.RoleClaim
	if roleClaim == "" {
		roleClaim = "realm_access.roles"
	}

	postLogoutURI := "/"
	if parsed, err := url.Parse(cfg.RedirectURL); err == nil {
		postLogoutURI = parsed.Scheme + "://" + parsed.Host + "/"
	}

	p := &OIDCProvider{ //nolint:exhaustruct
		cfg:           cfg,
		roleClaim:     roleClaim,
		roleMapping:   cfg.RoleMapping,
		logger:        logger,
		postLogoutURI: postLogoutURI,
	}

	if err := p.discover(ctx); err != nil {
		logger.Warn("OIDC provider initialization failed; retrying in the background until the IdP is reachable", zap.Error(err))

		go p.retryInit(ctx)
	}

	return p
}

// Ready reports whether OIDC discovery has completed and logins can be served.
func (p *OIDCProvider) Ready() bool {
	return p.state.Load() != nil
}

// discover runs one time-bounded discovery attempt and, on success, publishes
// the derived state.
func (p *OIDCProvider) discover(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, p.cfg.IssuerURL)
	if err != nil {
		return fmt.Errorf("OIDC discovery failed for %s: %w", p.cfg.IssuerURL, err)
	}

	scopes := p.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	var providerClaims struct {
		EndSessionURL string `json:"end_session_endpoint"`
		RevocationURL string `json:"revocation_endpoint"`
	}

	_ = provider.Claims(&providerClaims)

	p.state.Store(&oidcState{
		verifier: provider.Verifier(&oidc.Config{ //nolint:exhaustruct
			ClientID: p.cfg.ClientID,
		}),
		oauth2Cfg: oauth2.Config{ //nolint:exhaustruct
			ClientID:     p.cfg.ClientID,
			ClientSecret: p.cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  p.cfg.RedirectURL,
			Scopes:       scopes,
		},
		endSessionURL: providerClaims.EndSessionURL,
		revocationURL: providerClaims.RevocationURL,
	})

	p.logger.Info("OIDC provider initialized", zap.String("issuer", p.cfg.IssuerURL))

	return nil
}

// retryInit re-attempts discovery on a fixed interval until it succeeds or ctx
// is cancelled.
func (p *OIDCProvider) retryInit(ctx context.Context) {
	ticker := time.NewTicker(oidcRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.discover(ctx); err != nil {
				p.logger.Warn("OIDC provider retry failed; will retry", zap.Error(err))

				continue
			}

			return
		}
	}
}

func (p *OIDCProvider) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	s := p.state.Load()
	if s == nil {
		return nil, errOIDCUnavailable
	}

	return s.verifier.Verify(ctx, rawIDToken) //nolint:wrapcheck
}

// AuthCodeURL returns the IdP authorization URL, or "" when discovery has not
// completed yet (callers gate on Ready()).
func (p *OIDCProvider) AuthCodeURL(state string) string {
	s := p.state.Load()
	if s == nil {
		return ""
	}

	return s.oauth2Cfg.AuthCodeURL(state)
}

// Exchange swaps an authorization code for tokens, erroring when not ready.
func (p *OIDCProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	s := p.state.Load()
	if s == nil {
		return nil, errOIDCUnavailable
	}

	return s.oauth2Cfg.Exchange(ctx, code) //nolint:wrapcheck
}

func (p *OIDCProvider) ExtractRole(claims map[string]any) string {
	role := extractRoleFromClaims(claims, p.roleClaim, p.roleMapping)

	val := nestedValue(claims, strings.Split(p.roleClaim, "."))

	p.logger.Debug("OIDC role extraction",
		zap.String("role_claim", p.roleClaim),
		zap.Any("claim_values", val),
		zap.Any("role_mapping", p.roleMapping),
		zap.String("resolved_role", role),
	)

	return role
}

// TokenSource returns a refreshing token source, or nil when discovery has not
// completed yet (callers must nil-check).
func (p *OIDCProvider) TokenSource(ctx context.Context, refreshToken string) oauth2.TokenSource {
	s := p.state.Load()
	if s == nil {
		return nil
	}

	return s.oauth2Cfg.TokenSource(ctx, &oauth2.Token{ //nolint:exhaustruct
		RefreshToken: refreshToken,
	})
}

func (p *OIDCProvider) LogoutURL(idTokenHint string) string {
	s := p.state.Load()
	if s == nil || s.endSessionURL == "" {
		return ""
	}

	u, err := url.Parse(s.endSessionURL)
	if err != nil {
		return ""
	}

	q := u.Query()
	q.Set("client_id", s.oauth2Cfg.ClientID)
	q.Set("post_logout_redirect_uri", p.postLogoutURI)

	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}

	u.RawQuery = q.Encode()

	return u.String()
}

func (p *OIDCProvider) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	s := p.state.Load()
	if s == nil || s.revocationURL == "" || refreshToken == "" {
		return nil
	}

	data := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {s.oauth2Cfg.ClientID},
		"client_secret":   {s.oauth2Cfg.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.revocationURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create revocation request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("revocation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("revocation returned status %d", resp.StatusCode)
	}

	return nil
}

func extractRoleFromClaims(claims map[string]any, claimPath string, roleMapping map[string]string) string {
	val := nestedValue(claims, strings.Split(claimPath, "."))

	roles, ok := val.([]any)
	if !ok {
		return defaultRole
	}

	if len(roleMapping) > 0 {
		// Use explicit mapping: first match with highest privilege wins.
		for _, r := range roles {
			s, ok := r.(string)
			if !ok {
				continue
			}

			if mapped, exists := roleMapping[s]; exists && mapped == "admin" {
				return "admin"
			}
		}

		// Check for viewer mappings (admin takes priority, so check separately).
		for _, r := range roles {
			s, ok := r.(string)
			if !ok {
				continue
			}

			if mapped, exists := roleMapping[s]; exists && mapped == "viewer" {
				return "viewer"
			}
		}

		return defaultRole
	}

	// Default behavior: look for "admin" in claim values.
	for _, r := range roles {
		if s, ok := r.(string); ok && s == "admin" {
			return "admin"
		}
	}

	return defaultRole
}

const defaultRole = "viewer"

func nestedValue(m map[string]any, keys []string) any {
	var cur any = m

	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}

		cur = mm[k]
	}

	return cur
}
