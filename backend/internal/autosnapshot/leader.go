package autosnapshot

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const (
	heartbeatInterval   = 10 * time.Second
	leaderRetryInterval = 30 * time.Second
)

type leaderStorage interface {
	TryAcquireLeaderLock(ctx context.Context) (*pgx.Conn, bool, error)
	UpdateLeaderHeartbeat(ctx context.Context, instanceID string) error
}

// Leader holds an advisory lock and emits heartbeats until the context is cancelled.
type Leader struct {
	storage    leaderStorage
	logger     *zap.Logger
	instanceID string

	conn *pgx.Conn
}

func NewLeader(storage leaderStorage, logger *zap.Logger) *Leader {
	host, _ := os.Hostname()
	return &Leader{
		storage:    storage,
		logger:     logger,
		instanceID: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), time.Now().UTC().Format(time.RFC3339)),
	}
}

// Acquire blocks until the advisory lock is held or ctx is cancelled.
func (l *Leader) Acquire(ctx context.Context) error {
	for {
		conn, ok, err := l.storage.TryAcquireLeaderLock(ctx)
		if err != nil {
			l.logger.Warn("acquire leader lock failed, will retry", zap.Error(err))
		} else if ok {
			l.conn = conn
			l.logger.Info("leader acquired", zap.String("instance_id", l.instanceID))
			return nil
		} else {
			l.logger.Info("leader busy, waiting", zap.Duration("retry_in", leaderRetryInterval))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(leaderRetryInterval):
		}
	}
}

// RunHeartbeat writes heartbeat until ctx is done. Call after Acquire.
func (l *Leader) RunHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	if err := l.storage.UpdateLeaderHeartbeat(ctx, l.instanceID); err != nil {
		l.logger.Warn("initial heartbeat failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.storage.UpdateLeaderHeartbeat(ctx, l.instanceID); err != nil {
				l.logger.Warn("heartbeat failed", zap.Error(err))
			}
		}
	}
}

// Release releases the advisory lock connection back to the pool; PostgreSQL
// drops the session-level lock automatically when the connection closes.
func (l *Leader) Release() {
	if l.conn == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.conn.Close(ctx); err != nil {
		l.logger.Warn("leader conn close failed", zap.Error(err))
	}

	l.conn = nil
}

// InstanceID returns the leader instance identifier.
func (l *Leader) InstanceID() string {
	return l.instanceID
}
