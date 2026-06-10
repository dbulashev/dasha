package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

func (s *Handlers) GetSnapshotsStatus(
	_ context.Context,
	_ serverhttp.GetSnapshotsStatusRequestObject,
) (serverhttp.GetSnapshotsStatusResponseObject, error) {
	return serverhttp.GetSnapshotsStatus200JSONResponse{
		Available: s.storage != nil,
	}, nil
}

func (s *Handlers) PostSnapshot(
	ctx context.Context,
	req serverhttp.PostSnapshotRequestObject,
) (serverhttp.PostSnapshotResponseObject, error) {
	if s.storage == nil {
		return serverhttp.PostSnapshot501Response{}, nil
	}

	reports, err := s.repo.GetQueriesReport(ctx, req.Params.ClusterName, req.Params.Instance, nil)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.PostSnapshot404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("PostSnapshot | get report: %w", err)
	}

	var pgssStatsReset *time.Time

	resetTime, err := s.repo.GetPgssStatsResetTime(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if err == nil && resetTime != nil {
		pgssStatsReset = &resetTime.Time
	}

	var locks *autosnapshot.LockCapture
	if req.Params.IncludeLocks != nil && *req.Params.IncludeLocks {
		probeCount, probeInterval := 5, 500*time.Millisecond
		if cfg, cerr := s.storage.GetAutosnapshotConfig(ctx); cerr == nil {
			if cfg.LockProbeCount > 0 {
				probeCount = cfg.LockProbeCount
			}

			if cfg.LockProbeInterval > 0 {
				probeInterval = cfg.LockProbeInterval
			}
		}

		lc := autosnapshot.CaptureLocks(ctx, s.repo, req.Params.ClusterName, req.Params.Instance, req.Params.Database, probeCount, probeInterval)
		locks = &lc
	}

	id, createdAt, err := s.storage.CreateSnapshot(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, reports, storage.SnapshotOpts{
		PgssStatsReset: pgssStatsReset,
		LocksData:      locks,
	})
	if err != nil {
		return nil, fmt.Errorf("PostSnapshot | create: %w", err)
	}

	return serverhttp.PostSnapshot201JSONResponse{
		Id:        openapi_types.UUID(id),
		CreatedAt: createdAt,
	}, nil
}

func (s *Handlers) GetSnapshots(
	ctx context.Context,
	req serverhttp.GetSnapshotsRequestObject,
) (serverhttp.GetSnapshotsResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetSnapshots501Response{}, nil
	}

	items, err := s.storage.ListSnapshots(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if err != nil {
		return nil, fmt.Errorf("GetSnapshots | %w", err)
	}

	var ret serverhttp.GetSnapshots200JSONResponse = mapstruct.SliceMap(
		items,
		func(item storage.SnapshotListItem) serverhttp.SnapshotListItem {
			return serverhttp.SnapshotListItem{
				Id:             openapi_types.UUID(item.ID),
				CreatedAt:      item.CreatedAt,
				DashaVersion:   item.DashaVersion,
				JsonVersion:    item.JsonVersion,
				PgssStatsReset: item.PgssStatsReset,
				HasLocks:       shortcut.Ptr(item.HasLocks),
				Reason:         shortcut.Ptr(item.Reason),
			}
		})

	return ret, nil
}

func (s *Handlers) GetSnapshot(
	ctx context.Context,
	req serverhttp.GetSnapshotRequestObject,
) (serverhttp.GetSnapshotResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetSnapshot501Response{}, nil
	}

	reports, err := s.storage.GetSnapshot(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("GetSnapshot | %w", err)
	}

	if reports == nil {
		return serverhttp.GetSnapshot404Response{}, nil
	}

	var ret serverhttp.GetSnapshot200JSONResponse = mapstruct.SliceMap(
		reports, mapQueryReport)

	return ret, nil
}

func (s *Handlers) GetSnapshotLocks(
	ctx context.Context,
	req serverhttp.GetSnapshotLocksRequestObject,
) (serverhttp.GetSnapshotLocksResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetSnapshotLocks501Response{}, nil
	}

	raw, ok, err := s.storage.GetSnapshotLocks(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("GetSnapshotLocks | %w", err)
	}

	if !ok {
		return serverhttp.GetSnapshotLocks404Response{}, nil
	}

	var ls serverhttp.LockSnapshot
	if err := json.Unmarshal(raw, &ls); err != nil {
		return nil, fmt.Errorf("GetSnapshotLocks | unmarshal: %w", err)
	}

	return serverhttp.GetSnapshotLocks200JSONResponse(ls), nil
}
