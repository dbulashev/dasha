package storage

import (
	"context"
	"fmt"
	"time"
)

// User is one principal that has signed in at least once, with a count of the
// personal access tokens it currently owns.
type User struct {
	Subject     string
	Name        string
	Role        string
	CreatedAt   time.Time
	LastLoginAt *time.Time
	Tokens      int
}

// RecordLogin upserts the principal and stamps last_login_at. Name and role are
// refreshed from the identity provider on every login, so the row tracks the IdP
// instead of drifting from it — the stored role is an audit trail, never the
// source of truth for authorization.
func (s *Storage) RecordLogin(ctx context.Context, subject, name, role string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (subject, name, role, last_login_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (subject) DO UPDATE
		SET name          = EXCLUDED.name,
		    role          = EXCLUDED.role,
		    last_login_at = now()`,
		subject, name, role,
	)
	if err != nil {
		return fmt.Errorf("storage: record login: %w", err)
	}

	return nil
}

// ListUsers returns every principal that has signed in, most recent login first.
// Tokens counts non-revoked tokens — the same set the token lists return — so the
// number always matches the rows an administrator sees for that owner.
func (s *Storage) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.subject, u.name, u.role, u.created_at, u.last_login_at,
		       count(t.subject) AS tokens
		FROM users u
		LEFT JOIN api_tokens t
		       ON t.subject = u.subject
		      AND t.revoked_at IS NULL
		GROUP BY u.subject, u.name, u.role, u.created_at, u.last_login_at
		ORDER BY u.last_login_at DESC NULLS LAST`)
	if err != nil {
		return nil, fmt.Errorf("storage: list users: %w", err)
	}
	defer rows.Close()

	out := make([]User, 0, 8)

	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Subject, &u.Name, &u.Role, &u.CreatedAt, &u.LastLoginAt, &u.Tokens); err != nil {
			return nil, fmt.Errorf("storage: scan user: %w", err)
		}

		out = append(out, u)
	}

	return out, rows.Err()
}
