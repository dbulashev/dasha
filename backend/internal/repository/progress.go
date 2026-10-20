package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
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

func (p *PgxPool) GetProgressCopy(ctx context.Context, clusterName, instanceName string) ([]dto.ProgressCopy, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetProgressCopy | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getProgressCopy(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getProgressCopy | %w", err)
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
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

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
			datname, tableName, currentChildTable                               string
			phase                                                               pgtype.Text
			sampleBlksTotal, sampleBlksScanned, extStatsTotal, extStatsComputed int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &phase,
			&sampleBlksTotal, &sampleBlksScanned, &extStatsTotal, &extStatsComputed, &currentChildTable)
		if err != nil {
			return nil, fmt.Errorf("getProgressAnalyze | %w", err)
		}

		ret = append(ret, dto.ProgressAnalyze{
			Pid:               pid,
			Datname:           datname,
			TableName:         tableName,
			Phase:             phase.String,
			SampleBlksTotal:   sampleBlksTotal,
			SampleBlksScanned: sampleBlksScanned,
			ExtStatsTotal:     extStatsTotal,
			ExtStatsComputed:  extStatsComputed,
			CurrentChildTable: currentChildTable,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getProgressAnalyze | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getProgressBaseBackup(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressBaseBackup, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

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
			phase                                 pgtype.Text
			backupTotal, backupStreamed           int64
			progressPercentage                    pgtype.Float8
			tablespacesTotal, tablespacesStreamed int64
		)

		err = rows.Scan(&pid, &phase, &backupTotal, &backupStreamed,
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
			Phase:               phase.String,
			BackupTotal:         backupTotal,
			BackupStreamed:      backupStreamed,
			ProgressPercentage:  pct,
			TablespacesTotal:    tablespacesTotal,
			TablespacesStreamed: tablespacesStreamed,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getProgressBaseBackup | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getProgressCluster(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressCluster, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

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
			datname, tableName, command, clusterIndex                                               string
			phase                                                                                   pgtype.Text
			heapTuplesScanned, heapTuplesWritten, heapBlksTotal, heapBlksScanned, indexRebuildCount int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &command, &phase,
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
			Phase:             phase.String,
			ClusterIndex:      clusterIndex,
			HeapTuplesScanned: heapTuplesScanned,
			HeapTuplesWritten: heapTuplesWritten,
			HeapBlksTotal:     heapBlksTotal,
			HeapBlksScanned:   heapBlksScanned,
			IndexRebuildCount: indexRebuildCount,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getProgressCluster | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getProgressCopy(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressCopy, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryProgressCopy, nil)
	if err != nil {
		return nil, fmt.Errorf("getProgressCopy | %w", err)
	}

	var ret []dto.ProgressCopy

	// pg_total_relation_size waits on an AccessExclusiveLock taken after the pg_locks check.
	err = p.withLockTimeout(ctx, pool, func(q querier) error {
		rows, err := q.Query(ctx, qStr)
		if err != nil {
			return err
		}

		ret, err = scanProgressCopy(rows)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("getProgressCopy | %w", err)
	}

	return ret, nil
}

func scanProgressCopy(rows pgx.Rows) ([]dto.ProgressCopy, error) {
	defer rows.Close()

	ret := make([]dto.ProgressCopy, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			pid                                             int32
			datname, command, copyType                      pgtype.Text
			tableName, queryText                            pgtype.Text
			bytesProcessed, tuplesProcessed, tuplesExcluded int64
			bytesTotal, tuplesSkipped                       pgtype.Int8
			tableSize, tableTuples                          pgtype.Int8
			elapsedSeconds, progressPercentage, etaSeconds  pgtype.Float8
		)

		err := rows.Scan(&pid, &datname, &tableName, &command, &copyType, &queryText,
			&bytesProcessed, &bytesTotal, &tuplesProcessed, &tuplesExcluded, &tuplesSkipped,
			&tableSize, &tableTuples, &elapsedSeconds, &progressPercentage, &etaSeconds)
		if err != nil {
			return nil, fmt.Errorf("scan | %w", err)
		}

		row := dto.ProgressCopy{
			Pid:             pid,
			Datname:         datname.String,
			Command:         command.String,
			Type:            copyType.String,
			BytesProcessed:  bytesProcessed,
			TuplesProcessed: tuplesProcessed,
			TuplesExcluded:  tuplesExcluded,
		}

		if tableName.Valid {
			row.TableName = &tableName.String
		}

		if queryText.Valid {
			row.Query = &queryText.String
		}

		if bytesTotal.Valid {
			row.BytesTotal = &bytesTotal.Int64
		}

		if tuplesSkipped.Valid {
			row.TuplesSkipped = &tuplesSkipped.Int64
		}

		if tableSize.Valid {
			row.TableSize = &tableSize.Int64
		}

		if tableTuples.Valid {
			row.TableTuples = &tableTuples.Int64
		}

		if elapsedSeconds.Valid {
			row.ElapsedSeconds = &elapsedSeconds.Float64
		}

		if progressPercentage.Valid {
			row.ProgressPercentage = &progressPercentage.Float64
		}

		if etaSeconds.Valid {
			row.EtaSeconds = &etaSeconds.Float64
		}

		ret = append(ret, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getProgressIndex(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressIndex, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

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
			datname, tableName, indexName                                                                                string
			phase                                                                                                        pgtype.Text
			lockersTotal, lockersDone, blocksTotal, blocksDone, tuplesTotal, tuplesDone, partitionsTotal, partitionsDone int64
		)

		err = rows.Scan(&pid, &datname, &tableName, &indexName, &phase,
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
			Phase:            phase.String,
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getProgressIndex | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getProgressVacuum(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.ProgressVacuum, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

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
			pid                                                                int32
			datname, tableName                                                 string
			phase                                                              pgtype.Text
			heapBlksTotal, heapBlksScanned, heapBlksVacuumed, indexVacuumCount int64
			numDeadTuples                                                      int64
			maxDeadTuples, deadTupleBytes, maxDeadTupleBytes                   pgtype.Int8
		)

		err = rows.Scan(&pid, &datname, &tableName, &phase,
			&heapBlksTotal, &heapBlksScanned, &heapBlksVacuumed,
			&indexVacuumCount, &maxDeadTuples, &numDeadTuples,
			&deadTupleBytes, &maxDeadTupleBytes)
		if err != nil {
			return nil, fmt.Errorf("getProgressVacuum | %w", err)
		}

		row := dto.ProgressVacuum{
			Pid:              pid,
			Datname:          datname,
			TableName:        tableName,
			Phase:            phase.String,
			HeapBlksTotal:    heapBlksTotal,
			HeapBlksScanned:  heapBlksScanned,
			HeapBlksVacuumed: heapBlksVacuumed,
			IndexVacuumCount: indexVacuumCount,
			NumDeadTuples:    numDeadTuples,
		}

		if maxDeadTuples.Valid {
			row.MaxDeadTuples = &maxDeadTuples.Int64
		}

		if deadTupleBytes.Valid {
			row.DeadTupleBytes = &deadTupleBytes.Int64
		}

		if maxDeadTupleBytes.Valid {
			row.MaxDeadTupleBytes = &maxDeadTupleBytes.Int64
		}

		ret = append(ret, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getProgressVacuum | %w", err)
	}

	return ret, nil
}
