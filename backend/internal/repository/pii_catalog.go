package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/piicatalog"
	"github.com/dbulashev/dasha/internal/query"
)

// Guards storage against pathological schemas.
const maxColumnsPerDatabase = 200_000

// GetColumnCatalog returns the columns of one database with their schema,
// table and comment, and the server_version_num of the instance. It reads pg_catalog
// only: no table data, no pg_stats.
func (p *PgxPool) GetColumnCatalog(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	excludeSchemas []string,
	stmtTimeout time.Duration,
) ([]piicatalog.Column, int, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, 0, fmt.Errorf("GetColumnCatalog | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, 0, fmt.Errorf("get server version | %w", err)
	}

	qStr, err := query.Get(vNum, enums.QueryPiiCatalogColumns, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("GetColumnCatalog | %w", err)
	}

	if excludeSchemas == nil {
		// A nil slice binds as NULL, and NOT (x LIKE ANY(NULL)) excludes everything.
		excludeSchemas = []string{}
	}

	var cols []piicatalog.Column

	settings := []string{fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", stmtTimeout.Milliseconds())}

	err = p.withTxSettings(ctx, pool, settings, func(q querier) error {
		cols, err = scanColumnCatalog(ctx, q, qStr, excludeSchemas)

		return err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("GetColumnCatalog | %w", err)
	}

	return cols, vNum, nil
}

func scanColumnCatalog(ctx context.Context, q querier, qStr string, excludeSchemas []string) ([]piicatalog.Column, error) {
	rows, err := q.Query(ctx, qStr, excludeSchemas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []piicatalog.Column

	for rows.Next() {
		if len(cols) >= maxColumnsPerDatabase {
			return nil, fmt.Errorf("too many columns (>%d)", maxColumnsPerDatabase)
		}

		var pr piicatalog.Column
		if err := rows.Scan(&pr.Schema, &pr.Table, &pr.Name, &pr.Comment); err != nil {
			return nil, err
		}

		cols = append(cols, pr)
	}

	return cols, rows.Err()
}
