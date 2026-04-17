package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/storage"
)

type Handlers struct {
	cfg     *config.Config
	repo    repository.Repository
	storage *storage.Storage
}

func NewDashaHandlers(cfg *config.Config, repo repository.Repository, st *storage.Storage) *Handlers {
	return &Handlers{cfg: cfg, repo: repo, storage: st}
}

func mapQueryReport(t dto.QueryReport) serverhttp.QueryReport {
	return serverhttp.QueryReport{
		QueryID:              t.QueryID,
		Query:                t.Query,
		Rows:                 t.Rows,
		RowsPct:              t.RowsPct,
		Calls:                t.Calls,
		CallsPct:             t.CallsPct,
		TotalTimeMs:          t.TotalTimeMs,
		TotalTimePct:         t.TotalTimePct,
		ExecTimeMs:           t.ExecTimeMs,
		MinExecTimeMs:        t.MinExecTimeMs,
		MaxExecTimeMs:        t.MaxExecTimeMs,
		MeanExecTimeMs:       t.MeanExecTimeMs,
		PlanTimeMs:           t.PlanTimeMs,
		MinPlanTimeMs:        t.MinPlanTimeMs,
		MaxPlanTimeMs:        t.MaxPlanTimeMs,
		MeanPlanTimeMs:       t.MeanPlanTimeMs,
		IoTimeMs:             t.IoTimeMs,
		IoTimePct:            t.IoTimePct,
		CpuTimeMs:            t.CpuTimeMs,
		CpuTimePct:           t.CpuTimePct,
		CacheHitRatio:        t.CacheHitRatio,
		SharedBlksDirtiedPct: t.SharedBlksDirtiedPct,
		SharedBlksWrittenPct: t.SharedBlksWrittenPct,
		WalBytes:             t.WalBytes,
		WalBytesPct:          t.WalBytesPct,
		WalRecords:           t.WalRecords,
		WalFpi:               t.WalFpi,
		TempBlks:             t.TempBlks,
		TempBlksPct:          t.TempBlksPct,
	}
}

func mapQueryReportMetrics(t dto.QueryReport) serverhttp.QueryReportMetrics {
	return serverhttp.QueryReportMetrics{
		Rows:                 t.Rows,
		RowsPct:              t.RowsPct,
		Calls:                t.Calls,
		CallsPct:             t.CallsPct,
		TotalTimeMs:          t.TotalTimeMs,
		TotalTimePct:         t.TotalTimePct,
		ExecTimeMs:           t.ExecTimeMs,
		MinExecTimeMs:        t.MinExecTimeMs,
		MaxExecTimeMs:        t.MaxExecTimeMs,
		MeanExecTimeMs:       t.MeanExecTimeMs,
		PlanTimeMs:           t.PlanTimeMs,
		MinPlanTimeMs:        t.MinPlanTimeMs,
		MaxPlanTimeMs:        t.MaxPlanTimeMs,
		MeanPlanTimeMs:       t.MeanPlanTimeMs,
		IoTimeMs:             t.IoTimeMs,
		IoTimePct:            t.IoTimePct,
		CpuTimeMs:            t.CpuTimeMs,
		CpuTimePct:           t.CpuTimePct,
		CacheHitRatio:        t.CacheHitRatio,
		SharedBlksDirtiedPct: t.SharedBlksDirtiedPct,
		SharedBlksWrittenPct: t.SharedBlksWrittenPct,
		WalBytes:             t.WalBytes,
		WalBytesPct:          t.WalBytesPct,
		WalRecords:           t.WalRecords,
		WalFpi:               t.WalFpi,
		TempBlks:             t.TempBlks,
		TempBlksPct:          t.TempBlksPct,
	}
}

func paginationDefaults(limitPtr, offsetPtr *int, defaultLimit int) (int, int) {
	limit := defaultLimit
	if limitPtr != nil {
		limit = *limitPtr
	}

	offset := 0
	if offsetPtr != nil {
		offset = *offsetPtr
	}

	return limit, offset
}

func (s *Handlers) GetAuthInfo(
	_ context.Context,
	_ serverhttp.GetAuthInfoRequestObject,
) (serverhttp.GetAuthInfoResponseObject, error) {
	mode := serverhttp.AuthInfoMode(s.cfg.Auth.Mode)
	if mode == "" {
		mode = serverhttp.None
	}

	enableReset := s.cfg.EnableQueryStatsReset
	resp := serverhttp.GetAuthInfo200JSONResponse{
		Mode:                  mode,
		EnableQueryStatsReset: &enableReset,
	}

	if s.cfg.Auth.Mode == config.AuthModeOIDC {
		loginURL := "/auth/login"
		resp.OidcLoginUrl = &loginURL
	}

	return resp, nil
}

