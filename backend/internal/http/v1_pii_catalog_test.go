package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/piicatalog"
)

type fakePIIStore struct {
	statuses  map[piicatalog.Key]piicatalog.Status
	statusErr error
	catalog   piicatalog.Catalog
	err       error
}

func (f *fakePIIStore) ListPIIStatus(context.Context) (map[piicatalog.Key]piicatalog.Status, error) {
	return f.statuses, f.statusErr
}

type failingClusters struct{ config.Clusters }

func (failingClusters) Get(context.Context) ([]config.Cluster, error) {
	return nil, errors.New("discovery unavailable")
}

func (f *fakePIIStore) PIICatalog(context.Context, piicatalog.Key) (piicatalog.Catalog, error) {
	return f.catalog, f.err
}

func newPIIHandlers(st *fakePIIStore) *Handlers {
	cfg := &config.Config{}                                                            //nolint:exhaustruct
	cfg.PIICatalog = config.PIICatalogConfig{Schedule: "0 3 * * *", RetentionDays: 90} //nolint:exhaustruct
	cfg.Clusters = []config.Cluster{
		{Name: "c1", Databases: []config.Database{"app", "new"}}, //nolint:exhaustruct
	}

	return &Handlers{ //nolint:exhaustruct
		cfg:      cfg,
		clusters: config.NewClustersFromConfig(*cfg),
		pii:      st,
	}
}

func ptr[T any](v T) *T { return &v }

func TestGetPiiCatalogStatus_UnionOfConfigAndStorage(t *testing.T) {
	t.Parallel()

	h := newPIIHandlers(&fakePIIStore{ //nolint:exhaustruct
		statuses: map[piicatalog.Key]piicatalog.Status{
			{Cluster: "c1", Database: "app"}:  {SetHash: []byte{0xab}, PairCount: 7}, //nolint:exhaustruct
			{Cluster: "c0", Database: "gone"}: {SetHash: []byte{0x01}, PairCount: 2}, //nolint:exhaustruct
		},
	})

	resp, err := h.GetPiiCatalogStatus(t.Context(), serverhttp.GetPiiCatalogStatusRequestObject{}) //nolint:exhaustruct
	if err != nil {
		t.Fatal(err)
	}

	got := resp.(serverhttp.GetPiiCatalogStatus200JSONResponse)

	want := []struct {
		cluster, db string
		inConfig    bool
		collected   bool
	}{
		{"c0", "gone", false, true},
		{"c1", "app", true, true},
		{"c1", "new", true, false},
	}

	if len(got.Items) != len(want) {
		t.Fatalf("items = %+v", got.Items)
	}

	for i, w := range want {
		it := got.Items[i]
		if it.ClusterName != w.cluster || it.Database != w.db || it.InConfig != w.inConfig ||
			(it.SetHash != nil) != w.collected {
			t.Errorf("item %d = %+v, want %+v", i, it, w)
		}
	}

	if got.Schedule != "0 3 * * *" {
		t.Errorf("schedule = %q", got.Schedule)
	}

	resp, err = h.GetPiiCatalogStatus(t.Context(), serverhttp.GetPiiCatalogStatusRequestObject{
		Params: serverhttp.GetPiiCatalogStatusParams{ClusterName: ptr("c0")},
	})
	if err != nil {
		t.Fatal(err)
	}

	if items := resp.(serverhttp.GetPiiCatalogStatus200JSONResponse).Items; len(items) != 1 {
		t.Errorf("cluster filter: %+v", items)
	}
}

func TestGetPiiCatalogStatus_Errors(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("storage down")

	req := serverhttp.GetPiiCatalogStatusRequestObject{} //nolint:exhaustruct

	h := newPIIHandlers(&fakePIIStore{statusErr: storeErr}) //nolint:exhaustruct
	if _, err := h.GetPiiCatalogStatus(t.Context(), req); !errors.Is(err, storeErr) {
		t.Errorf("storage failure: err = %v", err)
	}

	h = newPIIHandlers(&fakePIIStore{}) //nolint:exhaustruct
	h.clusters = failingClusters{}

	if _, err := h.GetPiiCatalogStatus(t.Context(), req); err == nil {
		t.Error("clusters failure: want an error")
	}
}

func pairsReq(db string) serverhttp.GetPiiCatalogPairsRequestObject {
	return serverhttp.GetPiiCatalogPairsRequestObject{Params: serverhttp.GetPiiCatalogPairsParams{
		ClusterName: "c1", Database: db,
	}}
}

func TestGetPiiCatalogPairs(t *testing.T) {
	t.Parallel()

	changed := time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC)
	h := newPIIHandlers(&fakePIIStore{catalog: piicatalog.Catalog{ //nolint:exhaustruct
		SetHash: []byte{0xab, 0x01}, ChangedAt: changed, CheckedAt: changed.Add(time.Hour),
		Pairs: []piicatalog.Pair{{Name: "a"}, {Name: "b", Comment: ptr("")}},
	}})

	resp, err := h.GetPiiCatalogPairs(t.Context(), pairsReq("app"))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := resp.(serverhttp.GetPiiCatalogPairs200JSONResponse)
	if !ok || got.SetHash != "ab01" || !got.ChangedAt.Equal(changed) || len(got.Items) != 2 {
		t.Fatalf("got %T %+v", resp, resp)
	}

	if got.Items[0].ColumnComment != nil || got.Items[1].ColumnComment == nil {
		t.Errorf("NULL vs empty comment lost: %+v", got.Items)
	}
}

func TestGetPiiCatalogPairs_NotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		db       string
		err      error
		wantCode serverhttp.PiiCatalogErrorCode
	}{
		{"app", piicatalog.ErrNotCollected, serverhttp.PiiCatalogErrNotCollectedYet},
		{"new", piicatalog.ErrNotFound, serverhttp.PiiCatalogErrNotCollectedYet},
		{"nope", piicatalog.ErrNotFound, serverhttp.PiiCatalogErrNotFound},
		{"nope", piicatalog.ErrNotCollected, serverhttp.PiiCatalogErrNotFound},
	}

	for _, tc := range cases {
		h := newPIIHandlers(&fakePIIStore{err: tc.err}) //nolint:exhaustruct

		resp, err := h.GetPiiCatalogPairs(t.Context(), pairsReq(tc.db))
		if err != nil {
			t.Fatal(err)
		}

		r, ok := resp.(serverhttp.GetPiiCatalogPairs404JSONResponse)
		if !ok || r.Code != tc.wantCode {
			t.Errorf("%s/%v: got %T %+v, want 404 %q", tc.db, tc.err, resp, resp, tc.wantCode)
		}
	}
}

func TestGetPiiCatalog_KeyRejectsNUL(t *testing.T) {
	t.Parallel()

	h := newPIIHandlers(&fakePIIStore{}) //nolint:exhaustruct

	resp, err := h.GetPiiCatalogPairs(t.Context(), serverhttp.GetPiiCatalogPairsRequestObject{
		Params: serverhttp.GetPiiCatalogPairsParams{ClusterName: "c\x00", Database: "app"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := resp.(serverhttp.GetPiiCatalogPairs400JSONResponse); !ok {
		t.Fatalf("got %T", resp)
	}
}
