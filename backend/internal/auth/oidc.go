package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/dbulashev/dasha/internal/config"
)

type OIDCProvider struct {
	provider      *oidc.Provider
	verifier      *oidc.IDTokenVerifier
	OAuth2Cfg     oauth2.Config
	roleClaim     string
	roleMapping   map[string]string // corporate group → dasha role
	logger        *zap.Logger
	endSessionURL string
	revocationURL string
	postLogoutURI string
}

func NewOIDCProvider(ctx context.Context, cfg config.OIDCConfig, logger *zap.Logger) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed for %s: %w", cfg.IssuerURL, err)
	}

	verifier := provider.Verifier(&oidc.Config{ //nolint:exhaustruct
		ClientID: cfg.ClientID,
	})

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	oauth2Cfg := oauth2.Config{ //nolint:exhaustruct
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
	}

	roleClaim := cfg.RoleClaim
	if roleClaim == "" {
		roleClaim = "realm_access.roles"
	}

	var providerClaims struct {
		EndSessionURL string `json:"end_session_endpoint"`
		RevocationURL string `json:"revocation_endpoint"`
	}

	_ = provider.Claims(&providerClaims)

	postLogoutURI := "/"
	if parsed, err := url.Parse(cfg.RedirectURL); err == nil {
		postLogoutURI = parsed.Scheme + "://" + parsed.Host + "/"
	}

	return &OIDCProvider{
		provider:      provider,
		verifier:      verifier,
		OAuth2Cfg:     oauth2Cfg,
		roleClaim:     roleClaim,
		roleMapping:   cfg.RoleMapping,
		logger:        logger,
		endSessionURL: providerClaims.EndSessionURL,
		revocationURL: providerClaims.RevocationURL,
		postLogoutURI: postLogoutURI,
	}, nil
}

func (p *OIDCProvider) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	return p.verifier.Verify(ctx, rawIDToken) //nolint:wrapcheck
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

func (p *OIDCProvider) TokenSource(ctx context.Context, refreshToken string) oauth2.TokenSource {
	return p.OAuth2Cfg.TokenSource(ctx, &oauth2.Token{ //nolint:exhaustruct
		RefreshToken: refreshToken,
	})
}

func (p *OIDCProvider) LogoutURL(idTokenHint string) string {
	if p.endSessionURL == "" {
		return ""
	}

	u, err := url.Parse(p.endSessionURL)
	if err != nil {
		return ""
	}

	q := u.Query()
	q.Set("client_id", p.OAuth2Cfg.ClientID)
	q.Set("post_logout_redirect_uri", p.postLogoutURI)

	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}

	u.RawQuery = q.Encode()

	return u.String()
}

func (p *OIDCProvider) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if p.revocationURL == "" || refreshToken == "" {
		return nil
	}

	data := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
		"client_id":       {p.OAuth2Cfg.ClientID},
		"client_secret":   {p.OAuth2Cfg.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.revocationURL, strings.NewReader(data.Encode()))
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