func (s *Handlers) GetClusters(
	ctx context.Context,
	_ serverhttp.GetClustersRequestObject,
) (serverhttp.GetClustersResponseObject, error) {
	c, err := s.repo.Clusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetClusters | %w", err)
	}

	var ret serverhttp.GetClusters200JSONResponse

	for _, v := range c {
		instances := mapstruct.SliceMap(v.Instances, func(t dto.Instance) serverhttp.ClusterInstance {
			return serverhttp.ClusterInstance{
				HostName: shortcut.Ptr(t.HostName.String()),
			}
		})

		ret = append(ret, serverhttp.Cluster{
			Name:      shortcut.Ptr(v.Name.String()),
			Instances: &instances,
			Databases: &v.Databases,
		})
	}

	return ret, nil
}

func (s *Handlers) GetCommonSummary(
	ctx context.Context,
	req serverhttp.GetCommonSummaryRequestObject,
) (serverhttp.GetCommonSummaryResponseObject, error) {
	summary, err := s.repo.GetCommonSummary(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)

	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetCommonSummary404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetCommonSummary | %w", err)
	}

	var ret serverhttp.GetCommonSummary200JSONResponse = mapstruct.SliceMap(
		summary,
		func(t dto.CommonSummary) serverhttp.CommonSummary {
			return serverhttp.CommonSummary{
				Namespace:       t.Namespace,
				Amount:          t.Amount,
				ApproxSize:      t.ApproxSize,
				ApproxSizeBytes: t.ApproxSizeBytes,
				Kind:            t.Kind,
			}
		})

	return ret, nil
}

func (s *Handlers) GetInstanceInfo(
	ctx context.Context,
	req serverhttp.GetInstanceInfoRequestObject,
) (serverhttp.GetInstanceInfoResponseObject, error) {
	info, err := s.repo.GetInstanceInfo(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetInstanceInfo404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetInstanceInfo | %w", err)
	}

	ret := serverhttp.GetInstanceInfo200JSONResponse{
		InRecovery:  info.InRecovery,
		VersionNum:  info.VersionNum,
		Version:     &info.Version,
		VersionFull: &info.VersionFull,
	}

	return ret, nil
}

func (s *Handlers) GetDatabaseUsers(
	ctx context.Context,
	req serverhttp.GetDatabaseUsersRequestObject,
) (serverhttp.GetDatabaseUsersResponseObject, error) {
	users, err := s.repo.GetDatabaseUsers(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetDatabaseUsers404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetDatabaseUsers | %w", err)
	}

	var ret serverhttp.GetDatabaseUsers200JSONResponse = users

	return ret, nil
}

func (s *Handlers) GetConnectionStates(
	ctx context.Context,
	req serverhttp.GetConnectionStatesRequestObject,
) (serverhttp.GetConnectionStatesResponseObject, error) {
	states, err := s.repo.GetConnectionStates(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetConnectionStates404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetConnectionStates | %w", err)
	}

	var ret serverhttp.GetConnectionStates200JSONResponse = mapstruct.SliceMap(
		states,
		func(t dto.ConnectionStates) serverhttp.ConnectionState {
			return serverhttp.ConnectionState{
				State: t.State,
				Count: t.Count,
			}
		})

	return ret, nil
}

const defaultConnectionSourcesLimit = 30

func (s *Handlers) GetConnectionSources(
	ctx context.Context,
	req serverhttp.GetConnectionSourcesRequestObject,
) (serverhttp.GetConnectionSourcesResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultConnectionSourcesLimit)

	sources, err := s.repo.GetConnectionSources(ctx, req.Params.ClusterName, req.Params.Instance, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetConnectionSources404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetConnectionSources | %w", err)
	}

	var ret serverhttp.GetConnectionSources200JSONResponse = mapstruct.SliceMap(
		sources,
		func(t dto.ConnectionSources) serverhttp.ConnectionSource {
			return serverhttp.ConnectionSource{
				ApplicationName:  t.ApplicationName,
				ClientAddr:       t.ClientAddr,
				Database:         t.Database,
				TotalConnections: t.TotalConnections,
				Username:         t.Username,
			}
		})

	return ret, nil
}

const defaultConnectionStatActivityLimit = 50

