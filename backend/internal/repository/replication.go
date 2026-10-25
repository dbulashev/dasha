package repository

import (
	"context"
	"fmt"
	"time"

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
			syncState        pgtype.Text
			slotName         string
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
			SlotName:         slotName,
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

func (p *PgxPool) GetReplicationWalPosition(ctx context.Context, clusterName, instanceName string) (*dto.WalPosition, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetReplicationWalPosition | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(vNum, enums.QueryReplicationWalPosition, nil)
	if err != nil {
		return nil, fmt.Errorf("GetReplicationWalPosition | %w", err)
	}

	var (
		lsn pgtype.Text
		ret dto.WalPosition
	)

	if err := pool.QueryRow(ctx, qStr).Scan(&lsn, &ret.CapturedAt); err != nil {
		return nil, fmt.Errorf("GetReplicationWalPosition | %w", err)
	}

	if lsn.Valid {
		ret.Lsn = &lsn.String
	}

	return &ret, nil
}

func (p *PgxPool) GetReplicationSlotsForecast(
	ctx context.Context,
	clusterName, instanceName, fromLsn string,
	fromTime time.Time,
) (*dto.ReplicationSlotsForecast, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetReplicationSlotsForecast | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	ret, err := p.getReplicationSlotsForecast(ctx, vNum, pool, fromLsn, fromTime)
	if err != nil {
		return nil, fmt.Errorf("getReplicationSlotsForecast | %w", err)
	}

	return ret, nil
}

func (p *PgxPool) getReplicationSlotsForecast(
	ctx context.Context,
	serverVersion int,
	pool *pgxpool.Pool,
	fromLsn string,
	fromTime time.Time,
) (*dto.ReplicationSlotsForecast, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	qStr, err := query.Get(serverVersion, enums.QueryReplicationSlotsForecast, nil)
	if err != nil {
		return nil, fmt.Errorf("getReplicationSlotsForecast | %w", err)
	}

	rows, err := pool.Query(ctx, qStr, fromLsn, fromTime)
	if err != nil {
		return nil, fmt.Errorf("getReplicationSlotsForecast | %w", err)
	}

	ret := &dto.ReplicationSlotsForecast{Slots: make([]dto.ReplicationSlotForecast, 0, 4)} //nolint:mnd

	for rows.Next() {
		var (
			elapsedSeconds, walBytesPerSecond pgtype.Float8
			maxSlotWalKeepSize                pgtype.Int8
			slotName, slotType                pgtype.Text
			database, walStatus               pgtype.Text
			active                            pgtype.Bool
			retainedBytes, safeWalSize        pgtype.Int8
			secondsToLost                     pgtype.Float8
		)

		err = rows.Scan(&elapsedSeconds, &walBytesPerSecond, &maxSlotWalKeepSize,
			&slotName, &slotType, &database, &active, &walStatus,
			&retainedBytes, &safeWalSize, &secondsToLost)
		if err != nil {
			return nil, fmt.Errorf("getReplicationSlotsForecast | %w", err)
		}

		if elapsedSeconds.Valid {
			ret.ElapsedSeconds = &elapsedSeconds.Float64
		}

		if walBytesPerSecond.Valid {
			ret.WalBytesPerSecond = &walBytesPerSecond.Float64
		}

		if maxSlotWalKeepSize.Valid {
			ret.MaxSlotWalKeepSize = &maxSlotWalKeepSize.Int64
		}

		// The query always yields one row; a NULL slot_name means there are no slots.
		if !slotName.Valid {
			continue
		}

		item := dto.ReplicationSlotForecast{
			SlotName:  slotName.String,
			SlotType:  slotType.String,
			Database:  database.String,
			Active:    active.Bool,
			WalStatus: walStatus.String,
		}

		if retainedBytes.Valid {
			item.RetainedBytes = &retainedBytes.Int64
		}

		if safeWalSize.Valid {
			item.SafeWalSize = &safeWalSize.Int64
		}

		if secondsToLost.Valid {
			item.SecondsToLost = &secondsToLost.Float64
		}

		ret.Slots = append(ret.Slots, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getReplicationSlotsForecast | %w", err)
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
