package logs

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/logs/insights"
)

// PlansForQueryIDs reads a window for the plans of the given statements.
//
// It is what the index advisor asks for, and it answers ErrDisabled unless the
// feature is switched on: the report has to build exactly as it does on a
// cluster with no log source at all, and a caller that gets an error leaves its
// candidates saying evidence was never searched — which is not the same claim
// as having searched and found none.
//
// Nothing is pushed down per statement: a set of identifiers is not a filter a
// store runs, so the window is read once for every plan record in it and the
// identifiers are matched here. That also makes one scan serve a whole report
// rather than one per candidate.
func (s *service) PlansForQueryIDs(
	ctx context.Context,
	cluster, stream, database string,
	from, to time.Time,
	ids []int64,
) (insights.PlanWindow, error) {
	if !s.insights.IsEnabled() || !s.insights.IndexAdvisorEvidence {
		return insights.PlanWindow{}, ErrDisabled
	}

	if len(ids) == 0 {
		return insights.PlanWindow{}, nil
	}

	b, err := s.resolve(ctx, cluster, stream)
	if err != nil {
		return insights.PlanWindow{}, s.logEvidenceUnavailable(cluster, stream, err)
	}

	// The report answers for one database, and a statement identifier does not
	// tell two cloned databases apart. Plans that cannot be scoped to the
	// database are no evidence about it.
	if database == "" || b.fields.Database == "" {
		return insights.PlanWindow{}, s.logEvidenceUnavailable(cluster, stream,
			fmt.Errorf("%w: stream %q carries no database", ErrInvalid, stream))
	}

	q := PlansQuery{ //nolint:exhaustruct
		Cluster:  cluster,
		Stream:   stream,
		From:     from,
		To:       to,
		QueryIDs: ids,
		Database: database,
	}

	if err := validatePlansWindow(b, q); err != nil {
		return insights.PlanWindow{}, s.logEvidenceUnavailable(cluster, stream, err)
	}

	s.logRead(ctx, "log plan evidence", cluster, b.sourceName, stream)

	// No snapshot: this scan answers another report rather than a page the user
	// can come back to, and storing it would spend the daily volume on rows
	// nothing ever reads.
	res, groups, err := s.scanPlans(ctx, b, q, s.planLogging(ctx, b.cluster, q.Host))
	if err != nil {
		return insights.PlanWindow{}, s.logEvidenceUnavailable(cluster, stream, err)
	}

	if res.PlanRecords == 0 {
		s.logger.Info("index advisor plan evidence: no plans in window",
			zap.String("cluster", cluster),
			zap.String("service", stream),
			zap.String("reason", res.EmptyReason),
		)
	}

	return insights.PlanWindow{
		Groups:      groups,
		Partial:     res.Partial,
		PlanRecords: res.PlanRecords,
	}, nil
}

// logEvidenceUnavailable reports at Info what the report itself cannot say: the
// candidates come back with the evidence unsearched, and the reason is here or
// nowhere. ErrDisabled never reaches it — the default state is not an incident.
func (s *service) logEvidenceUnavailable(cluster, stream string, err error) error {
	s.logger.Info("index advisor plan evidence unavailable",
		zap.String("cluster", cluster),
		zap.String("service", stream),
		zap.Error(err),
	)

	return err
}
