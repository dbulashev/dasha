package querystats

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNoLocks covers both a missing snapshot and one taken without a lock capture.
var ErrNoLocks = errors.New("querystats: snapshot has no lock capture")

type SnapshotListItem struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	Database       string // database the snapshot was read through, not its content
	Databases      []string
	DashaVersion   string
	JsonVersion    int
	PgssStatsReset *time.Time
	StatsSource    *string // extension read through; nil when not recorded
	HasLocks       bool
	Reason         string // "manual" or "auto:<trigger_type>"
}

// SnapshotIndex reports found=false from GetSnapshotLocks for a missing
// snapshot and for one without a lock capture alike.
type SnapshotIndex interface {
	ListSnapshots(ctx context.Context, cluster, instance string) ([]SnapshotListItem, error)
	GetSnapshotLocks(ctx context.Context, id uuid.UUID) ([]byte, bool, error)
}

type SnapshotCatalog struct {
	index SnapshotIndex
}

func NewSnapshotCatalog(index SnapshotIndex) *SnapshotCatalog {
	return &SnapshotCatalog{index: index}
}

func (c *SnapshotCatalog) List(ctx context.Context, cluster, instance string) ([]SnapshotListItem, error) {
	items, err := c.index.ListSnapshots(ctx, cluster, instance)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}

	return items, nil
}

// Locks returns the stored lock capture as raw JSON, or ErrNoLocks.
func (c *SnapshotCatalog) Locks(ctx context.Context, id uuid.UUID) ([]byte, error) {
	raw, ok, err := c.index.GetSnapshotLocks(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("snapshot locks: %w", err)
	}

	if !ok {
		return nil, ErrNoLocks
	}

	return raw, nil
}
