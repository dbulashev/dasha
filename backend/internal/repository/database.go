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

func (p *PgxPool) GetDatabaseHealth(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.DatabaseHealth, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetDatabaseHealth | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getDatabaseHealth(ctx, vNum, pool)
}

func (p *PgxPool) getDatabaseHealth(ctx context.Context, serverVersion int, pool *pgxpool.Pool) (*dto.DatabaseHealth, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryDatabaseHealth, nil)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseHealth | %w", err)
	}

	var (
		deadlocks           int64
		conflicts           int64
		checksumFailures    pgtype.Int8
		checksumLastFailure pgtype.Timestamptz
		xactCommit          int64
		xactRollback        int64
		rollbackRatio       float64
		statsReset          pgtype.Timestamptz
	)

	err = pool.QueryRow(ctx, qStr).Scan(
		&deadlocks, &conflicts, &checksumFailures, &checksumLastFailure,
		&xactCommit, &xactRollback, &rollbackRatio, &statsReset,
	)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseHealth | %w", err)
	}

	ret := &dto.DatabaseHealth{
		Deadlocks:     deadlocks,
		Conflicts:     conflicts,
		XactCommit:    xactCommit,
		XactRollback:  xactRollback,
		RollbackRatio: rollbackRatio,
	}

	if checksumFailures.Valid {
		ret.ChecksumFailures = &checksumFailures.Int64
	}

	if checksumLastFailure.Valid {
		ret.ChecksumLastFailure = &checksumLastFailure.Time
	}

	if statsReset.Valid {
		ret.StatsReset = &statsReset.Time
	}

	return ret, nil
}

func (p *PgxPool) GetStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.StatsResetTime, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetStatsResetTime | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getStatsResetTime(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getStatsResetTime(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.StatsResetTime, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryDatabaseStatsResetTime, nil)
	if err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	ret := make([]dto.StatsResetTime, 0, 10) //nolint:mnd

	for rows.Next() {
		var resetTime pgtype.Timestamptz

		err = rows.Scan(&resetTime)
		if err != nil {
			return nil, fmt.Errorf("getStatsResetTime | %w", err)
		}

		ret = append(ret, dto.StatsResetTime{
			Time: resetTime.Time,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getStatsResetTime | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetPgssStatsResetTime(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.StatsResetTime, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetPgssStatsResetTime | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	// pg_stat_statements_info available since PostgreSQL 14
	if vNum < 140000 { //nolint:mnd
		return nil, nil
	}

	return p.getPgssStatsResetTime(ctx, vNum, pool)
}

func (p *PgxPool) GetDatabaseSize(ctx context.Context, clusterName, instanceName, databaseName string) (*dto.DatabaseSize, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetDatabaseSize | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getDatabaseSize(ctx, vNum, pool)
}

func (p *PgxPool) getDatabaseSize(ctx context.Context, serverVersion int, pool *pgxpool.Pool) (*dto.DatabaseSize, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryDatabaseSize, nil)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseSize | %w", err)
	}

	var (
		sizeBytes  int64
		sizePretty string
	)

	err = pool.QueryRow(ctx, qStr).Scan(&sizeBytes, &sizePretty)
	if err != nil {
		return nil, fmt.Errorf("getDatabaseSize | %w", err)
	}

	return &dto.DatabaseSize{SizeBytes: sizeBytes, SizePretty: sizePretty}, nil
}

func (p *PgxPool) getPgssStatsResetTime(ctx context.Context, serverVersion int, pool *pgxpool.Pool) (*dto.StatsResetTime, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryDatabasePgssStatsResetTime, nil)
	if err != nil {
		return nil, fmt.Errorf("getPgssStatsResetTime | %w", err)
	}

	var resetTime pgtype.Timestamptz

	err = pool.QueryRow(ctx, qStr).Scan(&resetTime)
	if err != nil {
		return nil, fmt.Errorf("getPgssStatsResetTime | %w", err)
	}

	if !resetTime.Valid {
		return nil, nil
	}

	return &dto.StatsResetTime{Time: resetTime.Time}, nil
}
