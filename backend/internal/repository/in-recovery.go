package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetInstanceInfo(ctx context.Context, clusterName, instanceName string) (dto.InstanceInfo, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return dto.InstanceInfo{}, fmt.Errorf("GetInstanceInfo | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return dto.InstanceInfo{}, fmt.Errorf("get server version | %w", err)
	}

	inRecovery, err := p.getInRecovery(ctx, vNum, pool)
	if err != nil {
		return dto.InstanceInfo{}, fmt.Errorf("getInRecovery | %w", err)
	}

	v, _ := p.getServerVersion(ctx, pool)
	vFull, _ := p.getServerVersionFull(ctx, pool)

	return dto.InstanceInfo{
		InRecovery:  inRecovery,
		VersionNum:  vNum,
		Version:     v,
		VersionFull: vFull,
	}, nil
}

func (p *PgxPool) getInRecovery(ctx context.Context, serverVersion int, pool *pgxpool.Pool) (bool, error) {
	qStr, err := query.Get(serverVersion, enums.QueryCommonInRecovery, nil)
	if err != nil {
		return false, fmt.Errorf("getInRecovery | %w", err)
	}

	var b bool

	err = pool.QueryRow(ctx, qStr).Scan(&b)
	if err != nil {
		return false, fmt.Errorf("getInRecovery | %w", err)
	}

	return b, nil
}
