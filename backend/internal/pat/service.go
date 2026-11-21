// Package pat holds the policy for personal access tokens: who may mint them,
// with which role and for how long. Secret generation lives in internal/pkg/pat.
package pat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	patsecret "github.com/dbulashev/dasha/internal/pkg/pat"
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

// Token is one personal access token as shown to its owner (never the secret).
// RevokedAt is non-nil only when revoked tokens were asked for.
type Token struct {
	ID         string
	Name       string
	Prefix     string
	Role       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

type Store interface {
	CreateAPIToken(ctx context.Context, hash []byte, prefix, name, subject, role string, expiresAt *time.Time) (string, error)
	ListAPITokens(ctx context.Context, subject string, includeRevoked bool) ([]Token, error)
	RevokeAPIToken(ctx context.Context, subject, id string) (bool, error)
	// APITokensReady reports whether the token table has been migrated.
	APITokensReady(ctx context.Context) bool
}

// Caller is what the policy needs to know about whoever is asking. The handler
// builds it from auth.UserContext, so the domain does not depend on auth.
type Caller struct {
	OIDC  bool
	Email string
	Role  string
}

var (
	ErrForbidden = errors.New("pat: caller may not manage tokens")
	ErrInvalid   = errors.New("pat: invalid token request")
)

type MintRequest struct {
	Name          string
	Role          string // empty: viewer
	ExpiresInDays int    // 0: never expires
}

type Minted struct {
	Token

	Secret string
}

type Service struct {
	store   Store
	minRole string
	now     func() time.Time
}

// NewService takes minRole from auth.pat_min_role. store may be nil when no
// storage is configured: nothing is listed or revoked, and minting fails.
func NewService(store Store, minRole string) *Service {
	return &Service{store: store, minRole: minRole, now: time.Now}
}

// Ready reports whether tokens can be used at all: storage is configured and
// its token table exists.
func (s *Service) Ready(ctx context.Context) bool {
	return s.store != nil && s.store.APITokensReady(ctx)
}

func (s *Service) Mint(ctx context.Context, c Caller, req MintRequest) (Minted, error) {
	// Only an individually-identifiable OIDC principal may mint tokens. This
	// enforces anti-chaining (a leaked PAT carries no email, so it cannot mint
	// more) and blocks shared static config tokens from minting tokens that would
	// survive removal of the static token from the config.
	subject, ok := c.subject()
	if !ok || !mintAllowed(s.minRole, c.Role) {
		return Minted{}, ErrForbidden
	}

	if req.Name == "" {
		return Minted{}, fmt.Errorf("%w: empty name", ErrInvalid)
	}

	role := req.Role
	if role == "" {
		role = config.RoleViewer
	}

	if !roleAllowed(c.Role, role) {
		return Minted{}, ErrForbidden
	}

	if req.ExpiresInDays < 0 || req.ExpiresInDays > maxExpiresInDays {
		return Minted{}, fmt.Errorf("%w: expires_in_days out of range", ErrInvalid)
	}

	if s.store == nil {
		return Minted{}, errors.New("pat: storage is not configured")
	}

	now := s.now().UTC()

	var expiresAt *time.Time
	if days := expiryDays(role, req.ExpiresInDays); days > 0 {
		t := now.Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &t
	}

	secret, hash, prefix, err := patsecret.Generate()
	if err != nil {
		return Minted{}, err
	}

	id, err := s.store.CreateAPIToken(ctx, hash, prefix, req.Name, subject, role, expiresAt)
	if err != nil {
		return Minted{}, err
	}

	return Minted{
		Token: Token{
			ID:        id,
			Name:      req.Name,
			Prefix:    prefix,
			Role:      role,
			CreatedAt: now,
			ExpiresAt: expiresAt,
		},
		Secret: secret,
	}, nil
}

// List returns nothing, without an error, to a caller that cannot own tokens.
func (s *Service) List(ctx context.Context, c Caller, includeRevoked bool) ([]Token, error) {
	subject, ok := c.subject()
	if !ok || s.store == nil {
		return nil, nil
	}

	return s.store.ListAPITokens(ctx, subject, includeRevoked)
}

// Revoke reports false when the caller has no live token with this id,
// including when it cannot own tokens at all.
func (s *Service) Revoke(ctx context.Context, c Caller, id string) (bool, error) {
	subject, ok := c.subject()
	if !ok || s.store == nil {
		return false, nil
	}

	return s.store.RevokeAPIToken(ctx, subject, id)
}

// subject is the stable owner key for a user's personal access tokens: the
// OIDC email. Personal tokens belong to an individually-identifiable principal,
// so only an OIDC session with a non-empty email qualifies; ok=false for shared
// static config tokens (which carry no unique per-user identity and would let a
// leaked shared token mint tokens that outlive it) and for PAT-authenticated
// callers (anti-chaining). Returning ok=false — rather than falling back to the
// non-unique, possibly-empty name — prevents distinct identities from colliding
// on one token namespace.
func (c Caller) subject() (string, bool) {
	if !c.OIDC || c.Email == "" {
		return "", false
	}

	return c.Email, true
}

// mintAllowed reports whether the caller's role clears the configured
// minimum (auth.pat_min_role) for managing personal tokens — the feature gate
// while PATs mature. An empty minRole means the config was not normalized and
// fails closed to the admin-only default.
func mintAllowed(minRole, callerRole string) bool {
	return minRole == config.RoleViewer || callerRole == config.RoleAdmin
}

// expiryDays returns the lifetime a token is actually minted with, in days;
// 0 means no expiry. `requested` is the caller's ask (0 = never expires).
//
// A viewer token gets what it asked for. An admin token is clamped to
// maxAdminExpiresInDays — including the "no expiry" case, which is the one that
// matters: leaving 0 alone would let the cap be bypassed by simply omitting
// expires_in_days. The response echoes the resulting expires_at, so a clamped
// request is visible rather than silent.
func expiryDays(role string, requested int) int {
	if role != config.RoleAdmin {
		return requested
	}

	if requested == 0 || requested > maxAdminExpiresInDays {
		return maxAdminExpiresInDays
	}

	return requested
}

// roleAllowed reports whether a caller with `caller` role may mint a token
// with `requested` role (least-privilege: a viewer cannot mint an admin token).
func roleAllowed(caller, requested string) bool {
	if requested != config.RoleViewer && requested != config.RoleAdmin {
		return false
	}

	if caller == config.RoleAdmin {
		return true
	}

	return requested == config.RoleViewer
}
