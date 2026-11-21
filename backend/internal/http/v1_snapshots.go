package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/autosnapshot"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/querystats"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetSnapshotsStatus(
	_ context.Context,
	_ serverhttp.GetSnapshotsStatusRequestObject,
) (serverhttp.GetSnapshotsStatusResponseObject, error) {
	return serverhttp.GetSnapshotsStatus200JSONResponse{
		Available: s.snapshotCatalog != nil,
	}, nil
}

func (s *Handlers) PostSnapshot(
	ctx context.Context,
	req serverhttp.PostSnapshotRequestObject,
) (serverhttp.PostSnapshotResponseObject, error) {
	if s.snapshots == nil {
		return serverhttp.PostSnapshot501Response{}, nil
	}

	includeLocks := req.Params.IncludeLocks != nil && *req.Params.IncludeLocks

	c, err := s.snapshots.Capture(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, includeLocks)
	switch {
	case errors.Is(err, repository.ErrNotFound), errors.Is(err, autosnapshot.ErrEmptyReport):
		return serverhttp.PostSnapshot404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("PostSnapshot | %w", err)
	}

	return serverhttp.PostSnapshot201JSONResponse{
		Id:        openapi_types.UUID(c.ID),
		CreatedAt: c.CreatedAt,
	}, nil
}

func (s *Handlers) GetSnapshots(
	ctx context.Context,
	req serverhttp.GetSnapshotsRequestObject,
) (serverhttp.GetSnapshotsResponseObject, error) {
	if s.snapshotCatalog == nil {
		return serverhttp.GetSnapshots501Response{}, nil
	}

	items, err := s.snapshotCatalog.List(ctx, req.Params.ClusterName, req.Params.Instance)
	if err != nil {
		return nil, fmt.Errorf("GetSnapshots | %w", err)
	}

	var ret serverhttp.GetSnapshots200JSONResponse = mapstruct.SliceMap(
		items,
		func(item querystats.SnapshotListItem) serverhttp.SnapshotListItem {
			return serverhttp.SnapshotListItem{
				Id:             openapi_types.UUID(item.ID),
				CreatedAt:      item.CreatedAt,
				Database:       shortcut.Ptr(item.Database),
				Databases:      shortcut.Ptr(item.Databases),
				DashaVersion:   item.DashaVersion,
				JsonVersion:    item.JsonVersion,
				PgssStatsReset: item.PgssStatsReset,
				HasLocks:       shortcut.Ptr(item.HasLocks),
				Reason:         shortcut.Ptr(item.Reason),
				StatsSource:    item.StatsSource,
			}
		})

	return ret, nil
}

func (s *Handlers) GetSnapshot(
	ctx context.Context,
	req serverhttp.GetSnapshotRequestObject,
) (serverhttp.GetSnapshotResponseObject, error) {
	if s.queryStats == nil {
		return serverhttp.GetSnapshot501Response{}, nil
	}

	reports, err := s.queryStats.Snapshot(ctx, req.Id, req.Params.Scope, deref(req.Params.Database))
	switch {
	case errors.Is(err, querystats.ErrSnapshotNotFound):
		return serverhttp.GetSnapshot404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("GetSnapshot | %w", err)
	}

	var ret serverhttp.GetSnapshot200JSONResponse = mapstruct.SliceMap(reports, mapQueryReport)

	return ret, nil
}

func (s *Handlers) GetSnapshotLocks(
	ctx context.Context,
	req serverhttp.GetSnapshotLocksRequestObject,
) (serverhttp.GetSnapshotLocksResponseObject, error) {
	if s.snapshotCatalog == nil {
		return serverhttp.GetSnapshotLocks501Response{}, nil
	}

	raw, err := s.snapshotCatalog.Locks(ctx, req.Id)

	switch {
	case errors.Is(err, querystats.ErrNoLocks):
		return serverhttp.GetSnapshotLocks404Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("GetSnapshotLocks | %w", err)
	}

	var ls serverhttp.LockSnapshot
	if err := json.Unmarshal(raw, &ls); err != nil {
		return nil, fmt.Errorf("GetSnapshotLocks | unmarshal: %w", err)
	}

	return serverhttp.GetSnapshotLocks200JSONResponse(ls), nil
}
