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
	statuses map[piicatalog.Key]piicatalog.Status
	catalog  piicatalog.Catalog
	err      error
}

func (f *fakePIIStore) ListPIIStatus(context.Context) (map[piicatalog.Key]piicatalog.Status, error) {
	return f.statuses, f.err
}

func (f *fakePIIStore) PIICatalog(context.Context, piicatalog.Key) (piicatalog.Catalog, error) {
	return f.catalog, f.err
}

func newPIIHandlers(st *fakePIIStore) *Handlers {
	cfg := config.Config{} //nolint:exhaustruct
	cfg.Clusters = []config.Cluster{
		{Name: "c1", Databases: []config.Database{"app"}}, //nolint:exhaustruct
	}

	return &Handlers{ //nolint:exhaustruct
		pii: piicatalog.NewReader(st, config.NewClustersFromConfig(cfg), "0 3 * * *"),
	}
}

func ptr[T any](v T) *T { return &v }

func TestGetPiiCatalogStatus(t *testing.T) {
	t.Parallel()

	attempt := time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC)
	h := newPIIHandlers(&fakePIIStore{ //nolint:exhaustruct
		statuses: map[piicatalog.Key]piicatalog.Status{
			{Cluster: "c1", Database: "app"}: { //nolint:exhaustruct
				SetHash: []byte{0xab, 0x01}, PairCount: 7, LastAttemptAt: &attempt, LastError: ptr("boom"),
			},
			{Cluster: "c0", Database: "gone"}: {PairCount: 0}, //nolint:exhaustruct
		},
	})

	resp, err := h.GetPiiCatalogStatus(t.Context(), serverhttp.GetPiiCatalogStatusRequestObject{
		Params: serverhttp.GetPiiCatalogStatusParams{ClusterName: ptr("c1")},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := resp.(serverhttp.GetPiiCatalogStatus200JSONResponse)
	if got.Schedule != "0 3 * * *" || len(got.Items) != 1 {
		t.Fatalf("got %+v", got)
	}

	it := got.Items[0]
	if it.ClusterName != "c1" || it.Database != "app" || !it.InConfig || it.SetHash == nil || *it.SetHash != "ab01" ||
		it.PairCount != 7 || it.LastAttemptAt != &attempt || it.LastError == nil || *it.LastError != "boom" {
		t.Errorf("item = %+v", it)
	}
}

func TestGetPiiCatalog_StorageMissing(t *testing.T) {
	t.Parallel()

	h := &Handlers{} //nolint:exhaustruct

	req := serverhttp.GetPiiCatalogStatusRequestObject{} //nolint:exhaustruct
	if _, err := h.GetPiiCatalogStatus(t.Context(), req); !errors.Is(err, errPIIStorageMissing) {
		t.Errorf("status: err = %v", err)
	}

	if _, err := h.GetPiiCatalogPairs(t.Context(), pairsReq("app")); !errors.Is(err, errPIIStorageMissing) {
		t.Errorf("pairs: err = %v", err)
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

func TestGetPiiCatalogPairs_ErrorMapping(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("storage down")

	cases := []struct {
		name     string
		req      serverhttp.GetPiiCatalogPairsRequestObject
		err      error
		want400  bool
		want404  serverhttp.PiiCatalogErrorCode
		wantFail error
	}{
		{name: "invalid key", req: serverhttp.GetPiiCatalogPairsRequestObject{
			Params: serverhttp.GetPiiCatalogPairsParams{ClusterName: "c\x00", Database: "app"},
		}, want400: true},
		{name: "not collected", req: pairsReq("app"), err: piicatalog.ErrNoCatalog,
			want404: serverhttp.PiiCatalogErrNotCollectedYet},
		{name: "not found", req: pairsReq("nope"), err: piicatalog.ErrNoCatalog,
			want404: serverhttp.PiiCatalogErrNotFound},
		{name: "storage", req: pairsReq("app"), err: storeErr, wantFail: storeErr},
	}

	for _, tc := range cases {
		h := newPIIHandlers(&fakePIIStore{err: tc.err}) //nolint:exhaustruct

		resp, err := h.GetPiiCatalogPairs(t.Context(), tc.req)

		switch {
		case tc.wantFail != nil:
			if !errors.Is(err, tc.wantFail) {
				t.Errorf("%s: err = %v", tc.name, err)
			}
		case err != nil:
			t.Errorf("%s: err = %v", tc.name, err)
		case tc.want400:
			r, ok := resp.(serverhttp.GetPiiCatalogPairs400JSONResponse)
			if !ok || r.Code != serverhttp.PiiCatalogErrBadRequest {
				t.Errorf("%s: got %T %+v", tc.name, resp, resp)
			}
		default:
			if r, ok := resp.(serverhttp.GetPiiCatalogPairs404JSONResponse); !ok || r.Code != tc.want404 {
				t.Errorf("%s: got %T %+v, want 404 %q", tc.name, resp, resp, tc.want404)
			}
		}
	}
}
