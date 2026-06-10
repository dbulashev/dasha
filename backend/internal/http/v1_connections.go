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
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultConnectionStatActivityLimit)

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
