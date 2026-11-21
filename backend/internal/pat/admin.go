package pat

import (
	"context"
	"time"

	"github.com/dbulashev/dasha/internal/config"
)

// AdminToken is the owner's view of a token plus the owning subject.
type AdminToken struct {
	Token

	Subject string
}

// User is a principal that has signed in at least once. Tokens counts its
// non-revoked tokens, the same set the token lists return.
type User struct {
	Subject     string
	Name        string
	Role        string
	CreatedAt   time.Time
	LastLoginAt *time.Time
	Tokens      int
}

type AdminStore interface {
	ListAllAPITokens(ctx context.Context, includeRevoked bool) ([]AdminToken, error)
	// RevokeAPITokenByID reports false when no active token has this id.
	RevokeAPITokenByID(ctx context.Context, id string) (bool, error)
	ListUsers(ctx context.Context) ([]User, error)
}

// Admin manages every principal's tokens. Without storage it refuses all
// requests with ErrForbidden, as the handlers did before it.
type Admin struct {
	store AdminStore
}

func NewAdmin(store AdminStore) *Admin {
	return &Admin{store: store}
}

func (a *Admin) Tokens(ctx context.Context, c Caller, includeRevoked bool) ([]AdminToken, error) {
	if !a.allowed(c) {
		return nil, ErrForbidden
	}

	return a.store.ListAllAPITokens(ctx, includeRevoked)
}

// Revoke reports false when no active token has this id.
func (a *Admin) Revoke(ctx context.Context, c Caller, id string) (bool, error) {
	if !a.allowed(c) {
		return false, ErrForbidden
	}

	return a.store.RevokeAPITokenByID(ctx, id)
}

func (a *Admin) Users(ctx context.Context, c Caller) ([]User, error) {
	if !a.allowed(c) {
		return nil, ErrForbidden
	}

	return a.store.ListUsers(ctx)
}

// allowed requires an interactive OIDC admin session, the same bar as for
// minting. A PAT carrying the admin role is refused: a leaked token must not
// enumerate or revoke the tokens that would replace it.
func (a *Admin) allowed(c Caller) bool {
	return a.store != nil && c.OIDC && c.Role == config.RoleAdmin
}
