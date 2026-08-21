package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/indexadvisor"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/sqlparse"
)

const (
	// indexAdvisorQueryTimeout bounds one workload or catalog query.
	indexAdvisorQueryTimeout = 15 * time.Second
	// indexAdvisorMaxCatalogRows caps each catalog query. Generous, because the
	// cap is not a page size: a partly read catalog is a wrong catalog, and the
	// candidates built on it are wrong in the direction that hurts — a missed
	// index makes a candidate duplicating it look new.
	indexAdvisorMaxCatalogRows = 100000
)

// GetIndexAdvisorReport assembles the index candidate report for one database
// across EVERY host of the cluster.
//
// It takes no instance on purpose. pg_stat_statements is per-instance and is not
// replicated, so a statement that never runs on the primary can be the entire
// read workload of a replica — and asking only the primary would answer "this
// database needs no index" about a load it never saw. Indexes, by contrast, are
// physically replicated: one CREATE INDEX on the primary serves every host, so
// the candidate list is rightly built from the cluster's load as a whole.
//
// Never cached: the page is opened deliberately, usually right after changing
// something, and a stale answer there is worse than the work it saves. What is
// cached is the parse of each statement, which is where the time actually goes.
func (p *PgxPool) GetIndexAdvisorReport(
	ctx context.Context,
	clusterName, databaseName string,
	excludeUsers []string,
) (indexadvisor.Report, error) {
	// Build times only itself; the cost is the catalog reads and the parsing here.
	started := time.Now()

	cfg := p.indexAdvisorConfig.WithDefaults()

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	hostPools, err := p.getHostPoolsByClusterAndDatabase(ctx, clusterName, databaseName)
	if err != nil {
		return indexadvisor.Report{}, fmt.Errorf("GetIndexAdvisorReport | %w", err)
	}

	workload, reached := p.collectIndexAdvisorClusterWorkload(ctx, hostPools, excludeUsers)

	// With nothing to judge, reading the catalog would be a full scan of it for an
	// answer that is already known — but the report is still built, because the
	// reasons the workload yielded nothing are the whole point of it.
	if len(workload.Entries) == 0 {
		rep := indexadvisor.Build(workload, indexadvisor.NewCatalog(), p.indexAdvisorConfig)
		rep.DurationMs = time.Since(started).Milliseconds()

		return rep, nil
	}

	cat, err := p.collectIndexAdvisorClusterCatalog(ctx, reached)
	if err != nil {
		return indexadvisor.Report{}, fmt.Errorf("GetIndexAdvisorReport | %w", err)
	}

	rep := indexadvisor.Build(workload, cat, p.indexAdvisorConfig)
	rep.DurationMs = time.Since(started).Milliseconds()

	return rep, nil
}

// indexAdvisorHost is a host that answered, kept with the version its queries
// were built for so the catalog pass does not ask twice.
type indexAdvisorHost struct {
	host string
	pool *pgxpool.Pool
	vNum int
}