func (s *Handlers) GetConnectionStatActivity(
	ctx context.Context,
	req serverhttp.GetConnectionStatActivityRequestObject,
) (serverhttp.GetConnectionStatActivityResponseObject, error) {
	limit := defaultConnectionStatActivityLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	offset := 0
	if req.Params.Offset != nil {
		offset = *req.Params.Offset
	}

	var username, state string
	if req.Params.Username != nil {
		username = *req.Params.Username
	}

	if req.Params.State != nil {
		state = *req.Params.State
	}

	activity, err := s.repo.GetConnectionStatActivity(
		ctx, req.Params.ClusterName, req.Params.Instance,
		limit, offset, username, state)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetConnectionStatActivity404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetConnectionStatActivity | %w", err)
	}

	var ret serverhttp.GetConnectionStatActivity200JSONResponse = mapstruct.SliceMap(
		activity,
		func(t dto.ConnectionStatActivity) serverhttp.ConnectionStatActivity {
			return serverhttp.ConnectionStatActivity{
				ApplicationName: t.ApplicationName,
				BackendType:     t.BackendType,
				ClientAddr:      t.ClientAddr,
				Database:        t.Database,
				Pid:             t.Pid,
				Ssl:             t.Ssl,
				State:           t.State,
				UserName:        t.UserName,
			}
		})

	return ret, nil
}

func (s *Handlers) GetFkTypeMismatch(
	ctx context.Context,
	req serverhttp.GetFkTypeMismatchRequestObject,
) (serverhttp.GetFkTypeMismatchResponseObject, error) {
	fkTypeMismatches, err := s.repo.GetFkTypeMismatch(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetFkTypeMismatch404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetFkTypeMismatch | %w", err)
	}

	var ret serverhttp.GetFkTypeMismatch200JSONResponse = mapstruct.SliceMap(
		fkTypeMismatches,
		func(t dto.FkTypeMismatch) serverhttp.FkTypeMismatch {
			return serverhttp.FkTypeMismatch{
				FkName:        t.FkName,
				FromRel:       t.FromRel,
				RelAttNames:   t.RelAttNames,
				ToRel:         t.ToRel,
				ToRelAttNames: t.ToRelAttNames,
			}
		})

	return ret, nil
}

func (s *Handlers) GetFksPossibleNulls(
	ctx context.Context,
	req serverhttp.GetFksPossibleNullsRequestObject,
) (serverhttp.GetFksPossibleNullsResponseObject, error) {
	fksPossibleNulls, err := s.repo.GetFksPossibleNulls(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetFksPossibleNulls404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetFksPossibleNulls | %w", err)
	}

	var ret serverhttp.GetFksPossibleNulls200JSONResponse = mapstruct.SliceMap(
		fksPossibleNulls,
		func(t dto.FksPossibleNulls) serverhttp.FksPossibleNulls {
			return serverhttp.FksPossibleNulls{
				FkName:   t.FkName,
				RelName:  t.RelName,
				AttNames: t.AttNames,
			}
		})

	return ret, nil
}

func (s *Handlers) GetFksPossibleSimilar(
	ctx context.Context,
	req serverhttp.GetFksPossibleSimilarRequestObject,
) (serverhttp.GetFksPossibleSimilarResponseObject, error) {
	fksPossibleSimilar, err := s.repo.GetFksPossibleSimilar(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetFksPossibleSimilar404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetFksPossibleSimilar | %w", err)
	}

	var ret serverhttp.GetFksPossibleSimilar200JSONResponse = mapstruct.SliceMap(
		fksPossibleSimilar,
		func(t dto.FksPossibleSimilar) serverhttp.FksPossibleSimilar {
			return serverhttp.FksPossibleSimilar{
				Table:   t.Table,
				FkName:  t.FkName,
				Fk1Name: t.Fk1Name,
			}
		})

	return ret, nil
}

func (s *Handlers) GetInvalidConstraints(
	ctx context.Context,
	req serverhttp.GetInvalidConstraintsRequestObject,
) (serverhttp.GetInvalidConstraintsResponseObject, error) {
	invalidConstraints, err := s.repo.GetInvalidConstraints(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetInvalidConstraints404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetInvalidConstraints | %w", err)
	}

	var ret serverhttp.GetInvalidConstraints200JSONResponse = mapstruct.SliceMap(
		invalidConstraints,
		func(t dto.InvalidConstraint) serverhttp.InvalidConstraint {
			return serverhttp.InvalidConstraint{
				Schema:           t.Schema,
				Table:            t.Table,
				Name:             t.Name,
				ReferencedSchema: t.ReferencedSchema,
				ReferencedTable:  t.ReferencedTable,
			}
		})

	return ret, nil
}

func (s *Handlers) GetDatabaseSize(
	ctx context.Context,
	req serverhttp.GetDatabaseSizeRequestObject,
) (serverhttp.GetDatabaseSizeResponseObject, error) {
	size, err := s.repo.GetDatabaseSize(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetDatabaseSize404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetDatabaseSize | %w", err)
	}

	ret := serverhttp.GetDatabaseSize200JSONResponse{
		SizeBytes:  size.SizeBytes,
		SizePretty: size.SizePretty,
	}

	return ret, nil
}

