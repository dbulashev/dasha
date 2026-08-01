package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/schemalint"
)

const (
	// schemaLintCheckTimeout bounds one check. A check that outruns it becomes a
	// skip so the neighbours still produce a report.
	schemaLintCheckTimeout = 15 * time.Second
	// schemaLintMaxRowsPerCheck caps what one check may return: a schema with a
	// systematic defect must not be able to produce an unbounded response.
	schemaLintMaxRowsPerCheck = 5000
	// schemaLintSweepTimeout bounds the whole instance sweep, not one database.
	schemaLintSweepTimeout = 2 * time.Minute
	// schemaLintMaxDatabases caps how many databases one sweep visits.
	schemaLintMaxDatabases = 30
	// schemaLintHeadroomTimeout bounds the instance-wide sequence probe. It runs
	// inside a health-score request, which is polled from the home page and must
	// answer even when one database does not.
	schemaLintHeadroomTimeout = 20 * time.Second
	// schemaLintFailureTTL is how long a failed sequence probe is remembered.
	// Short, because the cause is usually transient — but without it a query that
	// fails every time would make every poll wait out its timeout again.
	schemaLintFailureTTL = 30 * time.Second
	// partitionRootsCode names the helper query in the skip list — it is not a
	// check, but its absence changes the report (no rollup), so it is reported.
	partitionRootsCode = "partition_roots"
)

// SQLSTATEs that separate "this role may not look" and "this server has no such
// catalog" from a genuine failure.
const (
	insufficientPrivilegeCode = "42501"
	undefinedTableCode        = "42P01"
	undefinedColumnCode       = "42703"
	undefinedFunctionCode     = "42883"
)

type schemaLintCacheEntry struct {
	report    schemalint.Report
	expiresAt time.Time
}

// sequenceHeadroomEntry shares the cache map with the reports under its own key
// prefix. Far cheaper to compute, and it also stands in for a failed probe, on
// a shorter TTL of its own.
type sequenceHeadroomEntry struct {
	worst     float64
	known     bool
	expiresAt time.Time
}

// GetSchemaLintReport runs the enabled schema checks against one database and
// assembles the report. A failing check never fails the report: it is recorded
// as a skip with its reason, because a check that did not run must not read as
// a clean result.
func (p *PgxPool) GetSchemaLintReport(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	refresh bool,
) (schemalint.Report, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return schemalint.Report{}, fmt.Errorf("GetSchemaLintReport | %w", err)
	}

	// The key always names a real database: an empty databaseName resolves to
	// whichever pool answered, and caching that under "" would keep a second
	// copy of the same database's report.
	databaseName = poolDatabase(pool, databaseName)
	key := schemaLintCacheKey(clusterName, instanceName, databaseName)

	if cached, ok := p.cachedSchemaLintReport(key, refresh); ok {
		return cached, nil
	}

	return p.schemaLintReportForPool(ctx, key, clusterName, databaseName, pool)
}

// poolDatabase names the database a pool is connected to, falling back to the
// requested name when the pool cannot say.
func poolDatabase(pool *pgxpool.Pool, requested string) string {
	if requested != "" {
		return requested
	}

	if cfg := pool.Config(); cfg != nil && cfg.ConnConfig != nil {
		return cfg.ConnConfig.Database
	}

	return requested
}

// GetSequenceHeadroom answers the one question the health score asks of the
// schema checks: how close to its ceiling is the worst sequence (0..1). It runs
// that single query rather than the whole report — the score is polled from the
// home page and from fleet-wide sweeps, and must not drag a full catalog scan
// behind it.
//
// An empty databaseName means the whole instance, which is the scope the score
// itself has: a sequence stops writes in whichever database holds it, so
// answering from one database would report an instance healthy because its
// first database happens to be.
func (p *PgxPool) GetSequenceHeadroom(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
) (float64, bool, error) {
	if databaseName != "" {
		pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
		if err != nil {
			return 0, false, fmt.Errorf("GetSequenceHeadroom | %w", err)
		}

		return p.sequenceHeadroomForPool(ctx, clusterName, instanceName, poolDatabase(pool, databaseName), pool)
	}

	pools, err := p.instanceDatabasePools(ctx, clusterName, instanceName)
	if err != nil {
		return 0, false, fmt.Errorf("GetSequenceHeadroom | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, schemaLintHeadroomTimeout)
	defer cancel()

	var (
		worst    float64
		known    bool
		firstErr error
	)

	for _, dbp := range pools {
		if ctx.Err() != nil {
			break
		}

		w, ok, err := p.sequenceHeadroomForPool(ctx, clusterName, instanceName, dbp.database, dbp.pool)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}

			continue
		}

		if !ok {
			continue
		}

		known = true
		if w > worst {
			worst = w
		}
	}

	// One unreadable database among several is not a reason to drop the number
	// the others produced; nothing readable at all is.
	if !known && firstErr != nil {
		return 0, false, firstErr
	}

	return worst, known, nil
}

