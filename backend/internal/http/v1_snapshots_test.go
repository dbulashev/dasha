package http

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/repository"
)

// snapshotRepo scripts the report and provenance lookups; anything further
// panics on the nil embedded interface, which is the point.
type snapshotRepo struct {
	repository.Repository
	reports   []dto.QueryReport
	pgssDBErr error
}

func (f snapshotRepo) GetQueriesReport(context.Context, string, string, string, []string, *int64) ([]dto.QueryReport, error) {
	return f.reports, nil
}

func (f snapshotRepo) PgssDatabase(context.Context, string, string, string) (string, error) {
	return "", f.pgssDBErr
}

type failingSnapshotStore struct{}

func (failingSnapshotStore) CreateSnapshot(
	context.Context, string, string, string, []dto.QueryReport, autosnapshot.SnapshotOpts,
) (uuid.UUID, time.Time, error) {
	return uuid.Nil, time.Time{}, errors.New("must not be called")
}

func (failingSnapshotStore) GetAutosnapshotConfig(context.Context) (autosnapshot.Config, error) {
	return autosnapshot.Config{}, errors.New("must not be called") //nolint:exhaustruct
}

func snapshotHandlers(repo snapshotRepo) *Handlers {
	st := failingSnapshotStore{}

	return &Handlers{ //nolint:exhaustruct
		snapshots: autosnapshot.NewManualCapturer(autosnapshot.NewCapturer(repo, st, zap.NewNop()), st),
	}
}

func postSnapshotReq() serverhttp.PostSnapshotRequestObject {
	return serverhttp.PostSnapshotRequestObject{
		Params: serverhttp.PostSnapshotParams{ //nolint:exhaustruct
			ClusterName: "c1",
			Instance:    "host1",
			Database:    "app",
		},
	}
}

func TestPostSnapshotNotFound(t *testing.T) {
	t.Parallel()

	oneRow := []dto.QueryReport{{QueryID: 1, Datname: "app"}} //nolint:exhaustruct

	tests := []struct {
		name string
		repo snapshotRepo
	}{
		{name: "empty report", repo: snapshotRepo{}},                                                   //nolint:exhaustruct
		{name: "unknown host", repo: snapshotRepo{reports: oneRow, pgssDBErr: repository.ErrNotFound}}, //nolint:exhaustruct
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, err := snapshotHandlers(tt.repo).PostSnapshot(t.Context(), postSnapshotReq())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if _, ok := resp.(serverhttp.PostSnapshot404Response); !ok {
				t.Fatalf("got %T, want PostSnapshot404Response", resp)
			}
		})
	}
}

func TestPostSnapshotWithoutStorage(t *testing.T) {
	t.Parallel()

	resp, err := (&Handlers{}).PostSnapshot(t.Context(), postSnapshotReq()) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := resp.(serverhttp.PostSnapshot501Response); !ok {
		t.Fatalf("got %T, want PostSnapshot501Response", resp)
	}
}
