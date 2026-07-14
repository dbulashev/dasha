//go:build integration

package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsers_RecordLoginUpsertsAndStampsLastLogin(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	require.NoError(t, s.RecordLogin(ctx, "alice@corp", "Alice", "viewer"))

	users, err := s.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "alice@corp", users[0].Subject)
	assert.Equal(t, "Alice", users[0].Name)
	assert.Equal(t, "viewer", users[0].Role)
	assert.Equal(t, 0, users[0].Tokens)
	require.NotNil(t, users[0].LastLoginAt)

	first := *users[0].LastLoginAt
	created := users[0].CreatedAt

	// A second login updates the identity from the IdP and re-stamps last_login_at,
	// without creating a second row or moving created_at.
	require.NoError(t, s.RecordLogin(ctx, "alice@corp", "Alice Smith", "admin"))

	users, err = s.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1, "login must upsert, not insert a duplicate")
	assert.Equal(t, "Alice Smith", users[0].Name)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, created, users[0].CreatedAt, "created_at must not move on re-login")
	require.NotNil(t, users[0].LastLoginAt)
	assert.False(t, users[0].LastLoginAt.Before(first), "last_login_at must not go backwards")
}

func TestUsers_TokenCountCountsOnlyNonRevoked(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	require.NoError(t, s.RecordLogin(ctx, "bob@corp", "Bob", "admin"))

	id, err := s.CreateAPIToken(ctx, hashOf("s1"), "dasha_pat_s1", "ci", "bob@corp", "viewer", nil)
	require.NoError(t, err)
	_, err = s.CreateAPIToken(ctx, hashOf("s2"), "dasha_pat_s2", "mcp", "bob@corp", "viewer", nil)
	require.NoError(t, err)

	users, err := s.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, 2, users[0].Tokens)

	// The count tracks the same set the token lists return, so a revoked token drops out.
	ok, err := s.RevokeAPIToken(ctx, "bob@corp", id)
	require.NoError(t, err)
	require.True(t, ok)

	users, err = s.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, 1, users[0].Tokens)
}

func TestAdminTokens_ListAllAndRevokeAnyOwner(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	aliceID, err := s.CreateAPIToken(ctx, hashOf("a"), "dasha_pat_a", "ci", "alice@corp", "viewer", nil)
	require.NoError(t, err)
	_, err = s.CreateAPIToken(ctx, hashOf("b"), "dasha_pat_b", "mcp", "bob@corp", "admin", nil)
	require.NoError(t, err)

	all, err := s.ListAllAPITokens(ctx, false)
	require.NoError(t, err)
	require.Len(t, all, 2, "admin list spans every owner")

	owners := []string{all[0].Subject, all[1].Subject}
	assert.ElementsMatch(t, []string{"alice@corp", "bob@corp"}, owners)

	// An admin revokes another owner's token — no subject filter applies.
	ok, err := s.RevokeAPITokenByID(ctx, aliceID)
	require.NoError(t, err)
	assert.True(t, ok)

	_, resolved, err := s.ResolveAPIToken(ctx, hashOf("a"))
	require.NoError(t, err)
	assert.False(t, resolved, "revoked token must stop authenticating")

	all, err = s.ListAllAPITokens(ctx, false)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "bob@corp", all[0].Subject)

	// With include_revoked the audit row reappears, but it still cannot authenticate.
	all, err = s.ListAllAPITokens(ctx, true)
	require.NoError(t, err)
	require.Len(t, all, 2)

	revoked := 0
	for _, tok := range all {
		if tok.RevokedAt != nil {
			revoked++
		}
	}
	assert.Equal(t, 1, revoked, "exactly the revoked token carries revoked_at")

	// Revoking again is a no-op: the row is already revoked.
	ok, err = s.RevokeAPITokenByID(ctx, aliceID)
	require.NoError(t, err)
	assert.False(t, ok)
}