// sequenceHeadroomForPool is the cached per-database probe. Failures are cached
// too, briefly: the caller polls, and a query that fails every time must not
// cost its timeout every time.
func (p *PgxPool) sequenceHeadroomForPool(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	pool *pgxpool.Pool,
) (float64, bool, error) {
	key := "sequences/" + schemaLintCacheKey(clusterName, instanceName, databaseName)

	if cached, ok := p.schemaLintCache.Load(key); ok {
		if entry, valid := cached.(sequenceHeadroomEntry); valid && time.Now().Before(entry.expiresAt) {
			return entry.worst, entry.known, nil
		}
	}

	worst, known, err := p.readSequenceHeadroom(ctx, pool)
	if err != nil {
		// Not when it is the caller's own deadline that expired: that says nothing
		// about this database, and remembering it would blind the next poll too.
		if ctx.Err() == nil {
			p.storeSequenceHeadroom(key, 0, false, schemaLintFailureTTL)
		}

		return 0, false, fmt.Errorf("GetSequenceHeadroom | %s | %w", databaseName, err)
	}

	p.storeSequenceHeadroom(key, worst, known, p.schemaLintConfig.CacheTTLOrDefault())

	return worst, known, nil
}

func (p *PgxPool) readSequenceHeadroom(ctx context.Context, pool *pgxpool.Pool) (float64, bool, error) {
	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return 0, false, err
	}

	qStr, err := query.Get(vNum, enums.QuerySchemaLintSequencesUsage, nil)
	if err != nil {
		return 0, false, err
	}

	var in schemalint.Inputs

	if _, err := p.runSchemaLintQuery(ctx, pool, qStr, enums.QuerySchemaLintSequencesUsage, &in); err != nil {
		return 0, false, err
	}

	worst, known := schemalint.WorstSequenceUsage(in.Sequences, p.schemaLintConfig)

	return worst, known, nil
}

func (p *PgxPool) storeSequenceHeadroom(key string, worst float64, known bool, ttl time.Duration) {
	p.schemaLintCache.Store(key, sequenceHeadroomEntry{
		worst:     worst,
		known:     known,
		expiresAt: time.Now().Add(ttl),
	})
}

func schemaLintCacheKey(clusterName, instanceName, databaseName string) string {
	return clusterName + "/" + instanceName + "/" + databaseName
}

func (p *PgxPool) cachedSchemaLintReport(key string, refresh bool) (schemalint.Report, bool) {
	if refresh {
		return schemalint.Report{}, false
	}

	cached, ok := p.schemaLintCache.Load(key)
	if !ok {
		return schemalint.Report{}, false
	}

	entry, valid := cached.(schemaLintCacheEntry)
	if !valid || !time.Now().Before(entry.expiresAt) {
		return schemalint.Report{}, false
	}

	return entry.report, true
}

// schemaLintReportForPool is the shared body of the single-database report and
// the instance sweep: the sweep holds pools, not database names, and both must
// fill the same cache so opening the page after the sweep is free.
func (p *PgxPool) schemaLintReportForPool(
	ctx context.Context,
	key, clusterName, databaseName string,
	pool *pgxpool.Pool,
) (schemalint.Report, error) {
	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return schemalint.Report{}, fmt.Errorf("get server version | %w", err)
	}

	started := time.Now()
	report := schemalint.BuildReport(p.collectSchemaLint(ctx, pool, vNum), p.schemaLintConfig)
	report.DurationMs = time.Since(started).Milliseconds()

	p.logSchemaLintSkips(clusterName, databaseName, report.Skipped)

	// A report where nothing actually ran must not be cached: an expired sweep
	// deadline would otherwise pin an empty "all skipped" answer to the page for
	// the whole TTL, and it reads exactly like a clean schema.
	if ctx.Err() == nil && !allChecksFailed(report) {
		p.schemaLintCache.Store(key, schemaLintCacheEntry{
			report:    report,
			expiresAt: time.Now().Add(p.schemaLintConfig.CacheTTLOrDefault()),
		})
	}

	p.evictStaleSchemaLintCache()

	return report, nil
}

