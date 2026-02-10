//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetInvalidConstraints(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getInvalidConstraints(ctx, vNum, pool)
	require.NoError(t, err)

	// Fixture creates NOT VALID FK on deadrows_test.ref_user_id → users.id
	assert.NotEmpty(t, result, "should detect NOT VALID FK constraint")

	found := false
	for _, c := range result {
		if c.Table == "deadrows_test" && c.Name == "fk_deadrows_user" {
			found = true
			assert.Equal(t, "public", c.Schema)
			assert.Equal(t, "users", c.ReferencedTable)
			break
		}
	}
	assert.True(t, found, "should find fk_deadrows_user NOT VALID constraint")
}
