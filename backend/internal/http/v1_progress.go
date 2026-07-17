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
				Pid:               t.Pid,
				Datname:           t.Datname,
				TableName:         t.TableName,
				Phase:             t.Phase,
				HeapBlksTotal:     t.HeapBlksTotal,
				HeapBlksScanned:   t.HeapBlksScanned,
				HeapBlksVacuumed:  t.HeapBlksVacuumed,
				IndexVacuumCount:  t.IndexVacuumCount,
				MaxDeadTuples:     t.MaxDeadTuples,
				NumDeadTuples:     t.NumDeadTuples,
				DeadTupleBytes:    t.DeadTupleBytes,
				MaxDeadTupleBytes: t.MaxDeadTupleBytes,
			}
		})

	return ret, nil
}
