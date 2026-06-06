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
// table created. Same-package access lets the test set the unexported pools.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	pool := testinfra.IsolatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, createAPITokensSQL)
	require.NoError(t, err, "create api_tokens table")
	_, err = pool.Exec(ctx, createAPITokensSubjectIdxSQL)
	require.NoError(t, err, "create api_tokens index")

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
	toks, err := s.ListAPITokens(ctx, "alice@corp")
	require.NoError(t, err)
	require.Len(t, toks, 1)
	assert.Equal(t, id, toks[0].ID)
	assert.Nil(t, toks[0].LastUsedAt)

	require.NoError(t, s.TouchAPIToken(ctx, hashOf("secret-a")))
	toks, err = s.ListAPITokens(ctx, "alice@corp")
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

	toks, err = s.ListAPITokens(ctx, "alice@corp")
	require.NoError(t, err)
	assert.Empty(t, toks)
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
