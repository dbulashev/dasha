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
