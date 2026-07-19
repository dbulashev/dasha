package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/query"
)

// GetHotSampleTables returns the raw cumulative activity counters of user
// tables on one host, plus the host's stats epoch and recovery role.
// schema/object narrow the sample to a single table when non-nil (the
// live-percentile path); the daemon's full sweep passes nils.
func (p *PgxPool) GetHotSampleTables(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	schema, object *string,
) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	return p.getHotSample(ctx, clusterName, instanceName, databaseName, schema, object, hotobjects.KindTable, enums.QueryHotSampleTables)
}

// GetHotSampleIndexes is GetHotSampleTables for indexes.
func (p *PgxPool) GetHotSampleIndexes(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	schema, object *string,
) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	return p.getHotSample(ctx, clusterName, instanceName, databaseName, schema, object, hotobjects.KindIndex, enums.QueryHotSampleIndexes)
}

func (p *PgxPool) getHotSample(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	schema, object *string,
	kind hotobjects.Kind,
	q enums.Query,
) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, nil, false, fmt.Errorf("getHotSample | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, nil, false, fmt.Errorf("get server version | %w", err)
	}

	qStr, err := query.Get(vNum, q, nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("getHotSample | %w", err)
	}

	return scanHotSample(ctx, pool, qStr, kind, schema, object)
}

func scanHotSample(
	ctx context.Context,
	pool *pgxpool.Pool,
	qStr string,
	kind hotobjects.Kind,
	schema, object *string,
) ([]hotobjects.AnchorRow, *time.Time, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := pool.Query(ctx, qStr, schema, object)
	if err != nil {
		return nil, nil, false, fmt.Errorf("scanHotSample | %w", err)
	}

	var (
		ret        []hotobjects.AnchorRow
		statsReset *time.Time
		inRecovery bool
	)

	for rows.Next() {
		var (
			a         hotobjects.AnchorRow
			tableName *string
			reset     pgtype.Timestamptz
			counters  []byte
		)

		if kind == hotobjects.KindIndex {
			err = rows.Scan(&a.Schema, &a.Object, &tableName, &a.SizeBytes, &reset, &inRecovery, &counters)
		} else {
			err = rows.Scan(&a.Schema, &a.Object, &a.SizeBytes, &reset, &inRecovery, &counters)
		}

		if err != nil {
			return nil, nil, false, fmt.Errorf("scanHotSample | %w", err)
		}

		a.Kind = kind

		if tableName != nil {
			a.TableName = *tableName
		}

		if reset.Valid {
			t := reset.Time
			statsReset = &t
			a.StatsReset = &t
		}

		if err := json.Unmarshal(counters, &a.Counters); err != nil {
			return nil, nil, false, fmt.Errorf("scanHotSample counters | %w", err)
		}

		ret = append(ret, a)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("scanHotSample | %w", err)
	}

	return ret, statsReset, inRecovery, nil
}