func (s *Handlers) GetStatsResetTime(
	ctx context.Context,
	req serverhttp.GetStatsResetTimeRequestObject,
) (serverhttp.GetStatsResetTimeResponseObject, error) {
	statsResetTimes, err := s.repo.GetStatsResetTime(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetStatsResetTime404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetStatsResetTime | %w", err)
	}

	var ret serverhttp.GetStatsResetTime200JSONResponse = mapstruct.SliceMap(
		statsResetTimes,
		func(t dto.StatsResetTime) serverhttp.StatsResetTime {
			return serverhttp.StatsResetTime{
				Time: t.Time,
			}
		})

	return ret, nil
}

func (s *Handlers) GetPgssStatsResetTime(
	ctx context.Context,
	req serverhttp.GetPgssStatsResetTimeRequestObject,
) (serverhttp.GetPgssStatsResetTimeResponseObject, error) {
	t, err := s.repo.GetPgssStatsResetTime(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetPgssStatsResetTime404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetPgssStatsResetTime | %w", err)
	}

	if t == nil {
		return serverhttp.GetPgssStatsResetTime404Response{}, nil
	}

	return serverhttp.GetPgssStatsResetTime200JSONResponse{Time: t.Time}, nil
}

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

func (s *Handlers) GetQueriesBlocked(
	ctx context.Context,
	req serverhttp.GetQueriesBlockedRequestObject,
) (serverhttp.GetQueriesBlockedResponseObject, error) {
	queries, err := s.repo.GetQueriesBlocked(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesBlocked404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesBlocked | %w", err)
	}

	var ret serverhttp.GetQueriesBlocked200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryBlocked) serverhttp.QueryBlocked {
			return serverhttp.QueryBlocked{
				LockedItem:                            t.LockedItem,
				BlockedPid:                            t.BlockedPid,
				BlockedUser:                           t.BlockedUser,
				BlockedQuery:                          sanitize.SQL(t.BlockedQuery),
				BlockedDuration:                       t.BlockedDuration,
				BlockedMode:                           t.BlockedMode,
				BlockingPid:                           t.BlockingPid,
				BlockingUser:                          t.BlockingUser,
				StateOfBlockingProcess:                t.StateOfBlockingProcess,
				CurrentOrRecentQueryInBlockingProcess: sanitize.SQL(t.CurrentOrRecentQueryInBlockingProcess),
				BlockingDuration:                      t.BlockingDuration,
				BlockingMode:                          t.BlockingMode,
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesRunning(
	ctx context.Context,
	req serverhttp.GetQueriesRunningRequestObject,
) (serverhttp.GetQueriesRunningResponseObject, error) {
	minDuration := 3
	if req.Params.MinDuration != nil {
		minDuration = *req.Params.MinDuration
	}

	queries, err := s.repo.GetQueriesRunning(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, minDuration)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesRunning404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesRunning | %w", err)
	}

	var ret serverhttp.GetQueriesRunning200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryRunning) serverhttp.QueryRunning {
			return serverhttp.QueryRunning{
				Pid:         t.Pid,
				State:       t.State,
				Source:      t.Source,
				Duration:    t.Duration,
				Waiting:     t.Waiting,
				Query:       sanitize.SQL(t.Query),
				StartedAt:   t.StartedAt,
				DurationMs:  t.DurationMs,
				User:        t.User,
				BackendType: t.BackendType,
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10ByTime(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ByTimeRequestObject,
) (serverhttp.GetQueriesTop10ByTimeResponseObject, error) {
	queries, err := s.repo.GetQueriesTop10ByTime(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesTop10ByTime404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10ByTime | %w", err)
	}

	var ret serverhttp.GetQueriesTop10ByTime200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryTop10ByTime) serverhttp.QueryTop10ByTime {
			return serverhttp.QueryTop10ByTime{
				QueryID:    t.QueryID,
				ExecTime:   t.ExecTime,
				ExecTimeMs: t.ExecTimeMs,
				IoCpuPct:   t.IoCpuPct,
				IoPct:      t.IoPct,
				CpuPct:     t.CpuPct,
				QueryTrunc: sanitize.SQL(t.QueryTrunc),
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10ByWal(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ByWalRequestObject,
) (serverhttp.GetQueriesTop10ByWalResponseObject, error) {
	queries, err := s.repo.GetQueriesTop10ByWal(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesTop10ByWal404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10ByWal | %w", err)
	}

	var ret serverhttp.GetQueriesTop10ByWal200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryTop10ByWal) serverhttp.QueryTop10ByWal {
			return serverhttp.QueryTop10ByWal{
				QueryID:    t.QueryID,
				WalVolume:  t.WalVolume,
				WalBytes:   t.WalBytes,
				QueryTrunc: sanitize.SQL(t.QueryTrunc),
			}
		})

	return ret, nil
}

func (s *Handlers) GetQueriesTop10Chart(
	ctx context.Context,
	req serverhttp.GetQueriesTop10ChartRequestObject,
) (serverhttp.GetQueriesTop10ChartResponseObject, error) {
	items, err := s.repo.GetQueriesTop10Chart(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesTop10Chart404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesTop10Chart | %w", err)
	}

	ret := serverhttp.GetQueriesTop10Chart200JSONResponse{} //nolint:exhaustruct

	for _, item := range items {
		entry := serverhttp.QueryTop10ChartItem{
			QueryID: item.QueryID,
			Pct:     item.Pct,
		}

		switch item.Metric {
		case "calls":
			ret.Calls = append(ret.Calls, entry)
		case "total_exec_time":
			ret.TotalExecTime = append(ret.TotalExecTime, entry)
		case "rows":
			ret.Rows = append(ret.Rows, entry)
		case "shared_blks_hit":
			ret.SharedBlksHit = append(ret.SharedBlksHit, entry)
		case "shared_blks_read":
			ret.SharedBlksRead = append(ret.SharedBlksRead, entry)
		case "shared_blks_dirtied":
			ret.SharedBlksDirtied = append(ret.SharedBlksDirtied, entry)
		case "temp_blks_read":
			ret.TempBlksRead = append(ret.TempBlksRead, entry)
		case "temp_blks_written":
			ret.TempBlksWritten = append(ret.TempBlksWritten, entry)
		case "wal_records":
			ret.WalRecords = append(ret.WalRecords, entry)
		}
	}

	return ret, nil
}

func (s *Handlers) GetQueriesReport(
	ctx context.Context,
	req serverhttp.GetQueriesReportRequestObject,
) (serverhttp.GetQueriesReportResponseObject, error) {
	var excludeUsers []string
	if req.Params.ExcludeUsers != nil {
		excludeUsers = *req.Params.ExcludeUsers
	}

	queries, err := s.repo.GetQueriesReport(ctx, req.Params.ClusterName, req.Params.Instance, excludeUsers)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueriesReport404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueriesReport | %w", err)
	}

	var ret serverhttp.GetQueriesReport200JSONResponse = mapstruct.SliceMap(
		queries,
		func(t dto.QueryReport) serverhttp.QueryReport {
			t.Query = sanitize.SQL(t.Query)
			return mapQueryReport(t)
		})

	return ret, nil
}

func (s *Handlers) GetQueriesCompare(
	ctx context.Context,
	req serverhttp.GetQueriesCompareRequestObject,
) (serverhttp.GetQueriesCompareResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetQueriesCompare501Response{}, nil
	}

	// Load source A (always a snapshot).
	reportsA, err := s.storage.GetSnapshot(ctx, uuid.UUID(req.Params.SnapshotA))
	if err != nil {
		return nil, fmt.Errorf("GetQueriesCompare | snapshot A: %w", err)
	}

	if reportsA == nil {
		return serverhttp.GetQueriesCompare404Response{}, nil
	}

	var reportsB []dto.QueryReport

	if req.Params.SnapshotB != nil {
		reportsB, err = s.storage.GetSnapshot(ctx, uuid.UUID(*req.Params.SnapshotB))
		if err != nil {
			return nil, fmt.Errorf("GetQueriesCompare | snapshot B: %w", err)
		}

		if reportsB == nil {
			return serverhttp.GetQueriesCompare404Response{}, nil
		}
	} else {
		var excludeUsers []string
		if req.Params.ExcludeUsers != nil {
			excludeUsers = *req.Params.ExcludeUsers
		}

		reportsB, err = s.repo.GetQueriesReport(ctx, req.Params.ClusterName, req.Params.Instance, excludeUsers)
		if errors.Is(err, repository.ErrNotFound) {
			return serverhttp.GetQueriesCompare404Response{}, nil
		}

		if err != nil {
			return nil, fmt.Errorf("GetQueriesCompare | live report: %w", err)
		}
	}

	joined := mapstruct.SliceFullJoin(
		reportsA, reportsB,
		func(r dto.QueryReport) int64 { return r.QueryID },
		func(r dto.QueryReport) int64 { return r.QueryID },
	)

	items := make([]serverhttp.QueryCompareItem, 0, len(joined))

	for _, pair := range joined {
		var queryID int64

		var query string

		var left, right *serverhttp.QueryReportMetrics

		if pair.Left != nil {
			pair.Left.Query = sanitize.SQL(pair.Left.Query)
			queryID = pair.Left.QueryID
			query = pair.Left.Query
			m := mapQueryReportMetrics(*pair.Left)
			left = &m
		}

		if pair.Right != nil {
			pair.Right.Query = sanitize.SQL(pair.Right.Query)
			if queryID == 0 {
				queryID = pair.Right.QueryID
			}

			if query == "" {
				query = pair.Right.Query
			}

			m := mapQueryReportMetrics(*pair.Right)
			right = &m
		}

		items = append(items, serverhttp.QueryCompareItem{
			QueryID: queryID,
			Query:   query,
			Left:    left,
			Right:   right,
		})
	}

	return serverhttp.GetQueriesCompare200JSONResponse(items), nil
}

func (s *Handlers) GetQueryStatsStatus(
	ctx context.Context,
	req serverhttp.GetQueryStatsStatusRequestObject,
) (serverhttp.GetQueryStatsStatusResponseObject, error) {
	status, err := s.repo.GetQueryStatsStatus(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetQueryStatsStatus404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetQueryStatsStatus | %w", err)
	}

	ret := serverhttp.GetQueryStatsStatus200JSONResponse{
		Available: status.Available,
		Enabled:   status.Enabled,
		Readable:  status.Readable,
	}

	return ret, nil
}

func (s *Handlers) PostQueriesResetStats(
	ctx context.Context,
	req serverhttp.PostQueriesResetStatsRequestObject,
) (serverhttp.PostQueriesResetStatsResponseObject, error) {
	if !s.cfg.EnableQueryStatsReset {
		return serverhttp.PostQueriesResetStats403Response{}, nil
	}

	err := s.repo.ResetQueryStats(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.PostQueriesResetStats404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("PostQueriesResetStats | %w", err)
	}

	return serverhttp.PostQueriesResetStats204Response{}, nil
}

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

	id, createdAt, err := s.storage.CreateSnapshot(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database, reports, pgssStatsReset)
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

func (s *Handlers) GetProgressAnalyze(
	ctx context.Context,
	req serverhttp.GetProgressAnalyzeRequestObject,
) (serverhttp.GetProgressAnalyzeResponseObject, error) {
	progress, err := s.repo.GetProgressAnalyze(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetProgressAnalyze404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetProgressAnalyze | %w", err)
	}

	var ret serverhttp.GetProgressAnalyze200JSONResponse = mapstruct.SliceMap(
		progress,
		func(t dto.ProgressAnalyze) serverhttp.ProgressAnalyze {
			return serverhttp.ProgressAnalyze{
				Pid:               t.Pid,
				Datname:           t.Datname,
				TableName:         t.TableName,
				Phase:             t.Phase,
				SampleBlksTotal:   t.SampleBlksTotal,
				SampleBlksScanned: t.SampleBlksScanned,
				ExtStatsTotal:     t.ExtStatsTotal,
				ExtStatsComputed:  t.ExtStatsComputed,
				CurrentChildTable: t.CurrentChildTable,
			}
		})

	return ret, nil
}

func (s *Handlers) GetProgressBaseBackup(
	ctx context.Context,
	req serverhttp.GetProgressBaseBackupRequestObject,
) (serverhttp.GetProgressBaseBackupResponseObject, error) {
	progress, err := s.repo.GetProgressBaseBackup(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetProgressBaseBackup404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetProgressBaseBackup | %w", err)
	}

	var ret serverhttp.GetProgressBaseBackup200JSONResponse = mapstruct.SliceMap(
		progress,
		func(t dto.ProgressBaseBackup) serverhttp.ProgressBaseBackup {
			return serverhttp.ProgressBaseBackup{
				Pid:                 t.Pid,
				Phase:               t.Phase,
				BackupTotal:         t.BackupTotal,
				BackupStreamed:      t.BackupStreamed,
				ProgressPercentage:  t.ProgressPercentage,
				TablespacesTotal:    t.TablespacesTotal,
				TablespacesStreamed: t.TablespacesStreamed,
			}
		})

	return ret, nil
}

func (s *Handlers) GetProgressCluster(
	ctx context.Context,
	req serverhttp.GetProgressClusterRequestObject,
) (serverhttp.GetProgressClusterResponseObject, error) {
	progress, err := s.repo.GetProgressCluster(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetProgressCluster404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetProgressCluster | %w", err)
	}

	var ret serverhttp.GetProgressCluster200JSONResponse = mapstruct.SliceMap(
		progress,
		func(t dto.ProgressCluster) serverhttp.ProgressCluster {
			return serverhttp.ProgressCluster{
				Pid:               t.Pid,
				Datname:           t.Datname,
				TableName:         t.TableName,
				Command:           t.Command,
				Phase:             t.Phase,
				ClusterIndex:      t.ClusterIndex,
				HeapTuplesScanned: t.HeapTuplesScanned,
				HeapTuplesWritten: t.HeapTuplesWritten,
				HeapBlksTotal:     t.HeapBlksTotal,
				HeapBlksScanned:   t.HeapBlksScanned,
				IndexRebuildCount: t.IndexRebuildCount,
			}
		})

	return ret, nil
}

func (s *Handlers) GetProgressIndex(
	ctx context.Context,
	req serverhttp.GetProgressIndexRequestObject,
) (serverhttp.GetProgressIndexResponseObject, error) {
	progress, err := s.repo.GetProgressIndex(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetProgressIndex404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetProgressIndex | %w", err)
	}

	var ret serverhttp.GetProgressIndex200JSONResponse = mapstruct.SliceMap(
		progress,
		func(t dto.ProgressIndex) serverhttp.ProgressIndex {
			return serverhttp.ProgressIndex{
				Pid:              t.Pid,
				Datname:          t.Datname,
				TableName:        t.TableName,
				IndexName:        t.IndexName,
				Phase:            t.Phase,
				LockersTotal:     t.LockersTotal,
				LockersDone:      t.LockersDone,
				CurrentLockerPid: t.CurrentLockerPid,
				BlocksTotal:      t.BlocksTotal,
				BlocksDone:       t.BlocksDone,
				TuplesTotal:      t.TuplesTotal,
				TuplesDone:       t.TuplesDone,
				PartitionsTotal:  t.PartitionsTotal,
				PartitionsDone:   t.PartitionsDone,
			}
		})

	return ret, nil
}

func (s *Handlers) GetProgressVacuum(
	ctx context.Context,
	req serverhttp.GetProgressVacuumRequestObject,
) (serverhttp.GetProgressVacuumResponseObject, error) {
	progress, err := s.repo.GetProgressVacuum(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetProgressVacuum404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetProgressVacuum | %w", err)
	}

	var ret serverhttp.GetProgressVacuum200JSONResponse = mapstruct.SliceMap(
		progress,
		func(t dto.ProgressVacuum) serverhttp.ProgressVacuum {
			return serverhttp.ProgressVacuum{
				Pid:              t.Pid,
				Datname:          t.Datname,
				TableName:        t.TableName,
				Phase:            t.Phase,
				HeapBlksTotal:    t.HeapBlksTotal,
				HeapBlksScanned:  t.HeapBlksScanned,
				HeapBlksVacuumed: t.HeapBlksVacuumed,
				IndexVacuumCount: t.IndexVacuumCount,
				MaxDeadTuples:    t.MaxDeadTuples,
				NumDeadTuples:    t.NumDeadTuples,
			}
		})

	return ret, nil
}

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
	limit := 50
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

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
	limit := 10
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

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

const defaultPgSettingsLimit = 30

func (s *Handlers) GetPgSettings(
	ctx context.Context,
	req serverhttp.GetPgSettingsRequestObject,
) (serverhttp.GetPgSettingsResponseObject, error) {
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultPgSettingsLimit)

	settings, err := s.repo.GetPgSettings(ctx, req.Params.ClusterName, req.Params.Instance, limit, offset)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetPgSettings404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetPgSettings | %w", err)
	}

	var ret serverhttp.GetPgSettings200JSONResponse = mapstruct.SliceMap(
		settings,
		func(t dto.PgSetting) serverhttp.PgSetting {
			return serverhttp.PgSetting{
				Name:    t.Name,
				Setting: t.Setting,
				Unit:    t.Unit,
				Source:  t.Source,
			}
		})

	return ret, nil
}

