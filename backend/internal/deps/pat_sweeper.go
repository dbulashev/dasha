package deps

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/storage"
)

const (
	// patSweepInterval is how often idle tokens are swept. It does not need to be
	// tight: an idle token stops authenticating the moment it crosses the cutoff
	// (ResolveAPIToken checks it), so the sweep only writes the revocation down
	// for the audit trail and the UI.
	patSweepInterval = 6 * time.Hour

	// patSweepTimeout bounds one sweep so a stalled storage backend cannot leave
	// the goroutine hanging until the next tick.
	patSweepTimeout = 30 * time.Second
)

// RunIdleTokenSweeper revokes personal access tokens left unused for longer than
// storage.IdleRevokeDays, on startup and then periodically, until ctx is
// cancelled. A nil storage means PAT auth is disabled, so there is nothing to
// sweep. Failures are logged and retried on the next tick — this is a hygiene
// task, not something worth taking the server down for.
func RunIdleTokenSweeper(ctx context.Context, st *storage.Storage, logger *zap.Logger) {
	if st == nil {
		return
	}

	sweep := func() {
		sweepCtx, cancel := context.WithTimeout(ctx, patSweepTimeout)
		defer cancel()

		// Without the migration there is no api_tokens table, and every sweep would
		// log an error for a feature the deployment is not using.
		if !st.APITokensReady(sweepCtx) {
			return
		}

		revoked, err := st.RevokeIdleAPITokens(sweepCtx)
		if err != nil {
			logger.Warn("idle token sweep failed", zap.Error(err))

			return
		}

		if revoked > 0 {
			logger.Info("revoked idle personal access tokens",
				zap.Int64("count", revoked),
				zap.Int("idle_days", storage.IdleRevokeDays),
			)
		}
	}

	sweep()

	ticker := time.NewTicker(patSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
