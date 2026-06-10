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

func (s *Handlers) GetTablesDescribe(
	ctx context.Context,
	req serverhttp.GetTablesDescribeRequestObject,
) (serverhttp.GetTablesDescribeResponseObject, error) {
	table, err := s.repo.GetTablesDescribe(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.Schema,
		req.Params.Table,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesDescribe404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribe | %w", err)
	}

	ret := serverhttp.GetTablesDescribe200JSONResponse{
		Schema:        table.Schema,
		TableName:     table.TableName,
		TableType:     table.TableType,
		AccessMethod:  table.AccessMethod,
		Tablespace:    table.Tablespace,
		Options:       table.Options,
		SizeTotal:     table.SizeTotal,
		SizeTable:     table.SizeTable,
		SizeToast:     table.SizeToast,
		SizeIndexes:   table.SizeIndexes,
		EstimatedRows: table.EstimatedRows,
		StatInfo:      table.StatInfo,
		PartitionOf:   table.PartitionOf,
		Columns: mapstruct.SliceMap(table.Columns, func(c dto.TableDescribeColumn) serverhttp.TableDescribeColumn {
			return serverhttp.TableDescribeColumn{
				Name:        c.Name,
				Type:        c.Type,
				Collation:   c.Collation,
				Nullable:    c.Nullable,
				Default:     c.Default,
				Storage:     c.Storage,
				Description: c.Description,
				NullFrac:    c.NullFrac,
				NDistinct:   c.NDistinct,
				AvgWidth:    c.AvgWidth,
			}
		}),
		Indexes: mapstruct.SliceMap(table.Indexes, func(i dto.TableDescribeIndex) serverhttp.TableDescribeIndex {
			return serverhttp.TableDescribeIndex{
				Name:       i.Name,
				Definition: i.Definition,
				IsPrimary:  i.IsPrimary,
				IsUnique:   i.IsUnique,
				IsValid:    i.IsValid,
				SizeBytes:  i.SizeBytes,
				Size:       i.Size,
			}
		}),
		CheckConstraints: mapstruct.SliceMap(table.CheckConstraints, func(c dto.TableDescribeConstraint) serverhttp.TableDescribeConstraint {
			return serverhttp.TableDescribeConstraint{
				Name:       c.Name,
				Definition: c.Definition,
			}
		}),
		FkConstraints: mapstruct.SliceMap(table.FkConstraints, func(c dto.TableDescribeConstraint) serverhttp.TableDescribeConstraint {
			return serverhttp.TableDescribeConstraint{
				Name:       c.Name,
				Definition: c.Definition,
			}
		}),
		ReferencedBy: mapstruct.SliceMap(table.ReferencedBy, func(r dto.TableDescribeReferencedBy) serverhttp.TableDescribeReferencedBy {
			return serverhttp.TableDescribeReferencedBy{
				Name:        r.Name,
				SourceTable: r.SourceTable,
				Definition:  r.Definition,
			}
		}),
	}

	return ret, nil
}

func (s *Handlers) GetPgstattupleAvailable(
	ctx context.Context,
	req serverhttp.GetPgstattupleAvailableRequestObject,
) (serverhttp.GetPgstattupleAvailableResponseObject, error) {
	available, err := s.repo.GetPgstattupleAvailable(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
	)
	if err != nil {
		return nil, fmt.Errorf("GetPgstattupleAvailable | %w", err)
	}

	return serverhttp.GetPgstattupleAvailable200JSONResponse{Available: available}, nil
}