func (s *Handlers) GetAutovacuumSettings(
	ctx context.Context,
	req serverhttp.GetAutovacuumSettingsRequestObject,
) (serverhttp.GetAutovacuumSettingsResponseObject, error) {
	settings, err := s.repo.GetAutovacuumSettings(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetAutovacuumSettings404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetAutovacuumSettings | %w", err)
	}

	var ret serverhttp.GetAutovacuumSettings200JSONResponse = mapstruct.SliceMap(
		settings,
		func(t dto.PgSetting) serverhttp.PgSetting {
			return serverhttp.PgSetting{
				Name:    t.Name,
				Setting: t.Setting,
				Unit:    t.Unit,
				Source:  t.Source,
			}
		})

	return ret, nil
}

func (s *Handlers) GetSettingsAnalyze(
	ctx context.Context,
	req serverhttp.GetSettingsAnalyzeRequestObject,
) (serverhttp.GetSettingsAnalyzeResponseObject, error) {
	notifications, err := s.repo.GetSettingsAnalyze(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetSettingsAnalyze404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetSettingsAnalyze | %w", err)
	}

	var ret serverhttp.GetSettingsAnalyze200JSONResponse = mapstruct.SliceMap(
		notifications,
		func(t dto.SettingsNotification) serverhttp.SettingsNotification {
			return serverhttp.SettingsNotification{
				Key:    t.Key,
				Params: t.Params,
			}
		})

	return ret, nil
}

