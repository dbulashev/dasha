package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dbulashev/dasha/internal/activity"
	"github.com/dbulashev/dasha/internal/enums"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
	"github.com/dbulashev/dasha/internal/query"
)

const activityTextMaxRunes = 2000

// GetActivitySample reads the non-idle sessions of one host. The caller's
// context bounds the whole read.
func (p *PgxPool) GetActivitySample(
	ctx context.Context,
	clusterName, instanceName string,
	maxSessions int,
) (activity.Sample, error) {
	pool, vNum, err := p.activityPool(ctx, clusterName, instanceName)
	if err != nil {
		return activity.Sample{}, fmt.Errorf("GetActivitySample | %w", err)
	}

	smp, err := p.getActivitySample(ctx, vNum, pool, maxSessions)
	if err != nil {
		return activity.Sample{}, fmt.Errorf("GetActivitySample | %w", err)
	}

	return smp, nil
}

func (p *PgxPool) GetActivityAccess(ctx context.Context, clusterName, instanceName string) (activity.Access, error) {
	pool, vNum, err := p.activityPool(ctx, clusterName, instanceName)
	if err != nil {
		return activity.Access{}, fmt.Errorf("GetActivityAccess | %w", err)
	}

	acc, err := getActivityAccess(ctx, vNum, pool)
	if err != nil {
		return activity.Access{}, fmt.Errorf("GetActivityAccess | %w", err)
	}

	return acc, nil
}

// activityPool uses the host's default pool: pg_stat_activity is instance-wide.
func (p *PgxPool) activityPool(ctx context.Context, clusterName, instanceName string) (*pgxpool.Pool, int, error) {
	pool, err := p.getPoolByClusterNameAndInstance(ctx, clusterName, instanceName, "")
	if err != nil {
		return nil, 0, err
	}

	vNum, err := p.getServerVersionNum(ctx, pool)
	if err != nil {
		return nil, 0, fmt.Errorf("get server version | %w", err)
	}

	if vNum < activity.MinVersionNum {
		return nil, 0, activity.ErrUnsupportedVersion
	}

	return pool, vNum, nil
}

func (p *PgxPool) getActivitySample(
	ctx context.Context,
	serverVersion int,
	pool querier,
	maxSessions int,
) (activity.Sample, error) {
	qStr, err := query.Get(serverVersion, enums.QueryActivitySample, nil)
	if err != nil {
		return activity.Sample{}, err
	}

	rows, err := pool.Query(ctx, qStr, maxSessions+1)
	if err != nil {
		return activity.Sample{}, err
	}
	defer rows.Close()

	var smp activity.Sample

	for rows.Next() {
		var (
			s                     activity.Session
			pid                   pgtype.Int4
			queryStart, xactStart pgtype.Timestamptz
		)

		if err := rows.Scan(
			&smp.At, &pid, &s.BackendType, &s.Database, &s.User, &s.Application, &s.ClientAddr,
			&s.State, &s.WaitType, &s.WaitEvent, &s.QueryID, &s.Text, &queryStart, &xactStart, &s.HoldsXmin,
		); err != nil {
			return activity.Sample{}, fmt.Errorf("scan | %w", err)
		}

		// A row without a pid carries only now(): the host has no sessions to report.
		if !pid.Valid {
			continue
		}

		s.PID = pid.Int32

		if queryStart.Valid {
			s.QueryStart = queryStart.Time
		}

		if xactStart.Valid {
			s.XactStart = xactStart.Time
		}

		smp.Sessions = append(smp.Sessions, s)
	}

	if err := rows.Err(); err != nil {
		return activity.Sample{}, err
	}

	if len(smp.Sessions) > maxSessions {
		smp.Sessions = smp.Sessions[:maxSessions]
		smp.Truncated = true
	}

	parser := p.indexAdvisorParser()

	for i := range smp.Sessions {
		s := &smp.Sessions[i]
		s.Text = sanitize.SQL(s.Text)
		s.Key, s.KeyKind = activity.QueryKey(*s, parser)
		s.Text = activity.TruncateText(s.Text, activityTextMaxRunes)
	}

	return smp, nil
}

func getActivityAccess(ctx context.Context, serverVersion int, pool querier) (activity.Access, error) {
	qStr, err := query.Get(serverVersion, enums.QueryActivityAccess, statsPreloadedTemplateData{
		StatsPreloaded: query.StatsPreloaded(1),
	})
	if err != nil {
		return activity.Access{}, err
	}

	var (
		acc            activity.Access
		computeQueryID string
		pgssPreloaded  bool
	)

	row := pool.QueryRow(ctx, qStr,
		statsSettings(func(d statsSourceDef) string { return d.MaxSetting }),
		statsSettings(func(d statsSourceDef) string { return d.TrackUtility }))
	if err := row.Scan(&acc.CanReadAllStats, &computeQueryID, &pgssPreloaded, &acc.TrackUtility); err != nil {
		return activity.Access{}, err
	}

	acc.QueryIDAvailable = activity.QueryIDAvailable(computeQueryID, pgssPreloaded)

	return acc, nil
}
