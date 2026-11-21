package querystats

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
)

var (
	ErrSnapshotNotFound = errors.New("querystats: snapshot not found")
	ErrIncomparable     = errors.New("querystats: snapshots of different generations cannot be compared")
)

type Snapshot struct {
	Reports []dto.QueryReport

	// Attributed is true when every row names its database. Storage derives it
	// from the stored format, so no format number leaks out of storage.
	Attributed bool
}

// SnapshotStore returns ErrSnapshotNotFound for an unknown id.
type SnapshotStore interface {
	GetSnapshotReport(ctx context.Context, id uuid.UUID) (Snapshot, error)
}

type LiveSource interface {
	GetQueriesReport(ctx context.Context, cluster, instance, database string, excludeUsers []string, queryID *int64) ([]dto.QueryReport, error)
}

type CompareRequest struct {
	SnapshotA    uuid.UUID
	SnapshotB    *uuid.UUID // nil — compare against live statistics
	Cluster      string
	Instance     string
	Database     string
	Scope        *string
	ExcludeUsers []string
}

// Pair is one statement on both sides, each already narrowed to the scope.
// Key and Query come from whichever side has them, the left one first.
type Pair struct {
	Key         Key
	Query       string
	Left, Right *dto.QueryReport
}

type Service struct {
	snaps SnapshotStore
	live  LiveSource
}

func NewService(snaps SnapshotStore, live LiveSource) *Service {
	return &Service{snaps: snaps, live: live}
}

// Compare pairs snapshot A with snapshot B or, without B, with live
// statistics. A LiveSource error is returned wrapped, dto.ErrNotFound included.
func (s *Service) Compare(ctx context.Context, req CompareRequest) ([]Pair, error) {
	a, err := s.snaps.GetSnapshotReport(ctx, req.SnapshotA)
	if err != nil {
		return nil, fmt.Errorf("snapshot A: %w", err)
	}

	// Live statistics always name the database of each row, so an absent B is
	// the newer generation.
	b := Snapshot{Attributed: true} //nolint:exhaustruct

	if req.SnapshotB != nil {
		b, err = s.snaps.GetSnapshotReport(ctx, *req.SnapshotB)
		if err != nil {
			return nil, fmt.Errorf("snapshot B: %w", err)
		}
	}

	// Decided before the live report is read: on a refusal its rows are thrown
	// away, and it is a full pass over pg_stat_statements.
	scope, pairable := compareScope(a.Attributed, b.Attributed, req.Scope, req.Database)
	if !pairable {
		return nil, ErrIncomparable
	}

	if req.SnapshotB == nil {
		b.Reports, err = s.live.GetQueriesReport(ctx, req.Cluster, req.Instance, req.Database, req.ExcludeUsers, nil)
		if err != nil {
			return nil, fmt.Errorf("live report: %w", err)
		}

		// The live SQL already excludes these users from B; snapshot A was captured
		// unfiltered, so drop its solely-excluded queries too or they'd survive the join.
		a.Reports = excludeReportsByUser(a.Reports, req.ExcludeUsers)
	}

	joined := mapstruct.SliceFullJoin(
		ScopeReport(a.Reports, scope, req.Database),
		ScopeReport(b.Reports, scope, req.Database),
		KeyOf, KeyOf,
	)

	pairs := make([]Pair, 0, len(joined))

	for _, j := range joined {
		side := j.Left
		if side == nil {
			side = j.Right
		}

		p := Pair{Left: j.Left, Right: j.Right, Key: KeyOf(*side), Query: side.Query}
		if p.Query == "" && j.Right != nil {
			p.Query = j.Right.Query
		}

		pairs = append(pairs, p)
	}

	return pairs, nil
}

// Snapshot reads a stored snapshot narrowed to the requested scope.
func (s *Service) Snapshot(ctx context.Context, id uuid.UUID, scope *string, database string) ([]dto.QueryReport, error) {
	snap, err := s.snaps.GetSnapshotReport(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}

	// Rows of a pre-attribution snapshot name no database; narrowing one would
	// filter it down to nothing, so it stays instance-wide.
	sc := ResolveScope(scope, database)
	if !snap.Attributed {
		sc = dto.ScopeInstance
	}

	return ScopeReport(snap.Reports, sc, database), nil
}