// collectIndexAdvisorClusterWorkload reads every host in parallel and folds the
// results into one workload.
//
// A host that fails is REPORTED, not skipped, and the report carries the list:
// the candidates it would have produced are missing from an answer that otherwise
// looks complete, and only the caller can decide whether to act on a partial one.
// Hosts are read concurrently because they are independent and the slowest one
// otherwise sets the latency of the whole report.
func (p *PgxPool) collectIndexAdvisorClusterWorkload(
	ctx context.Context,
	hostPools []hostPool,
	excludeUsers []string,
) (indexadvisor.Workload, []indexAdvisorHost) {
	type hostResult struct {
		host     string
		pool     *pgxpool.Pool
		vNum     int
		workload indexadvisor.Workload
		err      error
	}

	resultsCh := make(chan hostResult, len(hostPools))

	var wg sync.WaitGroup

	for _, hp := range hostPools {
		wg.Add(1)

		go func() {
			defer wg.Done()

			vNum, err := p.getServerVersionNum(ctx, hp.Pool)
			if err != nil {
				resultsCh <- hostResult{host: hp.Host, err: err} //nolint:exhaustruct

				return
			}

			w, err := p.collectIndexAdvisorWorkload(ctx, hp.Pool, hp.Host, vNum, excludeUsers)
			resultsCh <- hostResult{host: hp.Host, pool: hp.Pool, vNum: vNum, workload: w, err: err}
		}()
	}

	wg.Wait()
	close(resultsCh)

	var (
		out     indexadvisor.Workload
		reached []indexAdvisorHost
	)

	for r := range resultsCh {
		if r.err != nil {
			p.logger.Warn("index advisor workload on host",
				zap.String("host", r.host), zap.Error(r.err))

			out.Unreachable = append(out.Unreachable, r.host)

			continue
		}

		out.Merge(r.workload)
		reached = append(reached, indexAdvisorHost{host: r.host, pool: r.pool, vNum: r.vNum})
	}

	// Goroutines finish in any order, and the catalog host is picked by position.
	sort.Slice(reached, func(i, j int) bool { return reached[i].host < reached[j].host })

	return out, reached
}

// collectIndexAdvisorClusterCatalog reads the schema once and the activity
// counters everywhere.
//
// The split follows what replication does: relations, columns and indexes are
// byte-identical on a physical replica, so reading them on more than one host
// would cost N catalog scans for one answer. pg_stat_user_tables is the opposite
// — it is per-instance and not replicated, and a table read entirely on a replica
// shows no scans at all on the primary — so its counters are summed over every
// host that answered.
func (p *PgxPool) collectIndexAdvisorClusterCatalog(
	ctx context.Context,
	hosts []indexAdvisorHost,
) (indexadvisor.Catalog, error) {
	cat, err := p.readIndexAdvisorSchemaFromAny(ctx, hosts)
	if err != nil {
		return cat, err
	}

	for _, h := range hosts {
		// Best effort: activity counters shape a warning, never whether a candidate
		// exists, so a host that will not answer them costs precision, not the report.
		if err := p.readIndexAdvisorWrites(ctx, h.pool, h.vNum, &cat); err != nil {
			p.logger.Warn("index advisor table activity on host",
				zap.String("host", h.host), zap.Error(err))
		}
	}

	return cat, nil
}

// readIndexAdvisorSchemaFromAny reads the structure from the first host able to
// answer. Every attempt fills a fresh catalog: a read that fails halfway leaves
// half a schema behind, and appending the next host's rows onto it would double
// every column and index the failed attempt did manage to read — which is the one
// way this report can invent a duplicate index out of nothing.
func (p *PgxPool) readIndexAdvisorSchemaFromAny(
	ctx context.Context,
	hosts []indexAdvisorHost,
) (indexadvisor.Catalog, error) {
	lastErr := ErrNotFound

	for _, h := range hosts {
		cat := indexadvisor.NewCatalog()

		if err := p.readIndexAdvisorSchema(ctx, h.pool, h.vNum, &cat); err != nil {
			lastErr = fmt.Errorf("host %s | %w", h.host, err)

			// The schema is the same on every host, so one failure is not the end
			// of the report — only every host failing is.
			p.logger.Warn("index advisor catalog on host",
				zap.String("host", h.host), zap.Error(err))

			continue
		}

		return cat, nil
	}

	return indexadvisor.NewCatalog(), fmt.Errorf("collectIndexAdvisorCatalog | %w", lastErr)
}

// indexAdvisorParser builds the SQL parser on first use, not at startup: it
// compiles a WASM module, which costs about a second and stays resident for the
// life of the process. An installation that never opens the page should not pay
// for it, and one that does pays once.
func (p *PgxPool) indexAdvisorParser() sqlparse.Parser {
	p.sqlParserOnce.Do(func() {
		cfg := p.indexAdvisorConfig.WithDefaults()

		p.sqlParser = sqlparse.New(sqlparse.Config{
			MaxQueryBytes: cfg.MaxQueryBytes,
			CacheSize:     cfg.ParseCacheSize,
		})
	})

	return p.sqlParser
}

