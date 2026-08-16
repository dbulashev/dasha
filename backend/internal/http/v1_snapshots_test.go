package http

import (
	"context"
	"testing"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

// emptyReportRepo answers the only call PostSnapshot gets to make before it
// decides there is nothing worth storing; anything further panics on the nil
// embedded interface, which is the point.
type emptyReportRepo struct {
	repository.Repository
}

func (emptyReportRepo) GetQueriesReport(
	context.Context,
	string, string, string,
	[]string,
	*int64,
) ([]dto.QueryReport, error) {
	return nil, nil
}

func TestPostSnapshotRefusesEmptyReport(t *testing.T) {
	t.Parallel()

	h := &Handlers{repo: emptyReportRepo{}, storage: &storage.Storage{}} //nolint:exhaustruct

	resp, err := h.PostSnapshot(t.Context(), serverhttp.PostSnapshotRequestObject{
		Params: serverhttp.PostSnapshotParams{ //nolint:exhaustruct
			ClusterName: "c1",
			Instance:    "host1",
			Database:    "app",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := resp.(serverhttp.PostSnapshot404Response); !ok {
		t.Fatalf("got %T, want PostSnapshot404Response", resp)
	}
}