func (s *Handlers) GetTablesDescribeBloat(
	ctx context.Context,
	req serverhttp.GetTablesDescribeBloatRequestObject,
) (serverhttp.GetTablesDescribeBloatResponseObject, error) {
	bloat, err := s.repo.GetTablesDescribeBloat(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.Schema,
		req.Params.Table,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesDescribeBloat200JSONResponse{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribeBloat | %w", err)
	}

	return serverhttp.GetTablesDescribeBloat200JSONResponse{
		TableLen:              bloat.TableLen,
		TableLenPretty:        bloat.TableLenPretty,
		ApproxTupleCount:      bloat.ApproxTupleCount,
		ApproxTupleLen:        bloat.ApproxTupleLen,
		ApproxTupleLenPretty:  bloat.ApproxTupleLenPretty,
		ApproxTuplePercent:    bloat.ApproxTuplePercent,
		DeadTupleCount:        bloat.DeadTupleCount,
		DeadTupleLen:          bloat.DeadTupleLen,
		DeadTupleLenPretty:    bloat.DeadTupleLenPretty,
		DeadTuplePercent:      bloat.DeadTuplePercent,
		ApproxFreeSpace:       bloat.ApproxFreeSpace,
		ApproxFreeSpacePretty: bloat.ApproxFreeSpacePretty,
		ApproxFreePercent:     bloat.ApproxFreePercent,
	}, nil
}

const (
	tupleHeaderSize = 23
	itemPointerSize = 4
	pageHeaderSize  = 24
)

func (s *Handlers) GetTablesDescribeVacuumStats(
	ctx context.Context,
	req serverhttp.GetTablesDescribeVacuumStatsRequestObject,
) (serverhttp.GetTablesDescribeVacuumStatsResponseObject, error) {
	stats, err := s.repo.GetTablesDescribeVacuumStats(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.Schema,
		req.Params.Table,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesDescribeVacuumStats404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribeVacuumStats | %w", err)
	}

	if stats == nil {
		return serverhttp.GetTablesDescribeVacuumStats404Response{}, nil
	}

	return serverhttp.GetTablesDescribeVacuumStats200JSONResponse{
		LastVacuum:         stats.LastVacuum,
		LastAutovacuum:     stats.LastAutovacuum,
		LastAnalyze:        stats.LastAnalyze,
		LastAutoanalyze:    stats.LastAutoanalyze,
		DeadTuples:         stats.DeadTuples,
		LiveTuples:         stats.LiveTuples,
		ModSinceAnalyze:    stats.ModSinceAnalyze,
		InsSinceVacuum:     stats.InsSinceVacuum,
		VacuumThreshold:    stats.VacuumThreshold,
		AnalyzeThreshold:   stats.AnalyzeThreshold,
		InsertVacThreshold: stats.InsertVacThreshold,
	}, nil
}

func (s *Handlers) GetTablesDescribeRowEstimate(
	ctx context.Context,
	req serverhttp.GetTablesDescribeRowEstimateRequestObject,
) (serverhttp.GetTablesDescribeRowEstimateResponseObject, error) {
	est, err := s.repo.GetTablesDescribeRowEstimate(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.Schema,
		req.Params.Table,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesDescribeRowEstimate404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribeRowEstimate | %w", err)
	}

	if est == nil {
		return serverhttp.GetTablesDescribeRowEstimate404Response{}, nil
	}

	nullBitmapSize := (est.ColumnsTotal + 7) / 8
	estimatedRowSize := tupleHeaderSize + nullBitmapSize + est.SumAvgWidth
	toastThreshold := est.BlockSize / 4
	willToast := estimatedRowSize > toastThreshold
	pageUsable := est.BlockSize - pageHeaderSize
	availableSpace := pageUsable * est.Fillfactor / 100
	rowsPerPage := 0
	if estimatedRowSize+itemPointerSize > 0 {
		rowsPerPage = availableSpace / (estimatedRowSize + itemPointerSize)
	}
	reservedSpace := pageUsable * (100 - est.Fillfactor) / 100
	rowsFitInReserved := 0
	if estimatedRowSize+itemPointerSize > 0 {
		rowsFitInReserved = reservedSpace / (estimatedRowSize + itemPointerSize)
	}

	candidates := make([]serverhttp.ToastCandidate, 0, len(est.ToastCandidates))
	for _, tc := range est.ToastCandidates {
		candidates = append(candidates, serverhttp.ToastCandidate{
			ColumnName: tc.ColumnName,
			AvgWidth:   tc.AvgWidth,
			Storage:    tc.Storage,
		})
	}

	return serverhttp.GetTablesDescribeRowEstimate200JSONResponse{
		BlockSize:         est.BlockSize,
		Fillfactor:        est.Fillfactor,
		ColumnsTotal:      est.ColumnsTotal,
		ColumnsWithStats:  est.ColumnsWithStats,
		SumAvgWidth:       est.SumAvgWidth,
		TupleHeaderSize:   tupleHeaderSize,
		NullBitmapSize:    nullBitmapSize,
		EstimatedRowSize:  estimatedRowSize,
		ToastThreshold:    toastThreshold,
		WillToast:         willToast,
		PageUsable:        pageUsable,
		AvailableSpace:    availableSpace,
		RowsPerPage:       rowsPerPage,
		ReservedSpace:     reservedSpace,
		RowsFitInReserved: rowsFitInReserved,
		ToastCandidates:   candidates,
	}, nil
}

const defaultDescribePartitionsLimit = 20

func (s *Handlers) GetTablesDescribePartitions(
	ctx context.Context,
	req serverhttp.GetTablesDescribePartitionsRequestObject,
) (serverhttp.GetTablesDescribePartitionsResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultDescribePartitionsLimit)

	partitions, err := s.repo.GetTablesDescribePartitions(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.Schema,
		req.Params.Table,
		limit, offset,
	)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetTablesDescribePartitions200JSONResponse{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetTablesDescribePartitions | %w", err)
	}

	ret := mapstruct.SliceMap(partitions, func(p dto.TableDescribePartition) serverhttp.TableDescribePartition {
		return serverhttp.TableDescribePartition{
			Schema:              p.Schema,
			Name:                p.Name,
			PartitionExpression: p.PartitionExpression,
			SizeBytes:           p.SizeBytes,
			Size:                p.Size,
		}
	})

	return serverhttp.GetTablesDescribePartitions200JSONResponse(ret), nil
}
