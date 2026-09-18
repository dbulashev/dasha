package logs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/logs/insights"
)

// ScanInsights is the kind of a snapshot taken by Insights.
const ScanInsights = "insights"

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
	Result    InsightsResult
}

// GroupsQuery pages the plan groups of one snapshot.
type GroupsQuery struct {
	ScanID  uuid.UUID
	QueryID *int64
	Order   string
	Limit   int
	Offset  int
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
}

func (s *service) Snapshot(ctx context.Context, id uuid.UUID) (Scan, error) {
	store, err := s.snapshotStore()
	if err != nil {
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

// saveInsightsScan stores the scan under a fresh id, all its groups included.
// Best-effort: with no storage, or when the write fails, the response carries no
// scan id and the scan itself stands.
func (s *service) saveInsightsScan(
	ctx context.Context,
	q InsightsQuery,
	res InsightsResult,
	groups []insights.PlanGroup,
) uuid.UUID {
	if s.snapshots == nil {
		return uuid.Nil
	}

	scan := Scan{
		ID:        uuid.New(),
		Kind:      ScanInsights,
		Cluster:   q.Cluster,
		Stream:    q.Stream,
		Host:      q.Host,
		From:      q.From,
		To:        q.To,
		CreatedAt: time.Now().UTC(),
		Result:    res,
	}
	scan.Result.ScanID = scan.ID
	scan.Result.Plans.Groups = nil

	if err := s.snapshots.SaveInsightsScan(ctx, scan, groups); err != nil {
		s.logger.Warn("log insights: snapshot not stored",
			zap.String("cluster", q.Cluster), zap.Error(err))

		return uuid.Nil
	}

	return scan.ID
}
