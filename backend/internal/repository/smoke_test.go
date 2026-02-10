//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

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

// TestSmoke_IsolatePool verifies that the test infrastructure works:
// container starts, fixture is applied, IsolatePool creates an isolated DB.
func TestSmoke_IsolatePool(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	ctx := t.Context()

	// Verify fixture data exists
	var count int
	err := pool.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 20000, count, "orders table should have 20000 rows")

	var usersCount int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&usersCount)
	require.NoError(t, err)
	assert.Equal(t, 1000, usersCount, "users table should have 1000 rows")
}

// TestSmoke_ServerVersion verifies that getServerVersionNum works with the test pool.
func TestSmoke_ServerVersion(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	ctx := t.Context()

	p := NewTestPgxPool(pool, zap.NewNop())
	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)
	assert.Greater(t, vNum, 100000, "version should be at least 10.0")
}
