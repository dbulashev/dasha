package querystats

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIndex map[uuid.UUID][]byte

func (fakeIndex) ListSnapshots(context.Context, string, string) ([]SnapshotListItem, error) {
	return nil, nil
}

func (f fakeIndex) GetSnapshotLocks(_ context.Context, id uuid.UUID) ([]byte, bool, error) {
	raw, ok := f[id]

	return raw, ok, nil
}

func TestSnapshotCatalogLocks(t *testing.T) {
	t.Parallel()

	withLocks := uuid.New()
	c := NewSnapshotCatalog(fakeIndex{withLocks: []byte(`{}`)})

	raw, err := c.Locks(t.Context(), withLocks)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))

	_, err = c.Locks(t.Context(), uuid.New())
	require.ErrorIs(t, err, ErrNoLocks)
}
