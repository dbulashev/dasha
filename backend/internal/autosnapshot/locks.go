package autosnapshot

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/sanitize"
)

// maxLockRows caps stored blocked/blocking pairs (kept by wait desc) to bound the jsonb size.
const maxLockRows = 100

// maxCaptureDuration caps total CaptureLocks wall time so pathological probe settings
// (up to 20 × 5s) can't stall the tick loop or the manual-snapshot request.
const maxCaptureDuration = 15 * time.Second

// BackgroundPeak is the worst blocked-session count seen by the cheap background
// probe over the spike window (hybrid capture).
type BackgroundPeak struct {
	BlockedCount int       `json:"blocked_count"`
	At           time.Time `json:"at"`
}

// LockCapture is the lock graph stored alongside an auto-snapshot, persisted as
// snapshots.locks_data (jsonb).
type LockCapture struct {
	Captured       bool               `json:"captured"`
	Probes         int                `json:"probes,omitempty"`
	ProbeInterval  string             `json:"probe_interval,omitempty"`
	HarshestAt     *time.Time         `json:"harshest_at,omitempty"`
	BlockedCount   int                `json:"blocked_count"`
	MaxWaitMs      float64            `json:"max_wait_ms,omitempty"`
	BackgroundPeak *BackgroundPeak    `json:"background_peak,omitempty"`
	Rows           []dto.QueryBlocked `json:"rows,omitempty"`
	Error          string             `json:"error,omitempty"`
}

// blockedProber is the slice of Repo captureLocks needs (one method), so it can
// be unit-tested with a fake.
type blockedProber interface {
	GetQueriesBlocked(ctx context.Context, clusterName, instanceName, databaseName string) ([]dto.QueryBlocked, error)
}

// CaptureLocks runs probeCount probes spaced by interval, keeping the harshest (most
// blocked sessions, tie-broken by longest wait). Best-effort: all-probes-error returns
// Captured=false rather than failing the snapshot. Exported for reuse by the manual path.
func CaptureLocks(
	ctx context.Context,
	repo blockedProber,
	cluster, instance, database string,
	probeCount int,
	interval time.Duration,
) LockCapture {
	if probeCount < 1 {
		probeCount = 1
	}

	// Cap total wall time (see maxCaptureDuration); slack on top of the configured sleeps.
	budget := time.Duration(probeCount)*interval + 5*time.Second
	if budget > maxCaptureDuration {
		budget = maxCaptureDuration
	}

	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	var (
		best      []dto.QueryBlocked
		bestCount int
		bestWait  float64
		haveBest  bool
		lastErr   error
	)

	for i := range probeCount {
		if i > 0 && !sleep(ctx, interval) {
			lastErr = ctx.Err()

			break
		}

		rows, err := repo.GetQueriesBlocked(ctx, cluster, instance, database)
		if err != nil {
			lastErr = err

			continue
		}

		count, wait := scoreBlocked(rows)
		if !haveBest || harsher(count, wait, bestCount, bestWait) {
			best, bestCount, bestWait, haveBest = rows, count, wait, true
		}
	}

	if !haveBest {
		c := LockCapture{Captured: false, Probes: probeCount, ProbeInterval: interval.String()} //nolint:exhaustruct
		if lastErr != nil {
			c.Error = lastErr.Error()
		}

		return c
	}

	slices.SortFunc(best, func(a, b dto.QueryBlocked) int {
		return cmp.Compare(derefF(b.BlockedDurationMs), derefF(a.BlockedDurationMs)) // desc
	})

	if len(best) > maxLockRows {
		best = best[:maxLockRows]
	}

	// Redact SQL literals before persisting (same masking as the live blocked endpoint).
	for i := range best {
		best[i].BlockedQuery = sanitize.SQL(best[i].BlockedQuery)
		best[i].CurrentOrRecentQueryInBlockingProcess = sanitize.SQL(best[i].CurrentOrRecentQueryInBlockingProcess)
	}

	now := time.Now().UTC()

	return LockCapture{ //nolint:exhaustruct
		Captured:      true,
		Probes:        probeCount,
		ProbeInterval: interval.String(),
		HarshestAt:    &now,
		BlockedCount:  bestCount,
		MaxWaitMs:     bestWait,
		Rows:          best,
	}
}

// scoreBlocked returns (distinct blocked sessions, max blocked wait ms).
func scoreBlocked(rows []dto.QueryBlocked) (int, float64) {
	seen := map[int32]struct{}{}

	var maxWait float64

	for _, r := range rows {
		seen[r.BlockedPid] = struct{}{}
		if r.BlockedDurationMs != nil && *r.BlockedDurationMs > maxWait {
			maxWait = *r.BlockedDurationMs
		}
	}

	return len(seen), maxWait
}

// harsher reports whether (count,wait) is harsher than (bCount,bWait): more
// blocked sessions, tie-broken by longer wait.
func harsher(count int, wait float64, bCount int, bWait float64) bool {
	if count != bCount {
		return count > bCount
	}

	return wait > bWait
}

// sleep waits for d, returning false if ctx is cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func derefF(p *float64) float64 {
	if p == nil {
		return 0
	}

	return *p
}