func (s *Handlers) GetReplicationStatus(
	ctx context.Context,
	req serverhttp.GetReplicationStatusRequestObject,
) (serverhttp.GetReplicationStatusResponseObject, error) {
	items, err := s.repo.GetReplicationStatus(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetReplicationStatus404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetReplicationStatus | %w", err)
	}

	var ret serverhttp.GetReplicationStatus200JSONResponse = mapstruct.SliceMap(
		items,
		func(t dto.ReplicationStatus) serverhttp.ReplicationStatus {
			return serverhttp.ReplicationStatus{
				Pid:              t.Pid,
				Usename:          shortcut.Ptr(t.Usename),
				ApplicationName:  t.ApplicationName,
				ClientAddr:       shortcut.Ptr(t.ClientAddr),
				State:            t.State,
				SentLsn:          shortcut.Ptr(t.SentLsn),
				WriteLsn:         shortcut.Ptr(t.WriteLsn),
				FlushLsn:         shortcut.Ptr(t.FlushLsn),
				ReplayLsn:        shortcut.Ptr(t.ReplayLsn),
				WriteLagSeconds:  shortcut.Ptr(t.WriteLagSeconds),
				FlushLagSeconds:  shortcut.Ptr(t.FlushLagSeconds),
				ReplayLagSeconds: shortcut.Ptr(t.ReplayLagSeconds),
				ReplayLagBytes:   shortcut.Ptr(t.ReplayLagBytes),
				SyncState:        t.SyncState,
				SlotName:         shortcut.Ptr(t.SlotName),
			}
		})

	return ret, nil
}

