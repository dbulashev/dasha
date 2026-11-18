package piicatalog

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/internal/config"
)

type fakeReadStore struct {
	statuses  map[Key]Status
	statusErr error
	catalog   Catalog
	err       error
}

func (f *fakeReadStore) ListPIIStatus(context.Context) (map[Key]Status, error) {
	return f.statuses, f.statusErr
}

func (f *fakeReadStore) PIICatalog(context.Context, Key) (Catalog, error) {
	return f.catalog, f.err
}

type failingClusters struct{ config.Clusters }

func (failingClusters) Get(context.Context) ([]config.Cluster, error) {
	return nil, errors.New("discovery unavailable")
}

func newTestReader(st ReadStore) *Reader {
	cfg := config.Config{} //nolint:exhaustruct
	cfg.Clusters = []config.Cluster{
		{Name: "c1", Databases: []config.Database{"app", "new"}}, //nolint:exhaustruct
	}

	return NewReader(st, config.NewClustersFromConfig(cfg), "0 3 * * *")
}

func TestReader_StatusesUnionOfConfigAndStorage(t *testing.T) {
	t.Parallel()

	r := newTestReader(&fakeReadStore{ //nolint:exhaustruct
		statuses: map[Key]Status{
			{Cluster: "c1", Database: "app"}:  {SetHash: []byte{0xab}, PairCount: 7}, //nolint:exhaustruct
			{Cluster: "c0", Database: "gone"}: {SetHash: []byte{0x01}, PairCount: 2}, //nolint:exhaustruct
		},
	})

	got, err := r.Statuses(t.Context(), "")
	require.NoError(t, err)

	want := []struct {
		key       Key
		inConfig  bool
		collected bool
	}{
		{Key{"c0", "gone"}, false, true},
		{Key{"c1", "app"}, true, true},
		{Key{"c1", "new"}, true, false},
	}

	require.Len(t, got, len(want))

	for i, w := range want {
		assert.Equal(t, w.key, got[i].Key, "item %d", i)
		assert.Equal(t, w.inConfig, got[i].InConfig, "item %d", i)
		assert.Equal(t, w.collected, got[i].Status.SetHash != nil, "item %d", i)
	}

	got, err = r.Statuses(t.Context(), "c0")
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestReader_StatusesErrors(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("storage down")

	_, err := newTestReader(&fakeReadStore{statusErr: storeErr}).Statuses(t.Context(), "") //nolint:exhaustruct
	require.ErrorIs(t, err, storeErr)

	r := NewReader(&fakeReadStore{}, failingClusters{}, "") //nolint:exhaustruct
	_, err = r.Statuses(t.Context(), "")
	require.Error(t, err)
}

func TestReader_CatalogNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		db      string
		wantErr error
	}{
		{"app", ErrNotCollected},
		{"new", ErrNotCollected},
		{"nope", ErrNotFound},
	}

	for _, tc := range cases {
		r := newTestReader(&fakeReadStore{err: ErrNoCatalog}) //nolint:exhaustruct

		_, err := r.Catalog(t.Context(), Key{Cluster: "c1", Database: tc.db})
		assert.ErrorIs(t, err, tc.wantErr, tc.db)
	}
}

func TestReader_CatalogPassesThrough(t *testing.T) {
	t.Parallel()

	cat := Catalog{SetHash: []byte{0xab}}                                                          //nolint:exhaustruct
	got, err := newTestReader(&fakeReadStore{catalog: cat}).Catalog(t.Context(), Key{"c1", "app"}) //nolint:exhaustruct
	require.NoError(t, err)
	assert.Equal(t, cat, got)

	storeErr := errors.New("storage down")
	_, err = newTestReader(&fakeReadStore{err: storeErr}).Catalog(t.Context(), Key{"c1", "app"}) //nolint:exhaustruct
	require.ErrorIs(t, err, storeErr)
}

func TestReader_CatalogRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	r := newTestReader(&fakeReadStore{}) //nolint:exhaustruct

	for _, k := range []Key{{"c\x00", "app"}, {"c1", "\xff"}} {
		_, err := r.Catalog(t.Context(), k)
		assert.ErrorIs(t, err, ErrInvalidKey, "%q", k)
	}
}
