//go:build integration

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

func TestGetFksPossibleNulls(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getFksPossibleNulls(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should detect nullable FK column (products.category_id)")

	// Fixture: products.category_id is nullable FK → categories(id)
	found := false
	for _, fk := range result {
		if fk.RelName == "products" {
			found = true
			assert.NotEmpty(t, fk.FkName)
			assert.NotEmpty(t, fk.AttNames)
			break
		}
	}
	assert.True(t, found, "should find products table with nullable FK")
}

func TestGetFksPossibleSimilar(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getFksPossibleSimilar(ctx, vNum, pool)
	require.NoError(t, err)
	// May or may not detect similar FKs depending on SQL logic
	// Just verify SQL executes and mapping works
	for _, fk := range result {
		assert.NotEmpty(t, fk.Table)
		assert.NotEmpty(t, fk.FkName)
		assert.NotEmpty(t, fk.Fk1Name)
	}
}

func TestGetFkTypeMismatch(t *testing.T) {
	t.Parallel()
	pool := testinfra.IsolatePool(t)
	p := NewTestPgxPool(pool, zap.NewNop())
	ctx := t.Context()

	vNum, err := p.getServerVersionNum(ctx, pool)
	require.NoError(t, err)

	result, err := p.getFkTypeMismatch(ctx, vNum, pool)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should detect type mismatch: products.category_id(int) → categories.id(bigint)")

	// Fixture: products.category_id is integer, categories.id is bigint
	found := false
	for _, fk := range result {
		if fk.FromRel == "products" && fk.ToRel == "categories" {
			found = true
			assert.NotEmpty(t, fk.FkName)
			break
		}
	}
	assert.True(t, found, "should find products→categories type mismatch FK")
}
