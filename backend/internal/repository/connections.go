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

func (p *PgxPool) GetConnectionSources(
	ctx context.Context,
	clusterName,
	instanceName string,
	limit,
	offset int,
) ([]dto.ConnectionSources, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetConnectionSources | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getConnectionSources(ctx, vNum, pool, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getConnectionSources | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetConnectionStates(ctx context.Context, clusterName, instanceName string) ([]dto.ConnectionStates, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetConnectionStates | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getConnectionStates(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getConnectionStates | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetConnectionStatActivity(
	ctx context.Context,
	clusterName,
	instanceName string,
	limit,
	offset int,
	username,
	state string,
) ([]dto.ConnectionStatActivity, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetConnectionStatActivity | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	if offset < 0 {
		offset = 0
	}

	ret, err := p.getConnectionStatActivity(ctx, vNum, pool, limit, offset, username, state)
	if err != nil {
		return nil, fmt.Errorf("getConnectionStatActivity | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getConnectionSources(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit,
	offset int,
) ([]dto.ConnectionSources, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryConnectionsConnectionSources, nil)
	if err != nil {
		return nil, fmt.Errorf("getConnectionSources | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("getConnectionSources | %w", err)
	}

	ret := make([]dto.ConnectionSources, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			database, user, source, ip pgtype.Text
			totalConnections           int64
		)

		err = rows.Scan(&database, &user, &source, &ip, &totalConnections)
		if err != nil {
			return nil, fmt.Errorf("getConnectionSources | %w", err)
		}

		ret = append(ret, dto.ConnectionSources{
			Database:         database.String,
			Username:         user.String,
			ApplicationName:  source.String,
			ClientAddr:       ip.String,
			TotalConnections: totalConnections,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getConnectionSources | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getConnectionStates(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.ConnectionStates, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryConnectionsConnectionStates, nil)
	if err != nil {
		return nil, fmt.Errorf("getConnectionStates | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getConnectionStates | %w", err)
	}

	ret := make([]dto.ConnectionStates, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			state pgtype.Text
			count int64
		)

		err = rows.Scan(&state, &count)
		if err != nil {
			return nil, fmt.Errorf("getConnectionStates | %w", err)
		}

		ret = append(ret, dto.ConnectionStates{
			State: state.String,
			Count: count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getConnectionStates | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getConnectionStatActivity(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	limit, offset int,
	username,
	state string,
) ([]dto.ConnectionStatActivity, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryConnectionsStatActivity, nil)
	if err != nil {
		return nil, fmt.Errorf("getConnectionStatActivity | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, limit, offset, username, state)
	if err != nil {
		return nil, fmt.Errorf("getConnectionStatActivity | %w", err)
	}

	ret := make([]dto.ConnectionStatActivity, 0, limit)

	for rows.Next() {
		var (
			pid                                                                 int
			database, username, applicationName, clientAddr, state, backendType pgtype.Text
			ssl                                                                 pgtype.Bool
		)

		err = rows.Scan(&pid, &database, &username, &applicationName, &clientAddr, &state, &ssl, &backendType)
		if err != nil {
			return nil, fmt.Errorf("getConnectionStatActivity | %w", err)
		}

		ret = append(ret, dto.ConnectionStatActivity{
			Pid:             pid,
			Database:        database.String,
			UserName:        username.String,
			ApplicationName: applicationName.String,
			ClientAddr:      clientAddr.String,
			State:           state.String,
			Ssl:             ssl.Bool,
			BackendType:     backendType.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getConnectionStatActivity | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetConnectionWaitEvents(ctx context.Context, clusterName, instanceName string) ([]dto.WaitEvent, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetConnectionWaitEvents | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getConnectionWaitEvents(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getConnectionWaitEvents | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getConnectionWaitEvents(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.WaitEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryConnectionsWaitEvents, nil)
	if err != nil {
		return nil, fmt.Errorf("getConnectionWaitEvents | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getConnectionWaitEvents | %w", err)
	}

	ret := make([]dto.WaitEvent, 0, 10) //nolint:mnd

	for rows.Next() {
		var (
			waitEventType string
			waitEvent     string
			count         int64
		)

		err = rows.Scan(&waitEventType, &waitEvent, &count)
		if err != nil {
			return nil, fmt.Errorf("getConnectionWaitEvents | %w", err)
		}

		ret = append(ret, dto.WaitEvent{
			WaitEventType: waitEventType,
			WaitEvent:     waitEvent,
			Count:         count,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getConnectionWaitEvents | %w", err)
	}

	return ret, nil
}
