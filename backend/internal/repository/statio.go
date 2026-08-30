package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/query"
	"github.com/dbulashev/dasha/internal/statio"
)

// GetIOSample reads the whole pg_stat_io matrix of one host as a raw cumulative
// slice. pg_stat_io is instance-wide, so the host's default pool is used and no
// database narrows the answer.
func (p *PgxPool) GetIOSample(ctx context.Context, clusterName, instanceName string) (*statio.Snapshot, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, fmt.Errorf("GetIOSample | %w", err)
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get server version | %w", err)
	}

	if vNum < statio.MinVersionNum {
		return nil, statio.ErrUnsupportedVersion
	}

	qStr, err := query.Get(vNum, enums.QueryStatioSnapshot, nil)
	if err != nil {
		return nil, fmt.Errorf("GetIOSample | %w", err)
	}

	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := pool.Query(qctx, qStr)
	if err != nil {
		return nil, fmt.Errorf("GetIOSample | %w", err)
	}
	defer rows.Close()

	snap := statio.Snapshot{CapturedAt: time.Now().UTC(), VersionNum: vNum}

	for rows.Next() {
		var (
			r        statio.Row
			reset    pgtype.Timestamptz
			opBytes  pgtype.Int4
			counters []byte
		)

		if err := rows.Scan(&r.BackendType, &r.Object, &r.Context, &reset,
			&snap.TrackIOTiming, &snap.TrackWALIOTiming, &opBytes, &counters); err != nil {
			return nil, fmt.Errorf("GetIOSample scan | %w", err)
		}

		if reset.Valid {
			t := reset.Time
			snap.StatsReset = &t
		}

		if opBytes.Valid {
			v := int(opBytes.Int32)
			snap.OpBytes = &v
		}

		if err := json.Unmarshal(counters, &r.Values); err != nil {
			return nil, fmt.Errorf("GetIOSample counters | %w", err)
		}

		snap.Rows = append(snap.Rows, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetIOSample | %w", err)
	}

	return &snap, nil
}
