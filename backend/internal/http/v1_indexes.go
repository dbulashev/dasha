package http

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/indexadvice"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
)

const defaultIndexesBloatLimit = 30

func (s *Handlers) GetIndexesBloat(
	ctx context.Context,
	req serverhttp.GetIndexesBloatRequestObject,
) (serverhttp.GetIndexesBloatResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultIndexesBloatLimit)

	indexes, err := s.repo.GetIndexesBloat(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesBloat404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesBloat | %w", err)
	}

	var ret serverhttp.GetIndexesBloat200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexBloat) serverhttp.IndexBloat {
			return serverhttp.IndexBloat{
				Schema:     t.Schema,
				Table:      t.Table,
				Index:      t.Index,
				BloatBytes: t.BloatBytes,
				IndexBytes: t.IndexBytes,
				Definition: t.Definition,
				Primary:    t.Primary,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesBtreeOnArray(
	ctx context.Context,
	req serverhttp.GetIndexesBtreeOnArrayRequestObject,
) (serverhttp.GetIndexesBtreeOnArrayResponseObject, error) {
	indexes, err := s.repo.GetIndexesBtreeOnArray(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesBtreeOnArray404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesBtreeOnArray | %w", err)
	}

	var ret serverhttp.GetIndexesBtreeOnArray200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexBtreeOnArray) serverhttp.IndexBtreeOnArray {
			return serverhttp.IndexBtreeOnArray{
				Table: t.Table,
				Index: t.Index,
			}
		})

	return ret, nil
}

const defaultIndexesCachingLimit = 30