func (s *Handlers) GetReplicationSlots(
	ctx context.Context,
	req serverhttp.GetReplicationSlotsRequestObject,
) (serverhttp.GetReplicationSlotsResponseObject, error) {
	items, err := s.repo.GetReplicationSlots(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetReplicationSlots404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetReplicationSlots | %w", err)
	}

	var ret serverhttp.GetReplicationSlots200JSONResponse = mapstruct.SliceMap(
		items,
		func(t dto.ReplicationSlot) serverhttp.ReplicationSlot {
			r := serverhttp.ReplicationSlot{
				SlotName:  t.SlotName,
				SlotType:  t.SlotType,
				Active:    t.Active,
				Database:  shortcut.Ptr(t.Database),
				WalStatus: shortcut.Ptr(t.WalStatus),
			}

			if t.SafeWalSize != nil {
				r.SafeWalSize = t.SafeWalSize
			}

			if t.BacklogBytes != nil {
				r.BacklogBytes = t.BacklogBytes
			}

			return r
		})

	return ret, nil
}

func (s *Handlers) GetReplicationConfig(
	ctx context.Context,
	req serverhttp.GetReplicationConfigRequestObject,
) (serverhttp.GetReplicationConfigResponseObject, error) {
	cfg, err := s.repo.GetReplicationConfig(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetReplicationConfig404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetReplicationConfig | %w", err)
	}

	ret := serverhttp.GetReplicationConfig200JSONResponse{
		SynchronousStandbyNames: cfg.SynchronousStandbyNames,
		SynchronousCommit:       cfg.SynchronousCommit,
	}

	return ret, nil
}

