package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetMaintenanceAutovacuumFreezeMaxAge(
	ctx context.Context,
	clusterName,
	instanceName string,
) ([]dto.MaintenanceAutovacuumFreezeMaxAge, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceAutovacuumFreezeMaxAge(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceInfo(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
	tableName *string,
	activity string,
	cold dto.ColdArgs,
	limit,
	offset int,
) ([]dto.MaintenanceInfo, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceInfo | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceInfo(ctx, vNum, pool, tableName, activity, cold, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceTransactionIdDanger(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.MaintenanceTransactionIdDanger, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceTransactionIdDanger | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceTransactionIdDanger(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceVacuumProgress(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) ([]dto.MaintenanceVacuumProgress, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceVacuumProgress | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceVacuumProgress(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetMaintenanceAutovacuumSummary(
	ctx context.Context,
	clusterName,
	instanceName,
	databaseName string,
) (*dto.MaintenanceAutovacuumSummary, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceAutovacuumSummary | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceAutovacuumSummary(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumSummary | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceAutovacuumFreezeMaxAge(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.MaintenanceAutovacuumFreezeMaxAge, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceAutovacuumFreezeMaxAge, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	ret := make([]dto.MaintenanceAutovacuumFreezeMaxAge, 0, 1)

	for rows.Next() {
		var autovacuumFreezeMaxAge int64

		err = rows.Scan(&autovacuumFreezeMaxAge)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
		}

		ret = append(ret, dto.MaintenanceAutovacuumFreezeMaxAge{
			AutovacuumFreezeMaxAge: autovacuumFreezeMaxAge,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumFreezeMaxAge | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceInfo(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	tableName *string,
	activity string,
	cold dto.ColdArgs,
	limit,
	offset int,
) ([]dto.MaintenanceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceInfo, healthScoreTemplateData{ColdCTE: query.ColdCTE(4)})
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	if activity == "" {
		activity = "all"
	}

	rows, err := pool.Query(ctx, qStr, append(append([]any{tableName, limit, offset}, coldQueryArgs(cold)...), activity)...)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	ret := make([]dto.MaintenanceInfo, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table                string
			lastVacuum, lastAutovacuum   pgtype.Timestamp
			lastAnalyze, lastAutoanalyze pgtype.Timestamp
			deadRows, liveRows           int64
			coldIdx                      *int
		)

		err = rows.Scan(&schema, &table, &lastVacuum, &lastAutovacuum, &lastAnalyze, &lastAutoanalyze, &deadRows, &liveRows, &coldIdx)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
		}

		ret = append(ret, dto.MaintenanceInfo{
			Schema:          schema,
			Table:           table,
			LastVacuum:      convertPgTimestampToTime(lastVacuum),
			LastAutovacuum:  convertPgTimestampToTime(lastAutovacuum),
			LastAnalyze:     convertPgTimestampToTime(lastAnalyze),
			LastAutoanalyze: convertPgTimestampToTime(lastAutoanalyze),
			DeadRows:        deadRows,
			LiveRows:        liveRows,
			ColdIdx:         coldIdx,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getMaintenanceInfo | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceTransactionIdDanger(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.MaintenanceTransactionIdDanger, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	maxAgeRows, err := p.getMaintenanceAutovacuumFreezeMaxAge(ctx, serverVersion, pool)
	if err != nil {
		return nil, fmt.Errorf("get autovacuum_freeze_max_age | %w", err)
	}

	var freezeMaxAge int64 = 200000000 // default PG value
	if len(maxAgeRows) > 0 {
		freezeMaxAge = maxAgeRows[0].AutovacuumFreezeMaxAge
	}

	// Danger threshold: tables with less than 5% of max age remaining
	dangerThreshold := freezeMaxAge / 20 //nolint:mnd

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceTransactionIdDanger, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, freezeMaxAge, dangerThreshold)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	ret := make([]dto.MaintenanceTransactionIdDanger, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			schema, table    string
			transactionsLeft int64
		)

		err = rows.Scan(&schema, &table, &transactionsLeft)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
		}

		ret = append(ret, dto.MaintenanceTransactionIdDanger{
			Schema:           schema,
			Table:            table,
			TransactionsLeft: transactionsLeft,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getMaintenanceTransactionIdDanger | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceVacuumProgress(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.MaintenanceVacuumProgress, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceVacuumProgress, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	ret := make([]dto.MaintenanceVacuumProgress, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			row   dto.MaintenanceVacuumProgress
			phase pgtype.Text
		)

		err = rows.Scan(&row.Pid, &phase, &row.StartedBy, &row.Mode)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
		}

		row.Phase = phase.String
		ret = append(ret, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getMaintenanceVacuumProgress | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceAutovacuumSummary(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) (*dto.MaintenanceAutovacuumSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceAutovacuumSummary, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumSummary | %w", err)
	}

	var ret dto.MaintenanceAutovacuumSummary

	err = pool.QueryRow(ctx, qStr).Scan(
		&ret.TablesDueVacuumOnly,
		&ret.TablesDueAnalyzeOnly,
		&ret.TablesDueBoth,
		&ret.TablesTotal,
		&ret.RunningVacuums,
		&ret.RunningAnalyzes,
		&ret.RunningVacuumsManual,
		&ret.RunningVacuumsAutovacuum,
		&ret.RunningVacuumsWraparound,
		&ret.RunningAnalyzesManual,
		&ret.RunningAnalyzesAutovacuum,
	)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumSummary | %w", err)
	}

	ret.Source = dto.AutovacuumSummarySourceFormula
	if serverVersion >= autovacuumScoresMinVersionNum {
		ret.Source = dto.AutovacuumSummarySourceScores
	}

	return &ret, nil
}

const autovacuumScoresMinVersionNum = 190000

var ErrAutovacuumScoresUnsupported = errors.New("pg_stat_autovacuum_scores requires PostgreSQL 19 or newer")

// GetMaintenanceAutovacuumQueue reads pg_stat_autovacuum_scores of one
// database, highest score first.
func (p *PgxPool) GetMaintenanceAutovacuumQueue(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	limit, offset int,
) (*dto.MaintenanceAutovacuumQueue, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetMaintenanceAutovacuumQueue | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getMaintenanceAutovacuumQueue(ctx, vNum, pool, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumQueue | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getMaintenanceAutovacuumQueue(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit, offset int,
) (*dto.MaintenanceAutovacuumQueue, error) {
	if serverVersion < autovacuumScoresMinVersionNum {
		return nil, ErrAutovacuumScoresUnsupported
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryMaintenanceAutovacuumScores, nil)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumQueue | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumQueue | %w", err)
	}
	defer rows.Close()

	ret := &dto.MaintenanceAutovacuumQueue{Rows: make([]dto.MaintenanceAutovacuumQueueRow, 0, limit)}

	for rows.Next() {
		var (
			schema, table                         pgtype.Text
			score, xid, mxid, vac, vacIns, analyz pgtype.Float8
			doVacuum, doAnalyze, forWraparound    pgtype.Bool
		)

		err = rows.Scan(&ret.Total, &schema, &table,
			&score, &xid, &mxid, &vac, &vacIns, &analyz,
			&doVacuum, &doAnalyze, &forWraparound)
		if err != nil {
			return nil, fmt.Errorf("getMaintenanceAutovacuumQueue | %w", err)
		}

		if !table.Valid {
			continue
		}

		row := dto.MaintenanceAutovacuumQueueRow{
			Schema:            schema.String,
			Table:             table.String,
			Score:             score.Float64,
			XidScore:          xid.Float64,
			MxidScore:         mxid.Float64,
			VacuumScore:       vac.Float64,
			VacuumInsertScore: vacIns.Float64,
			AnalyzeScore:      analyz.Float64,
			DoVacuum:          doVacuum.Bool,
			DoAnalyze:         doAnalyze.Bool,
			ForWraparound:     forWraparound.Bool,
		}
		row.Dominant = row.DominantComponent()

		ret.Rows = append(ret.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getMaintenanceAutovacuumQueue | %w", err)
	}

	return ret, nil
}

func convertPgTimestampToTime(ts pgtype.Timestamp) *time.Time {
	if !ts.Valid {
		return nil
	}

	return &ts.Time
}
