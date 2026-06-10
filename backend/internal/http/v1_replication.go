package http

import (
	"context"
	"errors"
	"fmt"

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