// collectIndexAdvisorWorkload reads the top of pg_stat_statements on one host for
// the current database and parses each statement.
//
// A statement that cannot be parsed is counted by reason rather than dropped: an
// empty candidate list next to fifty unparsed statements is not the same answer
// as an empty candidate list next to none, and the report has to keep the two
// apart.
func (p *PgxPool) collectIndexAdvisorWorkload(
	ctx context.Context,
	pool *pgxpool.Pool,
	host string,
	vNum int,
	excludeUsers []string,
) (indexadvisor.Workload, error) {
	// pg_stat_statements missing or unreadable is a state, not a failure — the
	// same treatment the query pages give it. On a cluster it is also a state
	// worth naming: the host is up, and its load is simply invisible to us. A
	// probe that never reached the server is the other statement entirely, and
	// the error carries it up to be reported as an unreachable host.
	readable, err := p.getQueryStatsReadable(ctx, vNum, pool)
	if err != nil {
		return indexadvisor.Workload{}, fmt.Errorf("collectIndexAdvisorWorkload | %w", err)
	}

	if !readable {
		return indexadvisor.Workload{NoStats: []string{host}}, nil
	}

	rows, err := p.readIndexAdvisorWorkloadRows(ctx, pool, vNum, excludeUsers)
	if err != nil {
		return indexadvisor.Workload{}, err
	}

	// Parsing happens after the rows are read, not inside the cursor: half a
	// second of WASM work must not hold a connection of a pool the pages share.
	out := indexadvisor.Workload{Available: true, Collected: len(rows), Hosts: []string{host}}
	parser := p.indexAdvisorParser()

	for _, row := range rows {
		// The parser holds no context: it is a WASM module behind a semaphore, and
		// a report whose deadline has passed would otherwise keep parsing rows for
		// an answer nobody is waiting for — and hold the semaphore against the
		// reports that still are.
		if err := ctx.Err(); err != nil {
			return indexadvisor.Workload{}, fmt.Errorf("collectIndexAdvisorWorkload | %w", err)
		}

		stmt, err := parser.Parse(row.query)
		if err != nil {
			out.CountNotParsed(sqlparse.ReasonOf(err))

			continue
		}

		out.Entries = append(out.Entries, indexadvisor.WorkloadEntry{
			QueryIDs: []int64{row.queryID},
			// One row, one host: this is where the pairing is still known, and
			// folding by fingerprint is what would otherwise lose it.
			QueryIDByHost: map[string]int64{host: row.queryID},
			Fingerprint:   stmt.Fingerprint,
			Query:         sanitize.SQL(row.query),
			Calls:         row.calls,
			TotalTimeMs:   row.totalTimeMs,
			Rows:          row.rows,
			Stmt:          stmt,
			Hosts:         []string{host},
		})
	}

	return out, nil
}

type indexAdvisorWorkloadRow struct {
	queryID     int64
	query       string
	calls       int64
	totalTimeMs float64
	rows        int64
}

