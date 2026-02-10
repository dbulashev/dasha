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

func (p *PgxPool) GetReplicationStatus(ctx context.Context, clusterName, instanceName string) ([]dto.ReplicationStatus, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetReplicationStatus | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getReplicationStatus(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getReplicationStatus | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getReplicationStatus(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.ReplicationStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryReplicationStatus, nil)
	if err != nil {
		return nil, fmt.Errorf("getReplicationStatus | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getReplicationStatus | %w", err)
	}

	ret := make([]dto.ReplicationStatus, 0, 4) //nolint:mnd

	for rows.Next() {
		var (
			pid              int
			usename          pgtype.Text
			applicationName  pgtype.Text
			clientAddr       pgtype.Text
			state            pgtype.Text
			sentLsn          pgtype.Text
			writeLsn         pgtype.Text
			flushLsn         pgtype.Text
			replayLsn        pgtype.Text
			writeLagSeconds  float64
			flushLagSeconds  float64
			replayLagSeconds float64
			replayLagBytes   pgtype.Int8
			syncState pgtype.Text
			slotName  string
		)

		err = rows.Scan(
			&pid, &usename, &applicationName, &clientAddr, &state,
			&sentLsn, &writeLsn, &flushLsn, &replayLsn,
			&writeLagSeconds, &flushLagSeconds, &replayLagSeconds,
			&replayLagBytes, &syncState, &slotName,
		)
		if err != nil {
			return nil, fmt.Errorf("getReplicationStatus | %w", err)
		}

		ret = append(ret, dto.ReplicationStatus{
			Pid:              pid,
			Usename:          usename.String,
			ApplicationName:  applicationName.String,
			ClientAddr:       clientAddr.String,
			State:            state.String,
			SentLsn:          sentLsn.String,
			WriteLsn:         writeLsn.String,
			FlushLsn:         flushLsn.String,
			ReplayLsn:        replayLsn.String,
			WriteLagSeconds:  writeLagSeconds,
			FlushLagSeconds:  flushLagSeconds,
			ReplayLagSeconds: replayLagSeconds,
			ReplayLagBytes:   replayLagBytes.Int64,
			SyncState:        syncState.String,
			SlotName: slotName,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getReplicationStatus | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetReplicationSlots(ctx context.Context, clusterName, instanceName string) ([]dto.ReplicationSlot, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetReplicationSlots | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getReplicationSlots(ctx, vNum, pool)
	if err != nil {
		return nil, fmt.Errorf("getReplicationSlots | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getReplicationSlots(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
) ([]dto.ReplicationSlot, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryReplicationSlots, nil)
	if err != nil {
		return nil, fmt.Errorf("getReplicationSlots | %w", err)
	}

	rows, err := pool.Query(ctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("getReplicationSlots | %w", err)
	}

	ret := make([]dto.ReplicationSlot, 0, 4) //nolint:mnd

	for rows.Next() {
		var (
			slotName     string
			slotType     string
			database     string
			active       bool
			walStatus    string
			safeWalSize  pgtype.Int8
			backlogBytes pgtype.Int8
		)

		err = rows.Scan(&slotName, &slotType, &database, &active, &walStatus, &safeWalSize, &backlogBytes)
		if err != nil {
			return nil, fmt.Errorf("getReplicationSlots | %w", err)
		}

		item := dto.ReplicationSlot{
			SlotName:  slotName,
			SlotType:  slotType,
			Database:  database,
			Active:    active,
			WalStatus: walStatus,
		}

		if safeWalSize.Valid {
			item.SafeWalSize = &safeWalSize.Int64
		}

		if backlogBytes.Valid {
			item.BacklogBytes = &backlogBytes.Int64
		}

		ret = append(ret, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getReplicationSlots | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) GetReplicationConfig(ctx context.Context, clusterName, instanceName string) (*dto.ReplicationConfig, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetReplicationConfig | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var syncStandbyNames, syncCommit string

	err = pool.QueryRow(ctx,
		"SELECT current_setting('synchronous_standby_names'), current_setting('synchronous_commit')",
	).Scan(&syncStandbyNames, &syncCommit)
	if err != nil {
		return nil, fmt.Errorf("GetReplicationConfig | %w", err)
	}

	return &dto.ReplicationConfig{
		SynchronousStandbyNames: syncStandbyNames,
		SynchronousCommit:       syncCommit,
	}, nil
}
