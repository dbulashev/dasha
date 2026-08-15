package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

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

// GetIndexAdvisorReport assembles the index candidate report for one database.
//
// Never cached: the page is opened deliberately, usually right after changing
// something, and a stale answer there is worse than the work it saves. What is
// cached is the parse of each statement, which is where the time actually goes.
func (p *PgxPool) GetIndexAdvisorReport(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	excludeUsers []string,
) (indexadvisor.Report, error) {
	cfg := p.indexAdvisorConfig.WithDefaults()

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return indexadvisor.Report{}, fmt.Errorf("GetIndexAdvisorReport | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return indexadvisor.Report{}, fmt.Errorf("GetIndexAdvisorReport | get server version | %w", err)
	}

	workload, err := p.collectIndexAdvisorWorkload(ctx, pool, vNum, excludeUsers)
	if err != nil {
		return indexadvisor.Report{}, fmt.Errorf("GetIndexAdvisorReport | %w", err)
	}

	// With nothing to judge, reading the catalog would be a full scan of it for an
	// answer that is already known — but the report is still built, because the
	// reasons the workload yielded nothing are the whole point of it.
	if len(workload.Entries) == 0 {
		return indexadvisor.Build(workload, indexadvisor.NewCatalog(), p.indexAdvisorConfig), nil
	}

	cat, err := p.collectIndexAdvisorCatalog(ctx, pool, vNum)
	if err != nil {
		return indexadvisor.Report{}, fmt.Errorf("GetIndexAdvisorReport | %w", err)
	}

	return indexadvisor.Build(workload, cat, p.indexAdvisorConfig), nil
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

// collectIndexAdvisorWorkload reads the top of pg_stat_statements for the current
// database and parses each statement.
//
// A statement that cannot be parsed is counted by reason rather than dropped: an
// empty candidate list next to fifty unparsed statements is not the same answer
// as an empty candidate list next to none, and the report has to keep the two
// apart.
func (p *PgxPool) collectIndexAdvisorWorkload(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
	excludeUsers []string,
) (indexadvisor.Workload, error) {
	// pg_stat_statements missing or unreadable is a state, not a failure — the
	// same treatment the query pages give it.
	if readable, _ := p.getQueryStatsReadable(ctx, vNum, pool); !readable {
		return indexadvisor.Workload{}, nil
	}

	rows, err := p.readIndexAdvisorWorkloadRows(ctx, pool, vNum, excludeUsers)
	if err != nil {
		return indexadvisor.Workload{}, err
	}

	// Parsing happens after the rows are read, not inside the cursor: half a
	// second of WASM work must not hold a connection of a pool the pages share.
	out := indexadvisor.Workload{Available: true, Collected: len(rows)}
	parser := p.indexAdvisorParser()

	for _, row := range rows {
		stmt, err := parser.Parse(row.query)
		if err != nil {
			out.CountNotParsed(sqlparse.ReasonOf(err))

			continue
		}

		out.Entries = append(out.Entries, indexadvisor.WorkloadEntry{
			QueryIDs:    []int64{row.queryID},
			Fingerprint: stmt.Fingerprint,
			Query:       sanitize.SQL(row.query),
			Calls:       row.calls,
			TotalTimeMs: row.totalTimeMs,
			Rows:        row.rows,
			Stmt:        stmt,
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

// collectIndexAdvisorCatalog reads the state of the database a candidate is
// judged against. Sequential, like the schema checks: the pool has four
// connections by default and is shared with the pages the user is looking at.
func (p *PgxPool) collectIndexAdvisorCatalog(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
) (indexadvisor.Catalog, error) {
	cat := indexadvisor.NewCatalog()

	pgStatsView := p.resolvePgStatsView(ctx, pool)

	readers := []struct {
		q    enums.Query
		data query.TemplateData
		scan func(rowScanner) error
	}{
		{
			q:    enums.QueryIndexAdvisorRelations,
			scan: func(row rowScanner) error { return scanIndexAdvisorRelation(row, &cat) },
		},
		{
			q:    enums.QueryIndexAdvisorColumns,
			data: struct{ PgStatsView string }{PgStatsView: pgStatsView},
			scan: func(row rowScanner) error { return scanIndexAdvisorColumn(row, &cat) },
		},
		{
			q:    enums.QueryIndexAdvisorIndexes,
			scan: func(row rowScanner) error { return scanIndexAdvisorIndex(row, &cat) },
		},
		{
			q:    enums.QueryIndexAdvisorWrites,
			scan: func(row rowScanner) error { return scanIndexAdvisorWrites(row, &cat) },
		},
	}

	for _, r := range readers {
		qStr, err := query.Get(vNum, r.q, r.data)
		if err != nil {
			return cat, fmt.Errorf("collectIndexAdvisorCatalog | %s | %w", r.q, err)
		}

		truncated, err := scanIndexAdvisorRows(ctx, pool, qStr, r.scan)
		if err != nil {
			return cat, fmt.Errorf("collectIndexAdvisorCatalog | %s | %w", r.q, err)
		}

		cat.Truncated = cat.Truncated || truncated
	}

	return cat, nil
}

func scanIndexAdvisorRelation(row rowScanner, cat *indexadvisor.Catalog) error {
	var (
		rel                  indexadvisor.Relation
		rootSchema, rootName string
	)

	if err := row.Scan(&rel.Schema, &rel.Name, &rel.Kind, &rel.Rows, &rel.Pages,
		&rootSchema, &rootName); err != nil {
		return err
	}

	if rootName != "" {
		rel.Root = indexadvisor.RelKey{Schema: rootSchema, Name: rootName}
	}

	cat.AddRelation(rel)

	return nil
}

func scanIndexAdvisorColumn(row rowScanner, cat *indexadvisor.Catalog) error {
	var (
		key indexadvisor.RelKey
		col indexadvisor.Column
	)

	if err := row.Scan(&key.Schema, &key.Name, &col.Name, &col.DataType,
		&col.BtreeIndexable, &col.StatsKnown, &col.NDistinct, &col.NullFrac); err != nil {
		return err
	}

	cat.AddColumn(key, col)

	return nil
}

func scanIndexAdvisorIndex(row rowScanner, cat *indexadvisor.Catalog) error {
	var (
		key indexadvisor.RelKey
		idx indexadvisor.Index
	)

	if err := row.Scan(&key.Schema, &key.Name, &idx.Name, &idx.Method,
		&idx.Unique, &idx.Primary, &idx.Valid, &idx.Partial, &idx.Expression,
		&idx.Columns); err != nil {
		return err
	}

	cat.AddIndex(key, idx)

	return nil
}

func scanIndexAdvisorWrites(row rowScanner, cat *indexadvisor.Catalog) error {
	var (
		key indexadvisor.RelKey
		w   indexadvisor.Writes
	)

	if err := row.Scan(&key.Schema, &key.Name, &w.Inserted, &w.Updated, &w.Deleted,
		&w.SeqScans, &w.IdxScans, &w.LiveTuples); err != nil {
		return err
	}

	cat.SetWrites(key, w)

	return nil
}

// scanIndexAdvisorRows runs one catalog query and hands each row to scan. One row
// over the cap reports the catalog as truncated rather than silently short.
func scanIndexAdvisorRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	qStr string,
	scan func(rowScanner) error,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, indexAdvisorQueryTimeout)
	defer cancel()

	rows, err := pool.Query(ctx, qStr, indexAdvisorMaxCatalogRows+1)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	count := 0

	for rows.Next() {
		count++
		if count > indexAdvisorMaxCatalogRows {
			return true, nil
		}

		if err := scan(rows); err != nil {
			return false, err
		}
	}

	return false, rows.Err()
}