func (p *PgxPool) readIndexAdvisorWorkloadRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
	excludeUsers []string,
) ([]indexAdvisorWorkloadRow, error) {
	qStr, err := query.Get(vNum, enums.QueryIndexAdvisorWorkload, p.pgssTemplateData(ctx, pool))
	if err != nil {
		return nil, fmt.Errorf("collectIndexAdvisorWorkload | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, indexAdvisorQueryTimeout)
	defer cancel()

	if excludeUsers == nil {
		excludeUsers = []string{}
	}

	limit := p.indexAdvisorConfig.WithDefaults().MaxQueries

	pgRows, err := pool.Query(ctx, qStr, excludeUsers, limit)
	if err != nil {
		return nil, fmt.Errorf("collectIndexAdvisorWorkload | %w", err)
	}
	defer pgRows.Close()

	out := make([]indexAdvisorWorkloadRow, 0, limit)

	for pgRows.Next() {
		var (
			row       indexAdvisorWorkloadRow
			text      pgtype.Text
			calls     pgtype.Int8
			totalTime pgtype.Float8
			rowCount  pgtype.Int8
		)

		if err := pgRows.Scan(&row.queryID, &text, &calls, &totalTime, &rowCount); err != nil {
			return nil, fmt.Errorf("collectIndexAdvisorWorkload | %w", err)
		}

		row.query = text.String
		row.calls = calls.Int64
		row.totalTimeMs = totalTime.Float64
		row.rows = rowCount.Int64

		out = append(out, row)
	}

	if err := pgRows.Err(); err != nil {
		return nil, fmt.Errorf("collectIndexAdvisorWorkload | %w", err)
	}

	return out, nil
}

// indexAdvisorReader is one catalog query with the scanner that fills the catalog
// from its rows.
type indexAdvisorReader struct {
	q    enums.Query
	data query.TemplateData
	// scan fills the catalog from one row and names the relation it belongs to,
	// so a read stopped by the row cap can drop the relation it stopped inside.
	scan func(rowScanner) (indexadvisor.RelKey, error)
}

// readIndexAdvisorSchema reads the structure a candidate is judged against:
// what exists, what columns it has, and what is already indexed.
//
// Sequential, like the schema checks: the pool has four connections by default
// and is shared with the pages the user is looking at.
func (p *PgxPool) readIndexAdvisorSchema(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
	cat *indexadvisor.Catalog,
) error {
	pgStatsView := p.resolvePgStatsView(ctx, pool)

	return p.runIndexAdvisorReaders(ctx, pool, vNum, cat, []indexAdvisorReader{
		{
			q:    enums.QueryIndexAdvisorRelations,
			scan: func(row rowScanner) (indexadvisor.RelKey, error) { return scanIndexAdvisorRelation(row, cat) },
		},
		{
			q: enums.QueryIndexAdvisorColumns,
			data: struct {
				PgStatsView         string
				PgStatsHasInherited bool
			}{PgStatsView: pgStatsView.Name, PgStatsHasInherited: pgStatsView.HasInherited},
			scan: func(row rowScanner) (indexadvisor.RelKey, error) { return scanIndexAdvisorColumn(row, cat) },
		},
		{
			q:    enums.QueryIndexAdvisorIndexes,
			scan: func(row rowScanner) (indexadvisor.RelKey, error) { return scanIndexAdvisorIndex(row, cat) },
		},
	})
}

// readIndexAdvisorWrites adds one host's table activity to the catalog. It is
// separate from the schema because it is the one part of the catalog that differs
// per host: pg_stat_user_tables counters are not replicated, and they are summed
// over the cluster rather than read once.
func (p *PgxPool) readIndexAdvisorWrites(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
	cat *indexadvisor.Catalog,
) error {
	return p.runIndexAdvisorReaders(ctx, pool, vNum, cat, []indexAdvisorReader{
		{
			q:    enums.QueryIndexAdvisorWrites,
			scan: func(row rowScanner) (indexadvisor.RelKey, error) { return scanIndexAdvisorWrites(row, cat) },
		},
	})
}

func (p *PgxPool) runIndexAdvisorReaders(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
	cat *indexadvisor.Catalog,
	readers []indexAdvisorReader,
) error {
	for _, r := range readers {
		qStr, err := query.Get(vNum, r.q, r.data)
		if err != nil {
			return fmt.Errorf("collectIndexAdvisorCatalog | %s | %w", r.q, err)
		}

		last, truncated, err := scanIndexAdvisorRows(ctx, pool, qStr, r.scan)
		if err != nil {
			return fmt.Errorf("collectIndexAdvisorCatalog | %s | %w", r.q, err)
		}

		if truncated {
			// Every catalog query orders by relation, so the cap can only fall
			// inside the last one read. Its rows are as far as the read got, not
			// as far as the relation goes, and half an index list is exactly what
			// makes a duplicate candidate look new — so that relation is dropped
			// rather than kept in part.
			cat.Forget(last)

			cat.Truncated = true
		}
	}

	return nil
}

func scanIndexAdvisorRelation(row rowScanner, cat *indexadvisor.Catalog) (indexadvisor.RelKey, error) {
	var (
		rel                      indexadvisor.Relation
		rootSchema, rootName     string
		parentSchema, parentName string
	)

	if err := row.Scan(&rel.Schema, &rel.Name, &rel.Kind, &rel.Rows, &rel.Pages,
		&rootSchema, &rootName, &parentSchema, &parentName); err != nil {
		return indexadvisor.RelKey{}, err
	}

	if rootName != "" {
		rel.Root = indexadvisor.RelKey{Schema: rootSchema, Name: rootName}
	}

	if parentName != "" {
		rel.Parent = indexadvisor.RelKey{Schema: parentSchema, Name: parentName}
	}

	cat.AddRelation(rel)

	return rel.RelKey, nil
}

func scanIndexAdvisorColumn(row rowScanner, cat *indexadvisor.Catalog) (indexadvisor.RelKey, error) {
	var (
		key indexadvisor.RelKey
		col indexadvisor.Column
	)

	if err := row.Scan(&key.Schema, &key.Name, &col.Name, &col.DataType,
		&col.BtreeIndexable, &col.StatsKnown, &col.NDistinct, &col.NullFrac); err != nil {
		return indexadvisor.RelKey{}, err
	}

	cat.AddColumn(key, col)

	return key, nil
}

func scanIndexAdvisorIndex(row rowScanner, cat *indexadvisor.Catalog) (indexadvisor.RelKey, error) {
	var (
		key       indexadvisor.RelKey
		idx       indexadvisor.Index
		predicate *string
	)

	if err := row.Scan(&key.Schema, &key.Name, &idx.Name, &idx.Method,
		&idx.Unique, &idx.Primary, &idx.Valid, &idx.Partial, &idx.Expression,
		&idx.Columns, &predicate); err != nil {
		return indexadvisor.RelKey{}, err
	}

	if predicate != nil {
		idx.NullPredicate = indexadvisor.NullPredicateColumns(*predicate)
	}

	cat.AddIndex(key, idx)

	return key, nil
}

func scanIndexAdvisorWrites(row rowScanner, cat *indexadvisor.Catalog) (indexadvisor.RelKey, error) {
	var (
		key indexadvisor.RelKey
		w   indexadvisor.Writes
	)

	if err := row.Scan(&key.Schema, &key.Name, &w.Inserted, &w.Updated, &w.Deleted,
		&w.SeqScans, &w.IdxScans); err != nil {
		return indexadvisor.RelKey{}, err
	}

	cat.AddWrites(key, w)

	return key, nil
}

// scanIndexAdvisorRows runs one catalog query and hands each row to scan. One row
// over the cap reports the catalog as truncated rather than silently short, along
// with the relation the last row read belonged to — the one the caller cannot
// assume it read whole.
func scanIndexAdvisorRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	qStr string,
	scan func(rowScanner) (indexadvisor.RelKey, error),
) (indexadvisor.RelKey, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, indexAdvisorQueryTimeout)
	defer cancel()

	rows, err := pool.Query(ctx, qStr, indexAdvisorMaxCatalogRows+1)
	if err != nil {
		return indexadvisor.RelKey{}, false, err
	}
	defer rows.Close()

	var last indexadvisor.RelKey

	count := 0

	for rows.Next() {
		count++
		if count > indexAdvisorMaxCatalogRows {
			return last, true, nil
		}

		key, err := scan(rows)
		if err != nil {
			return indexadvisor.RelKey{}, false, err
		}

		last = key
	}

	return indexadvisor.RelKey{}, false, rows.Err()
}