func (s *Handlers) GetIndexesCaching(
	ctx context.Context,
	req serverhttp.GetIndexesCachingRequestObject,
) (serverhttp.GetIndexesCachingResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultIndexesCachingLimit)

	indexes, err := s.repo.GetIndexesCaching(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesCaching404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesCaching | %w", err)
	}

	var ret serverhttp.GetIndexesCaching200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexCaching) serverhttp.IndexCaching {
			return serverhttp.IndexCaching{
				Schema:  t.Schema,
				Table:   t.Table,
				Index:   t.Index,
				HitRate: t.HitRate,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesHitRate(ctx context.Context,
	req serverhttp.GetIndexesHitRateRequestObject,
) (serverhttp.GetIndexesHitRateResponseObject, error) {
	indexes, err := s.repo.GetIndexesHitRate(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesHitRate404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesHitRate | %w", err)
	}

	var ret serverhttp.GetIndexesHitRate200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexHitRate) serverhttp.IndexHitRate {
			return serverhttp.IndexHitRate{
				Rate: t.Rate,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesInvalidOrNotReady(
	ctx context.Context,
	req serverhttp.GetIndexesInvalidOrNotReadyRequestObject,
) (serverhttp.GetIndexesInvalidOrNotReadyResponseObject, error) {
	indexes, err := s.repo.GetIndexesInvalidOrNotReady(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesInvalidOrNotReady404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesInvalidOrNotReady | %w", err)
	}

	var ret serverhttp.GetIndexesInvalidOrNotReady200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexInvalidOrNotReady) serverhttp.IndexInvalidOrNotReady {
			return serverhttp.IndexInvalidOrNotReady{
				Table:      t.Table,
				IndexName:  t.IndexName,
				IsValid:    t.IsValid,
				IsReady:    t.IsReady,
				Constraint: t.Constraint,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesMissing(
	ctx context.Context,
	req serverhttp.GetIndexesMissingRequestObject,
) (serverhttp.GetIndexesMissingResponseObject, error) {
	indexes, err := s.repo.GetIndexesMissing(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesMissing404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesMissing | %w", err)
	}

	var ret serverhttp.GetIndexesMissing200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexMissing) serverhttp.IndexMissing {
			return serverhttp.IndexMissing{
				Schema:                  t.Schema,
				Table:                   t.Table,
				PercentOfTimesIndexUsed: t.PercentOfTimesIndexUsed,
				EstimatedRows:           t.EstimatedRows,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesSimilar1(
	ctx context.Context,
	req serverhttp.GetIndexesSimilar1RequestObject,
) (serverhttp.GetIndexesSimilar1ResponseObject, error) {
	indexes, err := s.repo.GetIndexesSimilar1(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesSimilar1404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesSimilar1 | %w", err)
	}

	var ret serverhttp.GetIndexesSimilar1200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexSimilar1) serverhttp.IndexSimilar1 {
			return serverhttp.IndexSimilar1{
				Table:                   t.Table,
				I1UniqueIndexName:       t.I1UniqueIndexName,
				I2IndexName:             t.I2IndexName,
				I1UniqueIndexDefinition: t.I1UniqueIndexDefinition,
				I2IndexDefinition:       t.I2IndexDefinition,
				I1UsedInConstraint:      t.I1UsedInConstraint,
				I2UsedInConstraint:      t.I2UsedInConstraint,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesSimilar2(
	ctx context.Context,
	req serverhttp.GetIndexesSimilar2RequestObject,
) (serverhttp.GetIndexesSimilar2ResponseObject, error) {
	indexes, err := s.repo.GetIndexesSimilar2(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesSimilar2404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesSimilar2 | %w", err)
	}

	var ret serverhttp.GetIndexesSimilar2200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexSimilar2) serverhttp.IndexSimilar2 {
			return serverhttp.IndexSimilar2{
				Table:   t.Table,
				FkName:  t.FkName,
				FkName2: t.FkName2,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesSimilar3(
	ctx context.Context,
	req serverhttp.GetIndexesSimilar3RequestObject,
) (serverhttp.GetIndexesSimilar3ResponseObject, error) {
	indexes, err := s.repo.GetIndexesSimilar3(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesSimilar3404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesSimilar3 | %w", err)
	}

	var ret serverhttp.GetIndexesSimilar3200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexSimilar3) serverhttp.IndexSimilar3 {
			return serverhttp.IndexSimilar3{
				Table:                     t.Table,
				I1IndexName:               t.I1IndexName,
				I2IndexName:               t.I2IndexName,
				SimplifiedIndexDefinition: t.SimplifiedIndexDefinition,
				I1IndexDefinition:         t.I1IndexDefinition,
				I2IndexDefinition:         t.I2IndexDefinition,
				I1UsedInConstraint:        t.I1UsedInConstraint,
				I2UsedInConstraint:        t.I2UsedInConstraint,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesTopKBySize(
	ctx context.Context,
	req serverhttp.GetIndexesTopKBySizeRequestObject,
) (serverhttp.GetIndexesTopKBySizeResponseObject, error) {
	indexes, err := s.repo.GetIndexesTopKBySize(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesTopKBySize404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesTopKBySize | %w", err)
	}

	var ret serverhttp.GetIndexesTopKBySize200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexTopKBySize) serverhttp.IndexTopKBySize {
			return serverhttp.IndexTopKBySize{
				Tablespace: t.Tablespace,
				Table:      t.Table,
				Index:      t.Index,
				Size:       t.Size,
				SizeBytes:  t.SizeBytes,
			}
		})

	return ret, nil
}

const defaultIndexesUnusedLimit = 30

func (s *Handlers) GetIndexesUnused(
	ctx context.Context,
	req serverhttp.GetIndexesUnusedRequestObject,
) (serverhttp.GetIndexesUnusedResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultIndexesUnusedLimit)

	allHosts := req.Params.AllHosts != nil && *req.Params.AllHosts

	var threshold int
	if req.Params.Threshold != nil {
		threshold = *req.Params.Threshold
	}

	var (
		indexes []dto.IndexUnused
		err     error
	)

	if allHosts {
		indexes, err = s.repo.GetIndexesUnusedAllHosts(ctx, req.Params.ClusterName, req.Params.Database, threshold, limit, offset)
	} else {
		indexes, err = s.repo.GetIndexesUnused(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, threshold, limit, offset)
	}

	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesUnused404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesUnused | %w", err)
	}

	var ret serverhttp.GetIndexesUnused200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexUnused) serverhttp.IndexUnused {
			return serverhttp.IndexUnused{
				Schema:     t.Schema,
				Table:      t.Table,
				Index:      t.Index,
				SizeBytes:  t.SizeBytes,
				IndexScans: t.IndexScans,
			}
		})

	return ret, nil
}

func (s *Handlers) GetIndexesUnusedReport(
	ctx context.Context,
	req serverhttp.GetIndexesUnusedReportRequestObject,
) (serverhttp.GetIndexesUnusedReportResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultIndexesUnusedLimit)

	scans, err := s.repo.GetIndexUnusedReport(ctx, req.Params.ClusterName, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesUnusedReport404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesUnusedReport | %w", err)
	}

	// The verdict is cluster-wide, so it must be computed over EVERY index on EVERY
	// host before anything is dropped: a page of raw rows cannot be judged on its own.
	// Hence paginate the verdicts, not the input.
	reports := indexadvice.Report(scans, indexadvice.Thresholds{}) //nolint:exhaustruct

	// Biggest first — that is what a DROP reclaims, and it matches /api/indexes/unused.
	sort.SliceStable(reports, func(i, j int) bool {
		return reports[i].SizeBytes > reports[j].SizeBytes
	})

	page := reports
	if offset < len(page) {
		page = page[offset:]
	} else {
		page = nil
	}

	if len(page) > limit {
		page = page[:limit]
	}

	// unreachable_hosts is a required array, and the happy path — every host answered —
	// leaves it nil, which would serialize as null and break a typed client.
	unreachable := scans.Unreachable
	if unreachable == nil {
		unreachable = []string{}
	}

	ret := serverhttp.GetIndexesUnusedReport200JSONResponse{
		Indexes: mapstruct.SliceMap(page, func(r indexadvice.IndexReport) serverhttp.IndexVerdict {
			// Only a partitioned index has children summed into it; leave the count
			// absent otherwise rather than reporting a meaningless zero.
			var partitions *int
			if r.Partitioned {
				partitions = shortcut.Ptr(r.Partitions)
			}

			return serverhttp.IndexVerdict{
				Schema:      r.Schema,
				Table:       r.Table,
				Index:       r.Index,
				Partitioned: r.Partitioned,
				Partitions:  partitions,
				SizeBytes:   r.SizeBytes,
				Verdict:     serverhttp.IndexVerdictVerdict(r.Verdict),
				Reason:      r.Reason,
				PerInstance: mapstruct.SliceMap(r.PerInstance, func(h indexadvice.HostUsage) serverhttp.IndexHostUsage {
					return serverhttp.IndexHostUsage{
						Instance:        h.Instance,
						InRecovery:      h.InRecovery,
						IndexScans:      h.IndexScans,
						WindowDays:      h.WindowDays,
						ScansPerDay:     h.ScansPerDay,
						StatsResetKnown: h.StatsResetKnown,
					}
				}),
			}
		}),
		UnreachableHosts: unreachable,
	}

	return ret, nil
}

const defaultIndexesUsageLimit = 30

func (s *Handlers) GetIndexesUsage(
	ctx context.Context,
	req serverhttp.GetIndexesUsageRequestObject,
) (serverhttp.GetIndexesUsageResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultIndexesUsageLimit)

	indexes, err := s.repo.GetIndexesUsage(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesUsage404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesUsage | %w", err)
	}

	var ret serverhttp.GetIndexesUsage200JSONResponse = mapstruct.SliceMap(
		indexes,
		func(t dto.IndexUsage) serverhttp.IndexUsage {
			return serverhttp.IndexUsage{
				Schema:                  t.Schema,
				Table:                   t.Table,
				PercentOfTimesIndexUsed: t.PercentOfTimesIndexUsed,
				EstimatedRows:           t.EstimatedRows,
			}
		})

	return ret, nil
}
