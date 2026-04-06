package repository

import (
	"context"
	"fmt"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetHealthScoreMetrics(ctx context.Context, clusterName, instanceName string) (*dto.HealthScoreMetrics, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryCommonHealthScore, nil)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	var m dto.HealthScoreMetrics

	err = pool.QueryRow(ctx, qStr).Scan(
		&m.TotalConnections,
		&m.ActiveConnections,
		&m.IdleInTransaction,
		&m.LongestTransactionSeconds,
		&m.MaxConnections,
		&m.CacheHitRatio,
		&m.MaxDeadRatio,
		&m.AvgDeadRatio,
		&m.TablesHighBloat,
		&m.ReplicaCount,
		&m.MaxReplayLagSeconds,
		&m.MaxLagBytes,
		&m.DisconnectedReplicas,
		&m.MaxXidAge,
		&m.MaxVacuumAgeHours,
		&m.TablesNeverVacuumed,
	)
	if err != nil {
		return nil, fmt.Errorf("GetHealthScoreMetrics | %w", err)
	}

	return &m, nil
}
