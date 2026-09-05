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

// extensionTemplateData names one extension for the status probes.
type extensionTemplateData struct {
	Extension string
}

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

	src := p.statsSource(ctx, pool)

	available, name, err := p.getQueryStatsAvailable(ctx, vNum, pool, src)
	if err != nil {
		p.logger.Warn("failed to get query stats available", zap.Error(err))
	}

	enabled, err := p.getQueryStatsEnabled(ctx, vNum, pool, name)
	if err != nil {
		p.logger.Warn("failed to get query stats enabled", zap.Error(err))
	}

	readable, err := p.getQueryStatsReadable(ctx, vNum, pool)
	if err != nil {
		p.logger.Warn("failed to get query stats readable", zap.Error(err))
	}

	restricted, err := p.getQueryStatsRestricted(ctx, vNum, pool)
	if err != nil {
		p.logger.Warn("failed to get query stats restricted", zap.Error(err))
	}

	return dto.QueryStatsStatus{
		Available:  available,
		Enabled:    enabled,
		Readable:   readable,
		Restricted: restricted,
		Source:     name,
	}, nil
}

// getQueryStatsAvailable reports whether the extension can be installed here and
// which one the messages should name. Absent a resolved source the candidates are
// tried in recommendation order.
func (p *PgxPool) getQueryStatsAvailable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	src statsSource,
) (bool, string, error) {
	if src.Present() {
		available, err := p.probeExtensionAvailable(ctx, serverVersion, pool, src.Name())

		return available, src.Name(), err
	}

	var probeErr error

	for _, def := range recommendedSourceOrder {
		available, err := p.probeExtensionAvailable(ctx, serverVersion, pool, def.Ext)
		if err != nil {
			probeErr = errors.Join(probeErr, err)

			continue
		}

		if available {
			return true, def.Ext, nil
		}
	}

	return false, src.Name(), probeErr
}

func (p *PgxPool) probeExtensionAvailable(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	extension string,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsAvailable, extensionTemplateData{Extension: extension})
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
	extension string,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsEnabled, extensionTemplateData{Extension: extension})
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

func (p *PgxPool) getQueryStatsRestricted(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryCommonQueryStatsRestricted, nil)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsRestricted | %w", err)
	}

	var b bool

	err = pool.QueryRow(ctx, qStr).Scan(&b)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsRestricted | %w", err)
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
		// Only an answer from the server settles the question; a dead connection
		// or an expired deadline is not one.
		var pgErr *pgconn.PgError
		if IsTimeout(err) || !errors.As(err, &pgErr) {
			return false, fmt.Errorf("getQueryStatsReadable | %w", err)
		}

		// The reason (privileges, custom schema) is visible only here.
		p.logger.Debug("query statistics view is not readable", zap.Error(err))

		return false, nil
	}

	return true, nil
}
