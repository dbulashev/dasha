package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetQueryStatsStatus(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (dto.QueryStatsStatus, error) {
	// Same pool the statistics are read through, so the status describes what
	// the pages actually show: on a multi-database instance the extension may
	// live in another database, and reporting it as missing there would deny
	// data that is being displayed.
	pool, err := p.pgssPool(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return dto.QueryStatsStatus{}, fmt.Errorf("GetQueryStatsStatus | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return dto.QueryStatsStatus{}, fmt.Errorf("get server version | %w", err)
	}

	available, err := p.getQueryStatsAvailable(ctx, vNum, pool)
	if err != nil {
		p.logger.Warn("failed to get query stats available", zap.Error(err))
	}

	enabled, err := p.getQueryStatsEnabled(ctx, vNum, pool)
	if err != nil {
		p.logger.Warn("failed to get query stats enabled", zap.Error(err))
	}

	readable, err := p.getQueryStatsReadable(ctx, vNum, pool)
	if err != nil {
		p.logger.Warn("failed to get query stats readable", zap.Error(err))
	}

	return dto.QueryStatsStatus{
		Available: available,
		Enabled:   enabled,
		Readable:  readable,
	}, nil
}

func (p *PgxPool) getQueryStatsAvailable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsAvailable, nil)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsAvailable | %w", err)
	}

	var b bool

	err = pool.QueryRow(ctx, qStr).Scan(&b)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsAvailable | %w", err)
	}

	return b, nil
}

func (p *PgxPool) getQueryStatsEnabled(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsEnabled, nil)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsEnabled | %w", err)
	}

	var b bool

	err = pool.QueryRow(ctx, qStr).Scan(&b)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsEnabled | %w", err)
	}

	return b, nil
}

func (p *PgxPool) getQueryStatsReadable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsReadable, p.pgssTemplateData(ctx, pool))
	if err != nil {
		return false, fmt.Errorf("getQueryStatsReadable | %w", err)
	}

	_, err = pool.Exec(ctx, qStr)
	if err != nil {
		// Only an answer from the server settles the question. A connection that
		// died or a deadline that fired says nothing about the extension, and
		// reporting that as "not readable" would state as fact something this
		// probe never learned.
		var pgErr *pgconn.PgError
		if IsTimeout(err) || !errors.As(err, &pgErr) {
			return false, fmt.Errorf("getQueryStatsReadable | %w", err)
		}

		// Not an error for the caller — the UI just reports "not readable" —
		// but the actual reason (privileges, custom schema) is only visible here.
		p.logger.Debug("pg_stat_statements is not readable", zap.Error(err))

		return false, nil
	}

	return true, nil
}
