package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetTablesSchemas(
	ctx context.Context,
	req serverhttp.GetTablesSchemasRequestObject,
) (serverhttp.GetTablesSchemasResponseObject, error) {
	schemas, err := s.repo.GetTablesSchemas(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesSchemas404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesSchemas | %w", err)
	}

	var ret serverhttp.GetTablesSchemas200JSONResponse = schemas

	return ret, nil
}

func (s *Handlers) GetTablesSearch(
	ctx context.Context,
	req serverhttp.GetTablesSearchRequestObject,
) (serverhttp.GetTablesSearchResponseObject, error) {
	limit, _ := paginationDefaults(req.Params.Limit, nil, 50)

	q := ""
	if req.Params.Q != nil {
		q = *req.Params.Q
	}

	tables, err := s.repo.GetTablesSearch(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.Schema,
		q,
		limit,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesSearch404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesSearch | %w", err)
	}

	var ret serverhttp.GetTablesSearch200JSONResponse = tables

	return ret, nil
}

func (s *Handlers) GetTablesTopKBySize(
	ctx context.Context,
	req serverhttp.GetTablesTopKBySizeRequestObject,
) (serverhttp.GetTablesTopKBySizeResponseObject, error) {
	limit, _ := paginationDefaults(req.Params.Limit, nil, 10)

	tables, err := s.repo.GetTablesTopKBySize(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesTopKBySize404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesTopKBySize | %w", err)
	}

	var ret serverhttp.GetTablesTopKBySize200JSONResponse = mapstruct.SliceMap(
		tables,
		func(t dto.TableTopKBySize) serverhttp.TableTopKBySize {
			return serverhttp.TableTopKBySize{
				Table:      t.Table,
				NIdx:       t.NIdx,
				TotalBytes: t.TotalBytes,
				Total:      t.Total,
				Toast:      t.Toast,
				Indexes:    t.Indexes,
				Main:       t.Main,
				Fsm:        t.Fsm,
				Vm:         t.Vm,
				StatInfo:   t.StatInfo,
				Bloat:      t.Bloat,
				Options:    t.Options,
			}
		})

	return ret, nil
}

const defaultTablesCachingLimit = 30

func (s *Handlers) GetTablesCaching(
	ctx context.Context,
	req serverhttp.GetTablesCachingRequestObject,
) (serverhttp.GetTablesCachingResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultTablesCachingLimit)

	tables, err := s.repo.GetTablesCaching(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesCaching404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesCaching | %w", err)
	}

	var ret serverhttp.GetTablesCaching200JSONResponse = mapstruct.SliceMap(
		tables,
		func(t dto.TableCaching) serverhttp.TableCaching {
			return serverhttp.TableCaching{
				Schema:          t.Schema,
				Table:           t.Table,
				HitRate:         t.HitRate,
				IdxHitRate:      t.IdxHitRate,
				ToastHitRate:    t.ToastHitRate,
				ToastIdxHitRate: t.ToastIdxHitRate,
			}
		})

	return ret, nil
}

func (s *Handlers) GetTablesHitRate(
	ctx context.Context,
	req serverhttp.GetTablesHitRateRequestObject,
) (serverhttp.GetTablesHitRateResponseObject, error) {
	tables, err := s.repo.GetTablesHitRate(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesHitRate404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesHitRate | %w", err)
	}

	var ret serverhttp.GetTablesHitRate200JSONResponse = mapstruct.SliceMap(
		tables,
		func(t dto.TableHitRate) serverhttp.TableHitRate {
			return serverhttp.TableHitRate{
				Rate: t.Rate,
			}
		})

	return ret, nil
}

func (s *Handlers) GetTablesPartitions(
	ctx context.Context,
	req serverhttp.GetTablesPartitionsRequestObject,
) (serverhttp.GetTablesPartitionsResponseObject, error) {
	tables, err := s.repo.GetTablesPartitions(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesPartitions404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesPartitions | %w", err)
	}

	var ret serverhttp.GetTablesPartitions200JSONResponse = mapstruct.SliceMap(
		tables,
		func(t dto.TablePartition) serverhttp.TablePartition {
			return serverhttp.TablePartition{
				ParentSchema:       t.ParentSchema,
				Parent:             t.Parent,
				ChildsCount:        t.ChildsCount,
				ChildsSizeBytes:    t.ChildsSizeBytes,
				ChildsSize:         t.ChildsSize,
				ChildsAvgSizeBytes: t.ChildsAvgSizeBytes,
				ChildsAvgSize:      t.ChildsAvgSize,
			}
		})

	return ret, nil
}
