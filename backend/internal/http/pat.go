package http

import (
	"context"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/pkg/pat"
)

// maxExpiresInDays caps a token lifetime at 10 years, matching the swagger max,
// so the day→duration conversion cannot overflow int64.
const maxExpiresInDays = 3650

// maxAdminExpiresInDays caps an admin token at 30 days. An admin token carries
// full write access, and its role is frozen at mint time — it keeps working even
// after the owner is demoted or leaves, because resolution reads the role stored
// on the token rather than the identity provider. A bounded lifetime is the only
// mechanism that expires such a token without someone remembering to revoke it.
const maxAdminExpiresInDays = 30

// patSubject is the stable owner key for a user's personal access tokens: the
// OIDC email. Personal tokens belong to an individually-identifiable principal,
// so only an OIDC session with a non-empty email qualifies; ok=false for shared
// static config tokens (which carry no unique per-user identity and would let a
// leaked shared token mint tokens that outlive it) and for PAT-authenticated
// callers (anti-chaining). Returning ok=false — rather than falling back to the
// non-unique, possibly-empty Name — prevents distinct identities from colliding
// on one token namespace.
func patSubject(u *auth.UserContext) (string, bool) {
	if u == nil || u.AuthMethod != auth.MethodOIDC || u.Email == "" {
		return "", false
	}

	return u.Email, true
}

// patMintAllowed reports whether the caller's role clears the configured
// minimum (auth.pat_min_role) for managing personal tokens — the feature gate
// while PATs mature. An empty minRole means the config was not normalized and
// fails closed to the admin-only default.
func patMintAllowed(minRole, callerRole string) bool {
	return minRole == config.RoleViewer || callerRole == config.RoleAdmin
}

// patExpiryDays returns the lifetime a token is actually minted with, in days;
// 0 means no expiry. `requested` is the caller's ask (0 = never expires).
//
// A viewer token gets what it asked for. An admin token is clamped to
// maxAdminExpiresInDays — including the "no expiry" case, which is the one that
// matters: leaving 0 alone would let the cap be bypassed by simply omitting
// expires_in_days. The response echoes the resulting expires_at, so a clamped
// request is visible rather than silent.
func patExpiryDays(role string, requested int) int {
	if role != config.RoleAdmin {
		return requested
	}

	if requested == 0 || requested > maxAdminExpiresInDays {
		return maxAdminExpiresInDays
	}

	return requested
}

// patRoleAllowed reports whether a caller with `caller` role may mint a token
// with `requested` role (least-privilege: a viewer cannot mint an admin token).
func patRoleAllowed(caller, requested string) bool {
	if requested != config.RoleViewer && requested != config.RoleAdmin {
		return false
	}

	if caller == config.RoleAdmin {
		return true
	}

	return requested == config.RoleViewer
}

func (s *Handlers) ListPersonalTokens(
	ctx context.Context,
	req serverhttp.ListPersonalTokensRequestObject,
) (serverhttp.ListPersonalTokensResponseObject, error) {
	user := auth.UserFromContext(ctx)
	if user == nil || s.storage == nil {
		return serverhttp.ListPersonalTokens200JSONResponse{}, nil
	}

	subject, ok := patSubject(user)
	if !ok {
		return serverhttp.ListPersonalTokens200JSONResponse{}, nil
	}

	rows, err := s.storage.ListAPITokens(ctx, subject, includeRevoked(req.Params.IncludeRevoked))
	if err != nil {
		return nil, fmt.Errorf("ListPersonalTokens | %w", err)
	}

	out := make(serverhttp.ListPersonalTokens200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.PersonalAccessToken{
			Id:         r.ID,
			Name:       r.Name,
			Prefix:     r.Prefix,
			Role:       serverhttp.PersonalAccessTokenRole(r.Role),
			CreatedAt:  r.CreatedAt,
			LastUsedAt: r.LastUsedAt,
			ExpiresAt:  r.ExpiresAt,
			RevokedAt:  r.RevokedAt,
		})
	}

	return out, nil
}

// includeRevoked reads the optional include_revoked flag, defaulting to false so
// an omitted parameter keeps revoked tokens out of the listing.
func includeRevoked(v *bool) bool {
	return v != nil && *v
}

func (s *Handlers) CreatePersonalToken(
	ctx context.Context,
	req serverhttp.CreatePersonalTokenRequestObject,
) (serverhttp.CreatePersonalTokenResponseObject, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return serverhttp.CreatePersonalToken403Response{}, nil
	}

	// Only an individually-identifiable OIDC principal may mint tokens. This
	// enforces anti-chaining (a leaked PAT carries no email, so it cannot mint
	// more) and blocks shared static config tokens from minting tokens that would
	// survive removal of the static token from the config.
	subject, ok := patSubject(user)
	if !ok {
		return serverhttp.CreatePersonalToken403Response{}, nil
	}

	if !patMintAllowed(s.cfg.Auth.PATMinRole, user.Role) {
		return serverhttp.CreatePersonalToken403Response{}, nil
	}

	if req.Body == nil || req.Body.Name == "" {
		return serverhttp.CreatePersonalToken400Response{}, nil
	}

	role := config.RoleViewer
	if req.Body.Role != nil {
		role = string(*req.Body.Role)
	}

	if !patRoleAllowed(user.Role, role) {
		return serverhttp.CreatePersonalToken403Response{}, nil
	}

	// Bound the lifetime so the day→duration multiplication cannot overflow int64
	// (which would wrap to a past/garbage expiry). 10 years mirrors the swagger max.
	if req.Body.ExpiresInDays != nil && (*req.Body.ExpiresInDays < 0 || *req.Body.ExpiresInDays > maxExpiresInDays) {
		return serverhttp.CreatePersonalToken400Response{}, nil
	}

	if s.storage == nil {
		return nil, fmt.Errorf("CreatePersonalToken | storage is not configured")
	}

	requestedDays := 0
	if req.Body.ExpiresInDays != nil {
		requestedDays = *req.Body.ExpiresInDays
	}

	var expiresAt *time.Time
	if days := patExpiryDays(role, requestedDays); days > 0 {
		t := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &t
	}

	secret, hash, prefix, err := pat.Generate()
	if err != nil {
		return nil, fmt.Errorf("CreatePersonalToken | %w", err)
	}

	id, err := s.storage.CreateAPIToken(ctx, hash, prefix, req.Body.Name, subject, role, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("CreatePersonalToken | %w", err)
	}

	return serverhttp.CreatePersonalToken201JSONResponse{
		Id:        id,
		Name:      req.Body.Name,
		Prefix:    prefix,
		Role:      serverhttp.PersonalAccessTokenCreatedRole(role),
		Token:     secret,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Handlers) RevokePersonalToken(
	ctx context.Context,
	req serverhttp.RevokePersonalTokenRequestObject,
) (serverhttp.RevokePersonalTokenResponseObject, error) {
	user := auth.UserFromContext(ctx)
	if user == nil || s.storage == nil {
		return serverhttp.RevokePersonalToken404Response{}, nil
	}

	subject, hasSubject := patSubject(user)
	if !hasSubject {
		return serverhttp.RevokePersonalToken404Response{}, nil
	}

	ok, err := s.storage.RevokeAPIToken(ctx, subject, req.Id)
	if err != nil {
		return nil, fmt.Errorf("RevokePersonalToken | %w", err)
	}

	if !ok {
		return serverhttp.RevokePersonalToken404Response{}, nil
	}

	return serverhttp.RevokePersonalToken204Response{}, nil
}
