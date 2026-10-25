package http

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
)

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
			return serverhttp.ReplicationSlot{
				SlotName:     t.SlotName,
				SlotType:     t.SlotType,
				Active:       t.Active,
				Database:     shortcut.Ptr(t.Database),
				WalStatus:    shortcut.Ptr(t.WalStatus),
				SafeWalSize:  t.SafeWalSize,
				BacklogBytes: t.BacklogBytes,
			}
		})

	return ret, nil
}

var lsnRe = regexp.MustCompile(`^[0-9A-Fa-f]{1,8}/[0-9A-Fa-f]{1,8}$`)

func (s *Handlers) GetReplicationWalPosition(
	ctx context.Context,
	req serverhttp.GetReplicationWalPositionRequestObject,
) (serverhttp.GetReplicationWalPositionResponseObject, error) {
	pos, err := s.repo.GetReplicationWalPosition(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetReplicationWalPosition404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetReplicationWalPosition | %w", err)
	}

	return serverhttp.GetReplicationWalPosition200JSONResponse{
		Lsn:        pos.Lsn,
		CapturedAt: pos.CapturedAt,
	}, nil
}

func (s *Handlers) GetReplicationSlotsForecast(
	ctx context.Context,
	req serverhttp.GetReplicationSlotsForecastRequestObject,
) (serverhttp.GetReplicationSlotsForecastResponseObject, error) {
	if !lsnRe.MatchString(req.Params.FromLsn) {
		return serverhttp.GetReplicationSlotsForecast400Response{}, nil
	}

	fc, err := s.repo.GetReplicationSlotsForecast(ctx, req.Params.ClusterName, req.Params.Instance,
		req.Params.FromLsn, req.Params.FromTime)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetReplicationSlotsForecast404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetReplicationSlotsForecast | %w", err)
	}

	return serverhttp.GetReplicationSlotsForecast200JSONResponse{
		ElapsedSeconds:     fc.ElapsedSeconds,
		WalBytesPerSecond:  fc.WalBytesPerSecond,
		MaxSlotWalKeepSize: fc.MaxSlotWalKeepSize,
		Slots: mapstruct.SliceMap(fc.Slots, func(t dto.ReplicationSlotForecast) serverhttp.ReplicationSlotForecast {
			return serverhttp.ReplicationSlotForecast{
				SlotName:      t.SlotName,
				SlotType:      t.SlotType,
				Database:      t.Database,
				Active:        t.Active,
				WalStatus:     t.WalStatus,
				RetainedBytes: t.RetainedBytes,
				SafeWalSize:   t.SafeWalSize,
				SecondsToLost: t.SecondsToLost,
			}
		}),
	}, nil
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
