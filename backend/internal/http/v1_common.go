package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetAuthInfo(
	ctx context.Context,
	_ serverhttp.GetAuthInfoRequestObject,
) (serverhttp.GetAuthInfoResponseObject, error) {
	mode := serverhttp.AuthInfoMode(s.cfg.Auth.Mode)
	if mode == "" {
		mode = serverhttp.None
	}

	enableReset := s.cfg.EnableQueryStatsReset
	// PATs belong to an individually-identifiable OIDC principal (shared static
	// tokens cannot mint them) and require the api_tokens table to be migrated
	// (else minting 500s and PAT auth fails closed). Only advertise the feature
	// when both hold, so the frontend does not offer an unusable dialog.
	patEnabled := s.cfg.Auth.Mode == config.AuthModeOIDC && s.storage.APITokensReady(ctx)
	resp := serverhttp.GetAuthInfo200JSONResponse{
		Mode:                  mode,
		EnableQueryStatsReset: &enableReset,
		PatEnabled:            &patEnabled,
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
			Name:         shortcut.Ptr(v.Name.String()),
			Source:       shortcut.Ptr(v.Source),
			SupportsLogs: shortcut.Ptr(v.SupportsLogs),
			Instances:    &instances,
			Databases:    &v.Databases,
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
