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

func (s *Handlers) GetMaintenanceAutovacuumFreezeMaxAge(
	ctx context.Context,
	req serverhttp.GetMaintenanceAutovacuumFreezeMaxAgeRequestObject,
) (serverhttp.GetMaintenanceAutovacuumFreezeMaxAgeResponseObject, error) {
	data, err := s.repo.GetMaintenanceAutovacuumFreezeMaxAge(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetMaintenanceAutovacuumFreezeMaxAge404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	var ret serverhttp.GetMaintenanceAutovacuumFreezeMaxAge200JSONResponse = mapstruct.SliceMap(
		data,
		func(t dto.MaintenanceAutovacuumFreezeMaxAge) serverhttp.MaintenanceAutovacuumFreezeMaxAge {
			return serverhttp.MaintenanceAutovacuumFreezeMaxAge{
				AutovacuumFreezeMaxAge: t.AutovacuumFreezeMaxAge,
			}
		})

	return ret, nil
}

const defaultMaintenanceInfoLimit = 30

func (s *Handlers) GetMaintenanceInfo(
	ctx context.Context,
	req serverhttp.GetMaintenanceInfoRequestObject,
) (serverhttp.GetMaintenanceInfoResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultMaintenanceInfoLimit)

	data, err := s.repo.GetMaintenanceInfo(
		ctx,
		req.Params.ClusterName,
		req.Params.Instance,
		req.Params.Database,
		req.Params.TableName,
		limit,
		offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetMaintenanceInfo404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceInfo | %w", err)
	}

	var ret serverhttp.GetMaintenanceInfo200JSONResponse = mapstruct.SliceMap(
		data,
		func(t dto.MaintenanceInfo) serverhttp.MaintenanceInfo {
			return serverhttp.MaintenanceInfo{
				Schema:          t.Schema,
				Table:           t.Table,
				LastVacuum:      t.LastVacuum,
				LastAutovacuum:  t.LastAutovacuum,
				LastAnalyze:     t.LastAnalyze,
				LastAutoanalyze: t.LastAutoanalyze,
				DeadRows:        t.DeadRows,
				LiveRows:        t.LiveRows,
			}
		})

	return ret, nil
}

func (s *Handlers) GetMaintenanceTransactionIdDanger(
	ctx context.Context,
	req serverhttp.GetMaintenanceTransactionIdDangerRequestObject,
) (serverhttp.GetMaintenanceTransactionIdDangerResponseObject, error) {
	data, err := s.repo.GetMaintenanceTransactionIdDanger(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetMaintenanceTransactionIdDanger404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceTransactionIdDanger | %w", err)
	}

	var ret serverhttp.GetMaintenanceTransactionIdDanger200JSONResponse = mapstruct.SliceMap(
		data,
		func(t dto.MaintenanceTransactionIdDanger) serverhttp.MaintenanceTransactionIdDanger {
			return serverhttp.MaintenanceTransactionIdDanger{
				Schema:           t.Schema,
				Table:            t.Table,
				TransactionsLeft: t.TransactionsLeft,
			}
		})

	return ret, nil
}

func (s *Handlers) GetMaintenanceAutovacuumSummary(
	ctx context.Context,
	req serverhttp.GetMaintenanceAutovacuumSummaryRequestObject,
) (serverhttp.GetMaintenanceAutovacuumSummaryResponseObject, error) {
	data, err := s.repo.GetMaintenanceAutovacuumSummary(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetMaintenanceAutovacuumSummary404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceAutovacuumSummary | %w", err)
	}

	return serverhttp.GetMaintenanceAutovacuumSummary200JSONResponse{
		TablesDueVacuumOnly:  data.TablesDueVacuumOnly,
		TablesDueAnalyzeOnly: data.TablesDueAnalyzeOnly,
		TablesDueBoth:        data.TablesDueBoth,
		TablesTotal:          data.TablesTotal,
		RunningVacuums:       data.RunningVacuums,
		RunningAnalyzes:      data.RunningAnalyzes,
	}, nil
}

func (s *Handlers) GetMaintenanceVacuumProgress(
	ctx context.Context,
	req serverhttp.GetMaintenanceVacuumProgressRequestObject,
) (serverhttp.GetMaintenanceVacuumProgressResponseObject, error) {
	data, err := s.repo.GetMaintenanceVacuumProgress(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetMaintenanceVacuumProgress404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceVacuumProgress | %w", err)
	}

	var ret serverhttp.GetMaintenanceVacuumProgress200JSONResponse = mapstruct.SliceMap(
		data,
		func(t dto.MaintenanceVacuumProgress) serverhttp.MaintenanceVacuumProgress {
			return serverhttp.MaintenanceVacuumProgress{
				Pid:   t.Pid,
				Phase: t.Phase,
			}
		})

	return ret, nil
}
