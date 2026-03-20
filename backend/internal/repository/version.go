package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
)

func (p *PgxPool) getServerVersionNum(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(0, enums.QueryCommonServerVersionNum, nil)
	if err != nil {
		return 0, fmt.Errorf("getServerVersionNum | %w", err)
	}

	var version string

	err = pool.QueryRow(ctx, qStr).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("getServerVersionNum | %w", err)
	}

	versionInt, err := strconv.Atoi(version)
	if err != nil {
		return 0, fmt.Errorf("getServerVersionNum | %w", err)
	}

	return versionInt, nil
}

func (p *PgxPool) getServerVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(0, enums.QueryCommonServerVersion, nil)
	if err != nil {
		return "", fmt.Errorf("getServerVersion | %w", err)
	}

	var version string

	err = pool.QueryRow(ctx, qStr).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("getServerVersion | %w", err)
	}

	return version, nil
}

func (p *PgxPool) getServerVersionFull(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(0, enums.QueryCommonServerVersionFull, nil)
	if err != nil {
		return "", fmt.Errorf("getServerVersionFull | %w", err)
	}

	var version string

	err = pool.QueryRow(ctx, qStr).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("getServerVersionFull | %w", err)
	}

	return version, nil
}
