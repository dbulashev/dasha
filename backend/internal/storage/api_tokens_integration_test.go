//go:build integration

package storage

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

var tc *testinfra.TestContainer

func TestMain(m *testing.M) {
	ctx := context.Background()
	tc = testinfra.MustNew(ctx)

	if err := tc.Setup(ctx); err != nil {
		panic("fixture setup failed: " + err.Error())
	}
	testinfra.SetTestContainer(tc)

	code := m.Run()

	tc.TearDown(ctx)
	os.Exit(code)
}

// newTestStorage returns a Storage backed by an isolated DB with the api_tokens
// and users tables created. Same-package access lets the test set the unexported
// pools.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, createAPITokensSQL)
	require.NoError(t, err, "create api_tokens table")
	_, err = pool.Exec(ctx, createAPITokensSubjectIdxSQL)
	require.NoError(t, err, "create api_tokens index")
	_, err = pool.Exec(ctx, createUsersSQL)
	require.NoError(t, err, "create users table")

	return &Storage{pool: pool, ddlPool: pool, logger: zap.NewNop()}
}

func hashOf(s string) []byte {
	h := sha256.Sum256([]byte(s))

	return h[:]
}

func TestAPIToken_CRUDAndOwnership(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	id, err := s.CreateAPIToken(ctx, hashOf("secret-a"), "dasha_pat_aaa", "ci", "alice@corp", "viewer", nil)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// Resolve maps the hash to the owner's identity and role.
	idn, ok, err := s.ResolveAPIToken(ctx, hashOf("secret-a"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "alice@corp", idn.Subject)
	assert.Equal(t, "viewer", idn.Role)

	// A different hash does not resolve.
	_, ok, err = s.ResolveAPIToken(ctx, hashOf("nope"))
	require.NoError(t, err)
	assert.False(t, ok)

	// Listing returns the owner's token; last_used starts nil, then is stamped.
	toks, err := s.ListAPITokens(ctx, "alice@corp", false)
	require.NoError(t, err)
	require.Len(t, toks, 1)
	assert.Equal(t, id, toks[0].ID)
	assert.Nil(t, toks[0].LastUsedAt)

	require.NoError(t, s.TouchAPIToken(ctx, hashOf("secret-a")))
	toks, err = s.ListAPITokens(ctx, "alice@corp", false)
	require.NoError(t, err)
	require.Len(t, toks, 1)
	assert.NotNil(t, toks[0].LastUsedAt)

	// Revoke is ownership-scoped: a different subject cannot revoke.
	ok, err = s.RevokeAPIToken(ctx, "mallory@corp", id)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = s.RevokeAPIToken(ctx, "alice@corp", id)
	require.NoError(t, err)
	assert.True(t, ok)

	// After revoke: no longer resolvable and dropped from the list.
	_, ok, err = s.ResolveAPIToken(ctx, hashOf("secret-a"))
	require.NoError(t, err)
	assert.False(t, ok)

	toks, err = s.ListAPITokens(ctx, "alice@corp", false)
	require.NoError(t, err)
	assert.Empty(t, toks)

	// The revoked row survives as an audit trail and comes back on request,
	// stamped with the time it was revoked.
	toks, err = s.ListAPITokens(ctx, "alice@corp", true)
	require.NoError(t, err)
	require.Len(t, toks, 1)
	assert.Equal(t, id, toks[0].ID)
	assert.NotNil(t, toks[0].RevokedAt)
}

func TestAPIToken_IdleNotResolvedAndSwept(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	id, err := s.CreateAPIToken(ctx, hashOf("secret-idle"), "dasha_pat_idl", "old-ci", "carol@corp", "viewer", nil)
	require.NoError(t, err)

	fresh, err := s.CreateAPIToken(ctx, hashOf("secret-fresh"), "dasha_pat_frs", "ci", "carol@corp", "viewer", nil)
	require.NoError(t, err)
	require.NoError(t, s.TouchAPIToken(ctx, hashOf("secret-fresh")))

	// Backdate last use past the cutoff. The fixture writes the column directly:
	// there is no API to age a token, and the point is the cutoff, not the clock.
	_, err = s.pool.Exec(ctx,
		`UPDATE api_tokens SET last_used_at = now() - make_interval(days => $1) WHERE id::text = $2`,
		IdleRevokeDays+1, id)
	require.NoError(t, err)

	// Idle tokens stop authenticating immediately — before any sweep has run.
	_, ok, err := s.ResolveAPIToken(ctx, hashOf("secret-idle"))
	require.NoError(t, err)
	assert.False(t, ok, "token idle past the cutoff must not resolve")

	_, ok, err = s.ResolveAPIToken(ctx, hashOf("secret-fresh"))
	require.NoError(t, err)
	assert.True(t, ok, "recently used token must still resolve")

	// The sweep then writes the revocation down, leaving the active token alone.
	n, err := s.RevokeIdleAPITokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	toks, err := s.ListAPITokens(ctx, "carol@corp", true)
	require.NoError(t, err)
	require.Len(t, toks, 2)

	byID := map[string]*time.Time{}
	for _, tok := range toks {
		byID[tok.ID] = tok.RevokedAt
	}

	assert.NotNil(t, byID[id], "idle token is revoked")
	assert.Nil(t, byID[fresh], "active token is untouched")

	// Idempotent: a second pass finds nothing left to revoke.
	n, err = s.RevokeIdleAPITokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func TestAPIToken_NeverUsedIdleFromCreation(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	// last_used_at is NULL until first use, so idleness has to fall back to
	// created_at — otherwise a token minted and forgotten would live forever.
	id, err := s.CreateAPIToken(ctx, hashOf("secret-never"), "dasha_pat_nvr", "unused", "dave@corp", "viewer", nil)
	require.NoError(t, err)

	_, err = s.pool.Exec(ctx,
		`UPDATE api_tokens SET created_at = now() - make_interval(days => $1) WHERE id::text = $2`,
		IdleRevokeDays+1, id)
	require.NoError(t, err)

	_, ok, err := s.ResolveAPIToken(ctx, hashOf("secret-never"))
	require.NoError(t, err)
	assert.False(t, ok, "never-used token past the cutoff must not resolve")

	n, err := s.RevokeIdleAPITokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestAPIToken_ExpiredNotResolved(t *testing.T) {
	t.Parallel()

	s := newTestStorage(t)
	ctx := t.Context()

	past := time.Now().Add(-time.Hour)
	_, err := s.CreateAPIToken(ctx, hashOf("secret-exp"), "dasha_pat_exp", "ci", "bob@corp", "admin", &past)
	require.NoError(t, err)

	_, ok, err := s.ResolveAPIToken(ctx, hashOf("secret-exp"))
	require.NoError(t, err)
	assert.False(t, ok, "expired token must not resolve")
}