func (s *Handlers) GetDatabaseHealth(
	ctx context.Context,
	req serverhttp.GetDatabaseHealthRequestObject,
) (serverhttp.GetDatabaseHealthResponseObject, error) {
	health, err := s.repo.GetDatabaseHealth(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetDatabaseHealth404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetDatabaseHealth | %w", err)
	}

	ret := serverhttp.GetDatabaseHealth200JSONResponse{
		Deadlocks:     health.Deadlocks,
		Conflicts:     health.Conflicts,
		XactCommit:    health.XactCommit,
		XactRollback:  health.XactRollback,
		RollbackRatio: health.RollbackRatio,
	}

	if health.ChecksumFailures != nil {
		ret.ChecksumFailures = health.ChecksumFailures
	}

	if health.ChecksumLastFailure != nil {
		s := health.ChecksumLastFailure.Format("2006-01-02T15:04:05Z07:00")
		ret.ChecksumLastFailure = &s
	}

	if health.StatsReset != nil {
		s := health.StatsReset.Format("2006-01-02T15:04:05Z07:00")
		ret.StatsReset = &s
	}

	return ret, nil
}

func (s *Handlers) GetConnectionWaitEvents(
	ctx context.Context,
	req serverhttp.GetConnectionWaitEventsRequestObject,
) (serverhttp.GetConnectionWaitEventsResponseObject, error) {
	items, err := s.repo.GetConnectionWaitEvents(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetConnectionWaitEvents404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetConnectionWaitEvents | %w", err)
	}

	var ret serverhttp.GetConnectionWaitEvents200JSONResponse = mapstruct.SliceMap(
		items,
		func(t dto.WaitEvent) serverhttp.WaitEvent {
			return serverhttp.WaitEvent{
				WaitEventType: t.WaitEventType,
				WaitEvent:     t.WaitEvent,
				Count:         t.Count,
			}
		})

	return ret, nil
}
