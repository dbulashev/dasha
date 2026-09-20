package logs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/logs/insights"
)

// Kinds of snapshot, by the endpoint that took it.
const (
	ScanInsights = "insights"
	ScanPlans    = "plans"
	ScanCompare  = "compare"
)

// How a listing of groups is ordered.
const (
	GroupOrderSum   = "sum"
	GroupOrderMax   = "max"
	GroupOrderCount = "count"
)

const (
	defaultGroupPageSize = 50
	maxGroupPageSize     = 1000
)

const (
	// The write outlives the request that produced it, so it carries a deadline
	// of its own.
	snapshotWriteTimeout = 2 * time.Minute
	// Every pending write holds all groups of its scan in memory.
	maxPendingSnapshots = 8
)

// Scan is one stored read of a window. Result holds the summary; its plan
// groups are the ones a response carries, never the whole ranking.
type Scan struct {
	ID        uuid.UUID
	Kind      string
	Cluster   string
	Stream    string
	Host      string
	From, To  time.Time
	CreatedAt time.Time
	Result    ScanResult
}

// GroupsQuery pages the plan groups of one snapshot.
type GroupsQuery struct {
	ScanID  uuid.UUID
	QueryID *int64
	// WithFindings keeps the groups a rule fired on, including the count.
	WithFindings bool
	Order        string
	Limit        int
	Offset       int
}

// GroupPage is one page of plan groups; Total counts them all.
type GroupPage struct {
	Total int
	Rows  []insights.GroupRow
}

// SnapshotStore keeps the result of a scan so refining it reads the snapshot
// instead of the log source again.
type SnapshotStore interface {
	SaveInsightsScan(ctx context.Context, scan Scan, groups []insights.PlanGroup) error
	// GetInsightsScan returns the summary with the top groups of the scan.
	GetInsightsScan(ctx context.Context, id uuid.UUID, top int) (Scan, error)
	ListInsightsGroups(ctx context.Context, q GroupsQuery) (GroupPage, error)
	GetInsightsGroup(ctx context.Context, id uuid.UUID, ord int) (insights.PlanGroup, error)
	// AllInsightsGroups returns every group of a scan without its plan tree, for
	// a comparison that has to walk the whole ranking.
	AllInsightsGroups(ctx context.Context, id uuid.UUID) ([]insights.GroupRow, error)
}

func (s *service) Snapshot(ctx context.Context, id uuid.UUID) (Scan, error) {
	store, err := s.snapshotStore()
	if err != nil {
		return Scan{}, err
	}

	if err := s.awaitSnapshot(ctx, id); err != nil {
		return Scan{}, err
	}

	return store.GetInsightsScan(ctx, id, insightsTopGroups)
}

func (s *service) SnapshotGroups(ctx context.Context, q GroupsQuery) (GroupPage, error) {
	store, err := s.snapshotStore()
	if err != nil {
		return GroupPage{}, err
	}

	switch q.Order {
	case "":
		q.Order = GroupOrderSum
	case GroupOrderSum, GroupOrderMax, GroupOrderCount:
	default:
		return GroupPage{}, ErrInvalid
	}

	q.Limit, q.Offset = pageBounds(q.Limit, q.Offset)

	if err := s.awaitSnapshot(ctx, q.ScanID); err != nil {
		return GroupPage{}, err
	}

	return store.ListInsightsGroups(ctx, q)
}

func (s *service) SnapshotGroup(ctx context.Context, id uuid.UUID, ord int) (insights.PlanGroup, error) {
	store, err := s.snapshotStore()
	if err != nil {
		return insights.PlanGroup{}, err
	}

	if ord < 0 {
		return insights.PlanGroup{}, ErrInvalid
	}

	if err := s.awaitSnapshot(ctx, id); err != nil {
		return insights.PlanGroup{}, err
	}

	return store.GetInsightsGroup(ctx, id, ord)
}

func (s *service) snapshotStore() (SnapshotStore, error) {
	if !s.insights.IsEnabled() {
		return nil, ErrDisabled
	}

	if s.snapshots == nil {
		return nil, ErrNoStorage
	}

	return s.snapshots, nil
}

func pageBounds(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultGroupPageSize
	}

	return min(limit, maxGroupPageSize), max(offset, 0)
}

// saveScan stores the scan under a fresh id, all its groups included. The write
// runs in the background, so the answer does not wait for thousands of rows to
// land; a read of the id waits for it instead. Best-effort: with no storage, or
// with too many writes in flight, the response carries no scan id and the scan
// itself stands.
func (s *service) saveScan(scan Scan, groups []insights.PlanGroup) uuid.UUID {
	if s.snapshots == nil {
		return uuid.Nil
	}

	scan.ID = uuid.New()
	scan.CreatedAt = time.Now().UTC()
	scan.Result.ScanID = scan.ID
	scan.Result.Plans.Groups = nil

	done, ok := s.trackSnapshot(scan.ID)
	if !ok {
		s.logger.Warn("log insights: snapshot skipped, writes in flight",
			zap.String("cluster", scan.Cluster), zap.Int("pending", maxPendingSnapshots))

		return uuid.Nil
	}

	go s.writeSnapshot(scan, groups, done)

	return scan.ID
}

func (s *service) writeSnapshot(scan Scan, groups []insights.PlanGroup, done chan struct{}) {
	defer s.releaseSnapshot(scan.ID, done)

	ctx, cancel := context.WithTimeout(context.Background(), snapshotWriteTimeout)
	defer cancel()

	if err := s.snapshots.SaveInsightsScan(ctx, scan, groups); err != nil {
		s.logger.Warn("log insights: snapshot not stored",
			zap.String("cluster", scan.Cluster), zap.Error(err))
	}
}

func (s *service) trackSnapshot(id uuid.UUID) (chan struct{}, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	if len(s.pending) >= maxPendingSnapshots {
		return nil, false
	}

	done := make(chan struct{})
	s.pending[id] = done

	return done, true
}

func (s *service) releaseSnapshot(id uuid.UUID, done chan struct{}) {
	s.pendingMu.Lock()
	delete(s.pending, id)
	s.pendingMu.Unlock()

	close(done)
}

// awaitSnapshot blocks while the scan is still being written: a read that
// arrives before the rows land would otherwise miss them.
func (s *service) awaitSnapshot(ctx context.Context, id uuid.UUID) error {
	s.pendingMu.Lock()
	done := s.pending[id]
	s.pendingMu.Unlock()

	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
