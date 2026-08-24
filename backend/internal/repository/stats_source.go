package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// statsSourceDef names the objects one query-statistics extension provides.
type statsSourceDef struct {
	Ext   string
	View  string
	Info  string
	Reset string
}

// Ordered by priority: with both extensions installed and readable, the first wins.
var statsSourceDefs = []statsSourceDef{
	{
		Ext:   "pg_stat_statements",
		View:  "pg_stat_statements",
		Info:  "pg_stat_statements_info",
		Reset: "pg_stat_statements_reset",
	},
	{
		Ext:   "pgpro_stats",
		View:  "pgpro_stats_statements",
		Info:  "pgpro_stats_info",
		Reset: "pgpro_stats_statements_reset",
	},
}

// Order the status probes suggest an extension to install. Reversed on purpose:
// pgpro_stats in pg_available_extensions identifies Postgres Pro.
var recommendedSourceOrder = []statsSourceDef{statsSourceDefs[1], statsSourceDefs[0]}

// statsSource is the query-statistics extension one pool is read through. The
// zero value answers with the bare pg_stat_statements names.
type statsSource struct {
	def    statsSourceDef
	schema string // quoted; "" when not installed
	found  bool
}

func (s statsSource) Present() bool { return s.found }

// Name is the extension shown to the user and stored as snapshot provenance.
func (s statsSource) Name() string { return s.definition().Ext }

func (s statsSource) Relation() string { return qualify(s.schema, s.definition().View) }

func (s statsSource) InfoRelation() string { return qualify(s.schema, s.definition().Info) }

func (s statsSource) ResetFunc() string { return qualify(s.schema, s.definition().Reset) }

func (s statsSource) definition() statsSourceDef {
	if !s.found {
		return statsSourceDefs[0]
	}

	return s.def
}

// Both candidates in one round trip: the conflict rule needs both answers.
const statsSourceQuery = `
SELECT e.extname, n.nspname, e.extversion
FROM pg_catalog.pg_extension e
JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
WHERE e.extname::text = ANY($1)`

type catalogExt struct {
	schema  string // quoted
	version string
}

// Cached under the TTL rules of extSchemaEntry: a miss briefly, so CREATE
// EXTENSION needs no restart; a hit for long.
type statsSourceEntry struct {
	source  statsSource
	expires time.Time
}

// statsSourceResolver is the resolution logic with its I/O pulled out.
type statsSourceResolver struct {
	catalog  func(ctx context.Context) (map[string]catalogExt, error)
	readable func(ctx context.Context, relation string) (bool, error)
	now      func() time.Time
	cache    *sync.Map
	key      any
	logger   *zap.Logger
}

func (r statsSourceResolver) resolve(ctx context.Context) statsSource {
	if v, ok := r.cache.Load(r.key); ok {
		entry, _ := v.(statsSourceEntry)
		if r.now().Before(entry.expires) {
			return entry.source
		}
	}

	source, ttl := r.choose(ctx)
	r.cache.Store(r.key, statsSourceEntry{source: source, expires: r.now().Add(ttl)})

	return source
}

// choose picks the source and says how long the answer may be trusted.
func (r statsSourceResolver) choose(ctx context.Context) (statsSource, time.Duration) {
	installed, err := r.catalog(ctx)
	if err != nil {
		r.logger.Warn("query statistics source lookup failed", zap.Error(err))

		return statsSource{}, extSchemaMissTTL //nolint:exhaustruct
	}

	var candidates []statsSource

	for _, def := range statsSourceDefs {
		ext, ok := installed[def.Ext]
		if !ok {
			continue
		}

		candidates = append(candidates, statsSource{def: def, schema: ext.schema, found: true})

		r.logger.Debug("query statistics extension found",
			zap.String("extension", def.Ext),
			zap.String("schema", ext.schema),
			zap.String("version", ext.version))
	}

	switch len(candidates) {
	case 0:
		return statsSource{}, extSchemaMissTTL //nolint:exhaustruct
	case 1:
		return candidates[0], extSchemaHitTTL
	}

	return r.resolveConflict(ctx, candidates)
}

// resolveConflict settles the case both are in the catalog: pg_stat_statements
// can be created while its library is not loaded, and only a read tells them apart.
func (r statsSourceResolver) resolveConflict(ctx context.Context, candidates []statsSource) (statsSource, time.Duration) {
	for _, candidate := range candidates {
		ok, err := r.readable(ctx, candidate.Relation())
		if err != nil {
			// No answer from the server: hold the fallback on the short TTL.
			r.logger.Warn("query statistics source probe failed",
				zap.String("extension", candidate.Name()), zap.Error(err))

			return candidates[0], extSchemaMissTTL
		}

		if ok {
			return candidate, extSchemaHitTTL
		}

		r.logger.Debug("query statistics extension is installed but not readable",
			zap.String("extension", candidate.Name()))
	}

	return candidates[0], extSchemaHitTTL
}

// Resolved per pool, not per host: pg_extension is a per-database catalog.
func (p *PgxPool) statsSource(ctx context.Context, pool *pgxpool.Pool) statsSource {
	return p.statsSourceResolver(pool).resolve(ctx)
}

func (p *PgxPool) statsSourceResolver(pool *pgxpool.Pool) statsSourceResolver {
	return statsSourceResolver{
		catalog: func(ctx context.Context) (map[string]catalogExt, error) { return p.statsSourceCatalog(ctx, pool) },
		readable: func(ctx context.Context, relation string) (bool, error) {
			return p.statsSourceReadable(ctx, pool, relation)
		},
		now:    time.Now,
		cache:  &p.resolvedStatsSources,
		key:    pool,
		logger: p.logger,
	}
}

func (p *PgxPool) statsSourceCatalog(ctx context.Context, pool *pgxpool.Pool) (map[string]catalogExt, error) {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	names := make([]string, 0, len(statsSourceDefs))
	for _, def := range statsSourceDefs {
		names = append(names, def.Ext)
	}

	rows, err := pool.Query(queryCtx, statsSourceQuery, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	installed := make(map[string]catalogExt, len(statsSourceDefs))

	for rows.Next() {
		var name, schema, version string
		if err := rows.Scan(&name, &schema, &version); err != nil {
			return nil, err
		}

		installed[name] = catalogExt{schema: pgx.Identifier{schema}.Sanitize(), version: version}
	}

	return installed, rows.Err()
}

// Only an answer from the server settles readability; a dead connection or an
// expired deadline is returned as an error instead.
func (p *PgxPool) statsSourceReadable(ctx context.Context, pool *pgxpool.Pool, relation string) (bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, err := pool.Exec(queryCtx, "SELECT 1 FROM "+relation+" LIMIT 1")
	if err == nil {
		return true, nil
	}

	var pgErr *pgconn.PgError
	if IsTimeout(err) || !errors.As(err, &pgErr) {
		return false, err
	}

	return false, nil
}
