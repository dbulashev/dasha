package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/statlock"
)

// GetLockSample reads pg_stat_lock of one host as a raw cumulative slice. The
// view is instance-wide, so the host's default pool is used.
func (p *PgxPool) GetLockSample(ctx context.Context, clusterName, instanceName string) (*statlock.Snapshot, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetLockSample | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	return p.getLockSample(ctx, vNum, pool)
}

func (p *PgxPool) getLockSample(ctx context.Context, vNum int, pool *pgxpool.Pool) (*statlock.Snapshot, error) {
	if vNum < statlock.MinVersionNum {
		return nil, statlock.ErrUnsupportedVersion
	}

	qStr, err := query.Get(vNum, enums.QueryLocksStatSnapshot, nil)
	if err != nil {
		return nil, fmt.Errorf("GetLockSample | %w", err)
	}

	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := pool.Query(qctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("GetLockSample | %w", err)
	}
	defer rows.Close()

	snap := statlock.Snapshot{CapturedAt: time.Now().UTC(), VersionNum: vNum}

	for rows.Next() {
		var (
			r     statlock.Row
			reset pgtype.Timestamptz
		)

		if err := rows.Scan(&r.LockType, &r.Waits, &r.WaitTimeMs, &r.FastpathExceeded, &reset,
			&snap.DeadlockTimeoutMs, &snap.MaxLocksPerTransaction); err != nil {
			return nil, fmt.Errorf("GetLockSample scan | %w", err)
		}

		if reset.Valid && (snap.StatsReset == nil || reset.Time.After(*snap.StatsReset)) {
			t := reset.Time
			snap.StatsReset = &t
		}

		snap.Rows = append(snap.Rows, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetLockSample | %w", err)
	}

	return &snap, nil
}

// GetServerVersionNum returns server_version_num of one host, cached per pool.
func (p *PgxPool) GetServerVersionNum(ctx context.Context, clusterName, instanceName string) (int, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return 0, fmt.Errorf("GetServerVersionNum | %w", err)
	}

	return p.getServerVersionNum(ctx, pool)
}
