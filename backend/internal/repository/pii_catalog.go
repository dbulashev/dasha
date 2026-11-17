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
const maxPairsPerDatabase = 200_000

// GetColumnCatalog returns the distinct (column name, comment) pairs of one
// database. It reads pg_catalog only: no table data, no pg_stats.
func (p *PgxPool) GetColumnCatalog(
	ctx context.Context,
	clusterName, instanceName, databaseName string,
	excludeSchemas []string,
	stmtTimeout time.Duration,
) ([]piicatalog.Pair, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, databaseName)
	if err != nil {
		return nil, fmt.Errorf("GetColumnCatalog | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	qStr, err := query.Get(vNum, enums.QueryPiiCatalogColumns, nil)
	if err != nil {
		return nil, fmt.Errorf("GetColumnCatalog | %w", err)
	}

	if excludeSchemas == nil {
		excludeSchemas = []string{}
	}

	var pairs []piicatalog.Pair

	settings := []string{fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", stmtTimeout.Milliseconds())}

	err = p.withTxSettings(ctx, pool, settings, func(q querier) error {
		pairs, err = scanColumnCatalog(ctx, q, qStr, excludeSchemas)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("GetColumnCatalog | %w", err)
	}

	return pairs, nil
}

func scanColumnCatalog(ctx context.Context, q querier, qStr string, excludeSchemas []string) ([]piicatalog.Pair, error) {
	rows, err := q.Query(ctx, qStr, excludeSchemas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []piicatalog.Pair

	for rows.Next() {
		if len(pairs) >= maxPairsPerDatabase {
			return nil, fmt.Errorf("too many column/comment pairs (>%d)", maxPairsPerDatabase)
		}

		var pr piicatalog.Pair
		if err := rows.Scan(&pr.Name, &pr.Comment); err != nil {
			return nil, err
		}

		pairs = append(pairs, pr)
	}

	return pairs, rows.Err()
}