// allChecksFailed reports whether every check that was supposed to run ended in
// SkipError — a report with no evidence in it at all.
func allChecksFailed(report schemalint.Report) bool {
	if len(report.Findings) > 0 {
		return false
	}

	failed := 0

	for _, s := range report.Skipped {
		switch s.Reason {
		case schemalint.SkipError:
			failed++
		case schemalint.SkipDisabled, schemalint.SkipUnsupportedVersion:
			// Deliberately not run — says nothing about whether the rest worked.
		case schemalint.SkipInsufficientPrivileges:
			// The check ran and reported what it could not see: that is a result.
			return false
		}
	}

	return failed > 0
}

// GetSchemaLintSummary walks every database of one instance and returns the
// per-level counts of each. Sequential, capped and time-bounded: a cluster with
// dozens of databases must not turn one page load into a catalog scan storm.
// A database that cannot be read is marked failed rather than reported clean.
func (p *PgxPool) GetSchemaLintSummary(
	ctx context.Context,
	clusterName, instanceName string,
) ([]schemalint.DatabaseSummary, error) {
	pools, err := p.instanceDatabasePools(ctx, clusterName, instanceName)
	if err != nil {
		return nil, fmt.Errorf("GetSchemaLintSummary | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, schemaLintSweepTimeout)
	defer cancel()

	out := make([]schemalint.DatabaseSummary, 0, len(pools))

	for _, dbp := range pools {
		key := schemaLintCacheKey(clusterName, instanceName, dbp.database)

		report, ok := p.cachedSchemaLintReport(key, false)
		if !ok {
			report, err = p.schemaLintReportForPool(ctx, key, clusterName, dbp.database, dbp.pool)
			if err != nil {
				p.logger.Warn("schema lint sweep: database unreadable",
					zap.String("cluster", clusterName),
					zap.String("instance", instanceName),
					zap.String("database", dbp.database),
					zap.Error(err))

				out = append(out, schemalint.DatabaseSummary{Database: dbp.database, Failed: true})

				continue
			}
		}

		out = append(out, schemalint.DatabaseSummary{
			Database: dbp.database,
			Error:    report.Summary[schemalint.LevelError],
			Warning:  report.Summary[schemalint.LevelWarning],
			Notice:   report.Summary[schemalint.LevelNotice],
			Skipped:  report.NotRun(),
		})
	}

	return out, nil
}

type schemaLintDBPool struct {
	database string
	pool     *pgxpool.Pool
}

// instanceDatabasePools snapshots the pools of one instance, one per database.
func (p *PgxPool) instanceDatabasePools(
	ctx context.Context,
	clusterName, instanceName string,
) ([]schemaLintDBPool, error) {
	if err := p.ensurePool(ctx); err != nil {
		return nil, fmt.Errorf("ensure pool | %w", err)
	}

	p.mu.RLock()

	var pools []schemaLintDBPool

	seen := make(map[string]bool)

	for cluster, items := range p.pools {
		if cluster.String() != clusterName {
			continue
		}

		for _, it := range items {
			if it.Host.String() != instanceName {
				continue
			}

			db := string(it.Database)
			if seen[db] {
				continue
			}

			seen[db] = true

			pools = append(pools, schemaLintDBPool{database: db, pool: it.pool})
		}
	}

	p.mu.RUnlock()

	if len(pools) == 0 {
		return nil, fmt.Errorf("%w | %s/%s", ErrNotFound, clusterName, instanceName)
	}

	// Sorted so the sweep visits databases in the same order every time, and
	// capped so a cluster with a database per tenant cannot stall the page.
	slices.SortFunc(pools, func(a, b schemaLintDBPool) int {
		return strings.Compare(a.database, b.database)
	})

	if len(pools) > schemaLintMaxDatabases {
		pools = pools[:schemaLintMaxDatabases]
	}

	return pools, nil
}

// evictStaleSchemaLintCache drops expired entries so a database that stops being
// queried (dropped, removed from the config) does not keep its report forever.
// One pass per computed report — the map holds one key per database.
func (p *PgxPool) evictStaleSchemaLintCache() {
	now := time.Now()

	p.schemaLintCache.Range(func(k, v any) bool {
		switch entry := v.(type) {
		case schemaLintCacheEntry:
			if now.After(entry.expiresAt) {
				p.schemaLintCache.Delete(k)
			}
		case sequenceHeadroomEntry:
			if now.After(entry.expiresAt) {
				p.schemaLintCache.Delete(k)
			}
		}

		return true
	})
}

// collectSchemaLint runs the planned queries one after another. Sequential on
// purpose: the pool has four connections by default and is shared with the
// pages the user is looking at, so seven parallel catalog scans would only
// crowd them out.
func (p *PgxPool) collectSchemaLint(ctx context.Context, pool *pgxpool.Pool, vNum int) schemalint.Inputs {
	plans, skips := schemalint.Plan(p.schemaLintConfig, vNum)

	in := schemalint.Inputs{ServerVersionNum: vNum, Skipped: skips}

	needsRoots := false

	for _, plan := range plans {
		qStr, err := query.Get(vNum, plan.Query, nil)
		if err != nil {
			in.Skipped = append(in.Skipped, skipForCodes(plan.Codes, schemalint.SkipError, "template unavailable")...)
			continue
		}

		// Rows land in a scratch Inputs and are merged only once the query has
		// finished: a check that dies mid-cursor must not leave half its findings
		// in the report next to its own "did not run" skip.
		var scratch schemalint.Inputs

		truncated, err := p.runSchemaLintQuery(ctx, pool, qStr, plan.Query, &scratch)
		if err != nil {
			in.Skipped = append(in.Skipped, skipForCodes(plan.Codes, classifySchemaLintError(err), skipDetail(err))...)
			continue
		}

		mergeSchemaLintInputs(&in, scratch)

		in.Truncated = in.Truncated || truncated

		needsRoots = needsRoots || planCollapses(plan.Codes)
	}

	if needsRoots {
		roots, truncated, err := p.getPartitionRoots(ctx, pool, vNum)
		if err != nil {
			// Without the map every partition reports separately — noisy, but the
			// findings themselves are still correct, so this is a skip, not an error.
			in.Skipped = append(in.Skipped, schemalint.Skip{
				Code:   partitionRootsCode,
				Reason: classifySchemaLintError(err),
				Detail: skipDetail(err),
			})
		}

		// A partly read map leaves part of the tree unrolled, so the report shows
		// individual partitions where a complete run would show one parent. Same
		// flag as a capped check: what is on screen is not the finished answer.
		in.Truncated = in.Truncated || truncated
		in.PartitionRoots = roots
	}

	return in
}

// mergeSchemaLintInputs folds one completed query's rows into the report input.
// Only the row slices are merged — everything else (skips, version, truncation)
// is the caller's business.
func mergeSchemaLintInputs(dst *schemalint.Inputs, src schemalint.Inputs) {
	dst.Sequences = append(dst.Sequences, src.Sequences...)
	dst.RelationKeys = append(dst.RelationKeys, src.RelationKeys...)
	dst.SchemaPrivileges = append(dst.SchemaPrivileges, src.SchemaPrivileges...)
	dst.Unlogged = append(dst.Unlogged, src.Unlogged...)
	dst.UUIDLikeColumns = append(dst.UUIDLikeColumns, src.UUIDLikeColumns...)
	dst.WithoutFk = append(dst.WithoutFk, src.WithoutFk...)
	dst.WithoutColumns = append(dst.WithoutColumns, src.WithoutColumns...)
	dst.UnsafeNames = append(dst.UnsafeNames, src.UnsafeNames...)
	dst.InvalidConstrs = append(dst.InvalidConstrs, src.InvalidConstrs...)
	dst.FkTypeMismatch = append(dst.FkTypeMismatch, src.FkTypeMismatch...)
	dst.FkNullable = append(dst.FkNullable, src.FkNullable...)
	dst.FkSimilar = append(dst.FkSimilar, src.FkSimilar...)
	dst.IndexSimilar = append(dst.IndexSimilar, src.IndexSimilar...)
	dst.BtreeOnArray = append(dst.BtreeOnArray, src.BtreeOnArray...)
}

// planCollapses reports whether any of the codes rolls findings up to a
// partition root, i.e. whether the roots map is worth a query.
func planCollapses(codes []string) bool {
	for _, chk := range schemalint.Registry() {
		for _, code := range codes {
			if chk.Code == code && chk.CollapseParts {
				return true
			}
		}
	}

	return false
}

// borrowedQueries are checks whose SQL already serves a page of its own. Their
// templates take no limit parameter — capping them here would mean editing a
// query other endpoints depend on — so the cap is applied while scanning.
var borrowedQueries = map[enums.Query]bool{
	enums.QueryConstraintsInvalidConstraints: true,
	enums.QueryFksTypeMismatch:               true,
	enums.QueryFksPossibleNulls:              true,
	enums.QueryFksPossibleSimilar1:           true,
	enums.QueryIndexesSimilar3:               true,
	enums.QueryIndexesBtreeOnArray:           true,
}

func (p *PgxPool) runSchemaLintQuery(
	ctx context.Context,
	pool *pgxpool.Pool,
	qStr string,
	q enums.Query,
	in *schemalint.Inputs,
) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, schemaLintCheckTimeout)
	defer cancel()

	// One row over the cap tells us the result was cut short.
	args := []any{schemaLintMaxRowsPerCheck + 1}
	if borrowedQueries[q] {
		args = nil
	}

	rows, err := pool.Query(ctx, qStr, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	count := 0
	truncated := false

	for rows.Next() {
		count++
		if count > schemaLintMaxRowsPerCheck {
			truncated = true
			break
		}

		if err := scanSchemaLintRow(rows, q, in); err != nil {
			return false, err
		}
	}

	if err := rows.Err(); err != nil {
		return false, err
	}

	return truncated, nil
}

// rowScanner is the part of pgx.Rows the per-query scanners need.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSchemaLintRow(rows rowScanner, q enums.Query, in *schemalint.Inputs) error {
	switch q {
	case enums.QuerySchemaLintSequencesUsage:
		var (
			r       schemalint.SequenceRow
			freePct pgtype.Float8
		)

		if err := rows.Scan(&r.Schema, &r.Object, &r.LastValue, &r.LastValueKnown,
			&r.MaxValue, &r.MinValue, &freePct, &r.OwnedBy, &r.OwnedColumnType); err != nil {
			return err
		}

		// NULL means maxvalue = minvalue: a sequence with a single possible
		// value, which is exhausted by definition.
		if freePct.Valid {
			r.FreePct = freePct.Float64
		}

		in.Sequences = append(in.Sequences, r)

	case enums.QuerySchemaLintRelationsWithoutKey:
		var r schemalint.RelationKeyRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.HasUnique, &r.UniqueNullable); err != nil {
			return err
		}

		in.RelationKeys = append(in.RelationKeys, r)

	case enums.QuerySchemaLintPublicCreatePrivileges:
		var r schemalint.SchemaPrivilegeRow

		if err := rows.Scan(&r.Schema, &r.Owner); err != nil {
			return err
		}

		in.SchemaPrivileges = append(in.SchemaPrivileges, r)

	case enums.QuerySchemaLintUnloggedObjects:
		var r schemalint.UnloggedRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.RelKind); err != nil {
			return err
		}

		in.Unlogged = append(in.Unlogged, r)

	case enums.QuerySchemaLintUuidLikeColumns:
		var r schemalint.ColumnRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.Column, &r.ColumnType); err != nil {
			return err
		}

		in.UUIDLikeColumns = append(in.UUIDLikeColumns, r)

	case enums.QuerySchemaLintRelationsWithoutFk:
		var r schemalint.RelationRow

		if err := rows.Scan(&r.Schema, &r.Object); err != nil {
			return err
		}

		in.WithoutFk = append(in.WithoutFk, r)

	case enums.QuerySchemaLintRelationsWithoutColumns:
		var r schemalint.RelationRow

		if err := rows.Scan(&r.Schema, &r.Object); err != nil {
			return err
		}

		in.WithoutColumns = append(in.WithoutColumns, r)

	case enums.QuerySchemaLintUnsafeNames:
		var r schemalint.NameRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.RelKind, &r.Reserved); err != nil {
			return err
		}

		in.UnsafeNames = append(in.UnsafeNames, r)

	case enums.QueryConstraintsInvalidConstraints:
		var r schemalint.ConstraintRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.Constraint, &r.ReferencedSchema, &r.ReferencedTable); err != nil {
			return err
		}

		in.InvalidConstrs = append(in.InvalidConstrs, r)

	case enums.QueryFksTypeMismatch:
		var (
			r                 schemalint.PairRow
			toSchema, toRel   string
			fromAtts, toAttrs []string
		)

		if err := rows.Scan(&r.Schema, &r.First, &r.Object, &fromAtts, &toSchema, &toRel, &toAttrs); err != nil {
			return err
		}

		r.Second = toSchema + "." + toRel
		in.FkTypeMismatch = append(in.FkTypeMismatch, r)

	case enums.QueryFksPossibleNulls:
		var (
			r    schemalint.PairRow
			atts []string
		)

		if err := rows.Scan(&r.Schema, &r.First, &r.Object, &atts); err != nil {
			return err
		}

		r.Second = strings.Join(atts, ", ")
		in.FkNullable = append(in.FkNullable, r)

	case enums.QueryFksPossibleSimilar1:
		var r schemalint.PairRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.First, &r.Second); err != nil {
			return err
		}

		in.FkSimilar = append(in.FkSimilar, r)

	case enums.QueryIndexesSimilar3:
		var (
			r          schemalint.PairRow
			simplified string
			def1, def2 string
			used1      pgtype.Text
			used2      pgtype.Text
		)

		if err := rows.Scan(&r.Schema, &r.Object, &r.First, &r.Second,
			&simplified, &def1, &def2, &used1, &used2); err != nil {
			return err
		}

		in.IndexSimilar = append(in.IndexSimilar, r)

	case enums.QueryIndexesBtreeOnArray:
		var r schemalint.PairRow

		if err := rows.Scan(&r.Schema, &r.Object, &r.First); err != nil {
			return err
		}

		in.BtreeOnArray = append(in.BtreeOnArray, r)

	default:
		return fmt.Errorf("schema lint: no scanner for query %s", q)
	}

	return nil
}

