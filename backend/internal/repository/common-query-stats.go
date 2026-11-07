package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

	readable, err := p.getQueryStatsReadable(ctx, pool)
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

type statsReadableKey struct {
	pool     *pgxpool.Pool
	relation string
}

type statsReadableEntry struct {
	readable bool
	expires  time.Time
}

const statsReadableHitTTL = 5 * time.Minute

// Statistics queries fail with these codes when the extension is absent, not
// preloaded or closed to the role.
var statsUnavailableCodes = map[string]struct{}{
	undefinedTableCode:               {},
	invalidSchemaNameCode:            {},
	insufficientPrivilegeCode:        {},
	undefinedFunctionCode:            {},
	objectNotInPrerequisiteStateCode: {},
}

func isStatsUnavailable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	_, ok := statsUnavailableCodes[pgErr.Code]

	return ok
}

func (p *PgxPool) getQueryStatsReadable(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	src := p.statsSource(ctx, pool)
	key := statsReadableKey{pool: pool, relation: src.Relation()}

	if v, ok := p.statsReadable.Load(key); ok {
		if e, valid := v.(statsReadableEntry); valid && time.Now().Before(e.expires) {
			return e.readable, nil
		}
	}

	readable, err := p.probeStatsReadable(ctx, pool, src)
	if err != nil {
		return false, fmt.Errorf("getQueryStatsReadable | %w", err)
	}

	p.storeStatsReadable(key, readable)

	return readable, nil
}

func (p *PgxPool) storeStatsReadable(key statsReadableKey, readable bool) {
	ttl := extSchemaMissTTL
	if readable {
		ttl = statsReadableHitTTL
	}

	p.statsReadable.Store(key, statsReadableEntry{readable: readable, expires: time.Now().Add(ttl)})
}

// statsUnavailable reports whether a failed statistics query is explained by
// statistics the role cannot read, as confirmed by a fresh probe.
func (p *PgxPool) statsUnavailable(ctx context.Context, pool *pgxpool.Pool, err error) bool {
	if !isStatsUnavailable(err) {
		return false
	}

	src := p.statsSource(ctx, pool)

	readable, probeErr := p.probeStatsReadable(ctx, pool, src)
	if probeErr != nil || readable {
		return false
	}

	p.logger.Debug("query statistics are not readable", zap.Error(err))
	p.storeStatsReadable(statsReadableKey{pool: pool, relation: src.Relation()}, false)

	return true
}

// probeStatsReadable treats a dead connection or an expired deadline as an error.
func (p *PgxPool) probeStatsReadable(ctx context.Context, pool *pgxpool.Pool, src statsSource) (bool, error) {
	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return false, err
	}

	preloadedQ, err := query.Get(vNum, enums.QueryCommonQueryStatsPreloaded, statsPreloadedTemplateData{
		StatsPreloaded: query.StatsPreloaded(1),
	})
	if err != nil {
		return false, err
	}

	readableQ, err := query.Get(vNum, enums.QueryCommonQueryStatsReadable, pgssTemplateData{
		Pgss:     src.Relation(),
		PgssInfo: src.InfoRelation(),
	})
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	batch := &pgx.Batch{}
	batch.Queue(preloadedQ, []string{src.MaxSetting()})
	batch.Queue(readableQ)

	br := pool.SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()

	var loaded bool
	if err := br.QueryRow().Scan(&loaded); err != nil {
		return false, err
	}

	if !loaded {
		p.logger.Debug("query statistics library is not preloaded", zap.String("extension", src.Name()))

		return false, nil
	}

	if _, err := br.Exec(); err != nil {
		var pgErr *pgconn.PgError
		if IsTimeout(err) || !errors.As(err, &pgErr) {
			return false, err
		}

		// The reason (privileges, custom schema) is visible only here.
		p.logger.Debug("query statistics view is not readable",
			zap.String("extension", src.Name()), zap.Error(err))

		return false, nil
	}

	return true, nil
}
