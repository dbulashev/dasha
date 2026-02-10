package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetProgressAnalyze(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressAnalyze, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetProgressAnalyze | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getProgressAnalyze(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getProgressAnalyze | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetProgressBaseBackup(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressBaseBackup, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetProgressBaseBackup | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getProgressBaseBackup(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getProgressBaseBackup | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetProgressCluster(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressCluster, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetProgressCluster | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getProgressCluster(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getProgressCluster | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetProgressIndex(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressIndex, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetProgressIndex | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getProgressIndex(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getProgressIndex | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetProgressVacuum(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressVacuum, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetProgressVacuum | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getProgressVacuum(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getProgressVacuum | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getProgressAnalyze(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressAnalyze, error) {
	qStr, err := query.Get(serverVersion, enums.QueryProgressAnalyze, nil)
	if err != nil {
		return nil, fmt.Errorf("getProgressAnalyze | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getProgressAnalyze | %w", err)
	}

	ret := make([]dto.ProgressAnalyze, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                                                 int32
			datname, tableName, phaseDescription, currentChildTable             string
			sampleBlksTotal, sampleBlksScanned, extStatsTotal, extStatsComputed int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &phaseDescription,
			&sampleBlksTotal, &sampleBlksScanned, &extStatsTotal, &extStatsComputed, &currentChildTable)
		if err != nil {
			return nil, fmt.Errorf("getProgressAnalyze | %w", err)
		}

		ret = append(ret, dto.ProgressAnalyze{
			Pid:               pid,
			Datname:           datname,
			TableName:         tableName,
			PhaseDescription:  phaseDescription,
			SampleBlksTotal:   sampleBlksTotal,
			SampleBlksScanned: sampleBlksScanned,
			ExtStatsTotal:     extStatsTotal,
			ExtStatsComputed:  extStatsComputed,
			CurrentChildTable: currentChildTable,
		})
	}

	return ret, nil
}

func (p *PgxPool) getProgressBaseBackup(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressBaseBackup, error) {
	qStr, err := query.Get(serverVersion, enums.QueryProgressBaseBackup, nil)
	if err != nil {
		return nil, fmt.Errorf("getProgressBaseBackup | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getProgressBaseBackup | %w", err)
	}

	ret := make([]dto.ProgressBaseBackup, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                   int32
			phaseDescription                      string
			backupTotal, backupStreamed           int64
			progressPercentage                    pgtype.Float8
			tablespacesTotal, tablespacesStreamed int64
		)

		err = rows.Scan(&pid, &phaseDescription, &backupTotal, &backupStreamed,
			&progressPercentage, &tablespacesTotal, &tablespacesStreamed)
		if err != nil {
			return nil, fmt.Errorf("getProgressBaseBackup | %w", err)
		}

		var pct *float64
		if progressPercentage.Valid {
			pct = &progressPercentage.Float64
		}

		ret = append(ret, dto.ProgressBaseBackup{
			Pid:                 pid,
			PhaseDescription:    phaseDescription,
			BackupTotal:         backupTotal,
			BackupStreamed:      backupStreamed,
			ProgressPercentage:  pct,
			TablespacesTotal:    tablespacesTotal,
			TablespacesStreamed: tablespacesStreamed,
		})
	}

	return ret, nil
}

func (p *PgxPool) getProgressCluster(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressCluster, error) {
	qStr, err := query.Get(serverVersion, enums.QueryProgressCluster, nil)
	if err != nil {
		return nil, fmt.Errorf("getProgressCluster | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getProgressCluster | %w", err)
	}

	ret := make([]dto.ProgressCluster, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                                                                     int32
			datname, tableName, command, phaseDescription, clusterIndex                             string
			heapTuplesScanned, heapTuplesWritten, heapBlksTotal, heapBlksScanned, indexRebuildCount int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &command, &phaseDescription,
			&clusterIndex, &heapTuplesScanned, &heapTuplesWritten,
			&heapBlksTotal, &heapBlksScanned, &indexRebuildCount)
		if err != nil {
			return nil, fmt.Errorf("getProgressCluster | %w", err)
		}

		ret = append(ret, dto.ProgressCluster{
			Pid:               pid,
			Datname:           datname,
			TableName:         tableName,
			Command:           command,
			PhaseDescription:  phaseDescription,
			ClusterIndex:      clusterIndex,
			HeapTuplesScanned: heapTuplesScanned,
			HeapTuplesWritten: heapTuplesWritten,
			HeapBlksTotal:     heapBlksTotal,
			HeapBlksScanned:   heapBlksScanned,
			IndexRebuildCount: indexRebuildCount,
		})
	}

	return ret, nil
}

func (p *PgxPool) getProgressIndex(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressIndex, error) {
	qStr, err := query.Get(serverVersion, enums.QueryProgressIndex, nil)
	if err != nil {
		return nil, fmt.Errorf("getProgressIndex | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getProgressIndex | %w", err)
	}

	ret := make([]dto.ProgressIndex, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid, currentLockerPid                                                                                        int32
			datname, tableName, indexName, phaseDescription                                                              string
			lockersTotal, lockersDone, blocksTotal, blocksDone, tuplesTotal, tuplesDone, partitionsTotal, partitionsDone int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &indexName, &phaseDescription,
			&lockersTotal, &lockersDone, &currentLockerPid, &blocksTotal, &blocksDone,
			&tuplesTotal, &tuplesDone, &partitionsTotal, &partitionsDone)
		if err != nil {
			return nil, fmt.Errorf("getProgressIndex | %w", err)
		}

		ret = append(ret, dto.ProgressIndex{
			Pid:              pid,
			Datname:          datname,
			TableName:        tableName,
			IndexName:        indexName,
			PhaseDescription: phaseDescription,
			LockersTotal:     lockersTotal,
			LockersDone:      lockersDone,
			CurrentLockerPid: currentLockerPid,
			BlocksTotal:      blocksTotal,
			BlocksDone:       blocksDone,
			TuplesTotal:      tuplesTotal,
			TuplesDone:       tuplesDone,
			PartitionsTotal:  partitionsTotal,
			PartitionsDone:   partitionsDone,
		})
	}

	return ret, nil
}

func (p *PgxPool) getProgressVacuum(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressVacuum, error) {
	qStr, err := query.Get(serverVersion, enums.QueryProgressVacuum, nil)
	if err != nil {
		return nil, fmt.Errorf("getProgressVacuum | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getProgressVacuum | %w", err)
	}

	ret := make([]dto.ProgressVacuum, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                                                                              int32
			datname, tableName, phaseDescription                                                             string
			heapBlksTotal, heapBlksScanned, heapBlksVacuumed, indexVacuumCount, maxDeadTuples, numDeadTuples int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &phaseDescription,
			&heapBlksTotal, &heapBlksScanned, &heapBlksVacuumed,
			&indexVacuumCount, &maxDeadTuples, &numDeadTuples)
		if err != nil {
			return nil, fmt.Errorf("getProgressVacuum | %w", err)
		}

		ret = append(ret, dto.ProgressVacuum{
			Pid:              pid,
			Datname:          datname,
			TableName:        tableName,
			PhaseDescription: phaseDescription,
			HeapBlksTotal:    heapBlksTotal,
			HeapBlksScanned:  heapBlksScanned,
			HeapBlksVacuumed: heapBlksVacuumed,
			IndexVacuumCount: indexVacuumCount,
			MaxDeadTuples:    maxDeadTuples,
			NumDeadTuples:    numDeadTuples,
		})
	}

	return ret, nil
}
