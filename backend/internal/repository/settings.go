package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) GetPgSettings(ctx context.Context, clusterName, instanceName string, limit, offset int) ([]dto.PgSetting, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetPgSettings | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getPgSettings(ctx, vNum, pool, enums.QuerySettingsPgSettings, limit, offset)
}

func (p *PgxPool) GetAutovacuumSettings(ctx context.Context, clusterName, instanceName string) ([]dto.PgSetting, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetAutovacuumSettings | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getAutovacuumSettings(ctx, vNum, pool)
}

func (p *PgxPool) GetSettingsAnalyze(ctx context.Context, clusterName, instanceName string) ([]dto.SettingsNotification, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetSettingsAnalyze | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getSettingsAnalyze(ctx, vNum, pool)
}

func (p *PgxPool) getPgSettings(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	q enums.Query, limit,
	offset int,
) ([]dto.PgSetting, error) {
	qStr, err := query.Get(serverVersion, q, nil)
	if err != nil {
		return nil, fmt.Errorf("getPgSettings | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getPgSettings | %w", err)
	}

	ret := make([]dto.PgSetting, 0, limit)

	for rows.Next() {
		var (
			name, setting string
			unit          pgtype.Text
			source        string
		)

		err = rows.Scan(&name, &setting, &unit, &source)
		if err != nil {
			return nil, fmt.Errorf("getPgSettings | %w", err)
		}

		ret = append(ret, dto.PgSetting{
			Name:    name,
			Setting: setting,
			Unit:    unit.String,
			Source:  source,
		})
	}

	return ret, nil
}

func (p *PgxPool) getAutovacuumSettings(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.PgSetting, error) {
	qStr, err := query.Get(serverVersion, enums.QuerySettingsAutovacuumSettings, nil)
	if err != nil {
		return nil, fmt.Errorf("getAutovacuumSettings | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getAutovacuumSettings | %w", err)
	}

	ret := make([]dto.PgSetting, 0, 20) //nolint: mnd

	for rows.Next() {
		var (
			name, setting string
			unit          pgtype.Text
			source        string
		)

		err = rows.Scan(&name, &setting, &unit, &source)
		if err != nil {
			return nil, fmt.Errorf("getAutovacuumSettings | %w", err)
		}

		ret = append(ret, dto.PgSetting{
			Name:    name,
			Setting: setting,
			Unit:    unit.String,
			Source:  source,
		})
	}

	return ret, nil
}

func (p *PgxPool) getSettingsAnalyze(ctx context.Context, serverVersion int, pool *pgxpool.Pool) ([]dto.SettingsNotification, error) {
	qStr, err := query.Get(serverVersion, enums.QuerySettingsAnalyzeSettings, nil)
	if err != nil {
		return nil, fmt.Errorf("getSettingsAnalyze | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getSettingsAnalyze | %w", err)
	}

	ret := make([]dto.SettingsNotification, 0, 10) //nolint: mnd

	for rows.Next() {
		var (
			key    string
			params map[string]string
		)

		err = rows.Scan(&key, &params)
		if err != nil {
			return nil, fmt.Errorf("getSettingsAnalyze | %w", err)
		}

		ret = append(ret, dto.SettingsNotification{
			Key:    key,
			Params: params,
		})
	}

	return ret, nil
}