func (p *PgxPool) getPartitionRoots(
	ctx context.Context,
	pool *pgxpool.Pool,
	vNum int,
) (map[schemalint.ObjectRef]schemalint.ObjectRef, bool, error) {
	qStr, err := query.Get(vNum, enums.QuerySchemaLintPartitionRoots, nil)
	if err != nil {
		return nil, false, fmt.Errorf("getPartitionRoots | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, schemaLintCheckTimeout)
	defer cancel()

	// Capped like every other query: a tree of thousands of partitions must not
	// be able to pull an unbounded map into memory. One row over the cap tells us
	// the map is incomplete, which the report has to say — an unrolled partition
	// is indistinguishable from a table with a defect of its own.
	rows, err := pool.Query(ctx, qStr, schemaLintMaxRowsPerCheck+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var (
		roots     = make(map[schemalint.ObjectRef]schemalint.ObjectRef)
		truncated bool
	)

	for rows.Next() {
		if len(roots) >= schemaLintMaxRowsPerCheck {
			truncated = true
			break
		}

		var child, root schemalint.ObjectRef

		if err := rows.Scan(&child.Schema, &child.Object, &root.Schema, &root.Object); err != nil {
			return nil, false, err
		}

		roots[child] = root
	}

	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	return roots, truncated, nil
}

func skipForCodes(codes []string, reason schemalint.SkipReason, detail string) []schemalint.Skip {
	out := make([]schemalint.Skip, 0, len(codes))
	for _, code := range codes {
		out = append(out, schemalint.Skip{Code: code, Reason: reason, Detail: detail})
	}

	return out
}

// classifySchemaLintError turns a query failure into the reason the page shows.
func classifySchemaLintError(err error) schemalint.SkipReason {
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case insufficientPrivilegeCode:
			return schemalint.SkipInsufficientPrivileges
		case undefinedTableCode, undefinedColumnCode, undefinedFunctionCode:
			// The catalog the check reads does not exist here — an older or
			// differently built server, not a fault.
			return schemalint.SkipUnsupportedVersion
		}
	}

	return schemalint.SkipError
}

// skipDetail keeps the reason short and free of anything read from the database:
// a SQLSTATE, or the fact that time ran out.
func skipDetail(err error) string {
	if IsTimeout(err) {
		return "timeout"
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "SQLSTATE " + pgErr.Code
	}

	return "query failed"
}

// logSchemaLintSkips records genuine failures in the log: the API reports them
// as skips with a sanitized detail, and an operator needs more than that.
func (p *PgxPool) logSchemaLintSkips(clusterName, databaseName string, skips []schemalint.Skip) {
	for _, s := range skips {
		if s.Reason == schemalint.SkipError {
			p.logger.Warn("schema lint check failed",
				zap.String("cluster", clusterName),
				zap.String("database", databaseName),
				zap.String("check", s.Code),
				zap.String("detail", s.Detail))
		}
	}
}
