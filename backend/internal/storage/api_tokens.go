package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// APIToken is one personal access token as shown to its owner (never the secret).
// RevokedAt is nil for a live token; it is only ever non-nil when the caller asked
// for revoked tokens to be included.
type APIToken struct {
	ID         string
	Prefix     string
	Name       string
	Role       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

// AdminAPIToken is a token as shown to an administrator: the owner's view plus
// the owning subject, which a user never needs (their tokens are all their own).
type AdminAPIToken struct {
	APIToken

	Subject string
}

// APITokenIdentity is the (subject, role) a presented token resolves to, plus
// its expiry (nil = never) so the caller can bound how long it caches the result.
type APITokenIdentity struct {
	Subject   string
	Role      string
	ExpiresAt *time.Time
}

// CreateAPIToken inserts a token (storing only its hash) and returns the new id.
func (s *Storage) CreateAPIToken(
	ctx context.Context,
	hash []byte,
	prefix, name, subject, role string,
	expiresAt *time.Time,
) (string, error) {
	var id string

	err := s.pool.QueryRow(ctx, `
		INSERT INTO api_tokens (token_hash, token_prefix, name, subject, role, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text`,
		hash, prefix, name, subject, role, expiresAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("storage: create api token: %w", err)
	}

	return id, nil
}

// IdleRevokeDays is how long a token may go unused before it is revoked: a
// credential nobody has used for three months is far more likely forgotten than
// needed, and a forgotten credential is exactly the one that leaks unnoticed.
// Idleness is measured from last use, falling back to creation for a token that
// was never used at all.
const IdleRevokeDays = 90

// ResolveAPIToken returns the identity for an active token matching the given
// hash: not revoked, not expired, and not idle past IdleRevokeDays. ok=false when
// no such token exists.
//
// The idle check is enforced here as well as by RevokeIdleAPITokens, rather than
// relying on the sweep alone: the sweep runs periodically, so between two passes
// an idle token would still authenticate. Checking at resolve time makes the
// cutoff take effect the moment it is crossed.
func (s *Storage) ResolveAPIToken(ctx context.Context, hash []byte) (*APITokenIdentity, bool, error) {
	var idn APITokenIdentity

	err := s.pool.QueryRow(ctx, `
		SELECT subject, role, expires_at
		FROM api_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		  AND coalesce(last_used_at, created_at) > now() - make_interval(days => $2)`,
		hash, IdleRevokeDays,
	).Scan(&idn.Subject, &idn.Role, &idn.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("storage: resolve api token: %w", err)
	}

	return &idn, true, nil
}

// TouchAPIToken records the last-used time. Best-effort: errors are ignored by
// the caller since auth must not fail on an audit-stamp write.
func (s *Storage) TouchAPIToken(ctx context.Context, hash []byte) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1`, hash)
	if err != nil {
		return fmt.Errorf("storage: touch api token: %w", err)
	}

	return nil
}

// ListAPITokens returns the owner's tokens, newest first. Revoked tokens are
// excluded unless includeRevoked is set — revoked rows are kept in the table as
// an audit trail, so they can be shown on request without ever being usable.
func (s *Storage) ListAPITokens(ctx context.Context, subject string, includeRevoked bool) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, token_prefix, name, role, created_at, last_used_at, expires_at, revoked_at
		FROM api_tokens
		WHERE subject = $1 AND ($2 OR revoked_at IS NULL)
		ORDER BY created_at DESC`,
		subject, includeRevoked,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: list api tokens: %w", err)
	}
	defer rows.Close()

	out := make([]APIToken, 0, 8)

	for rows.Next() {
		var t APIToken
		if err := rows.Scan(
			&t.ID, &t.Prefix, &t.Name, &t.Role,
			&t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("storage: scan api token: %w", err)
		}

		out = append(out, t)
	}

	return out, rows.Err()
}

// RevokeAPIToken marks the owner's token revoked. Ownership is enforced by the
// subject filter; found=false when no matching active token exists.
func (s *Storage) RevokeAPIToken(ctx context.Context, subject, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		WHERE id::text = $1 AND subject = $2 AND revoked_at IS NULL`,
		id, subject,
	)
	if err != nil {
		return false, fmt.Errorf("storage: revoke api token: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// ListAllAPITokens returns every owner's tokens, newest first, for the
// administration view. Revoked tokens are excluded unless includeRevoked is set.
// Callers must gate this on an admin role.
func (s *Storage) ListAllAPITokens(ctx context.Context, includeRevoked bool) ([]AdminAPIToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, token_prefix, name, role, created_at, last_used_at, expires_at, revoked_at, subject
		FROM api_tokens
		WHERE $1 OR revoked_at IS NULL
		ORDER BY created_at DESC`,
		includeRevoked,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: list all api tokens: %w", err)
	}
	defer rows.Close()

	out := make([]AdminAPIToken, 0, 16)

	for rows.Next() {
		var t AdminAPIToken
		if err := rows.Scan(
			&t.ID, &t.Prefix, &t.Name, &t.Role,
			&t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt, &t.Subject,
		); err != nil {
			return nil, fmt.Errorf("storage: scan admin api token: %w", err)
		}

		out = append(out, t)
	}

	return out, rows.Err()
}

// RevokeIdleAPITokens revokes every token unused for longer than IdleRevokeDays
// and returns how many were revoked. Idle tokens already fail to resolve (see
// ResolveAPIToken); this writes the revocation down so the state is visible in
// the token lists and carries a revoked_at for the audit trail, instead of a row
// that looks live but silently no longer works.
//
// Safe to run concurrently from several replicas: the revoked_at IS NULL guard
// makes it idempotent, so at worst two passes race and one updates nothing.
func (s *Storage) RevokeIdleAPITokens(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		WHERE revoked_at IS NULL
		  AND coalesce(last_used_at, created_at) <= now() - make_interval(days => $1)`,
		IdleRevokeDays,
	)
	if err != nil {
		return 0, fmt.Errorf("storage: revoke idle api tokens: %w", err)
	}

	return tag.RowsAffected(), nil
}

// RevokeAPITokenByID revokes a token regardless of owner. Unlike RevokeAPIToken
// there is no subject filter, so callers must gate this on an admin role.
// found=false when no matching active token exists.
func (s *Storage) RevokeAPITokenByID(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		WHERE id::text = $1 AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return false, fmt.Errorf("storage: revoke api token by id: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}
