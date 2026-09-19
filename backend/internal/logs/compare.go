package logs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/internal/logs/insights"
)

// CompareQuery reads two windows of plans and puts them side by side. ScanID
// takes the place of the current window: a scan already stored is not read
// again, and its window is the one compared.
type CompareQuery struct {
	PlansQuery
	BaseFrom, BaseTo time.Time
	ScanID           uuid.UUID
}

// ComparedWindow is one side of a comparison.
type ComparedWindow struct {
	From, To time.Time
	Result   ScanResult
}

// CompareResult holds both windows and the statements that changed between
// them. Baseline is nil when the current window found no plan: there is nothing
// to compare against, and the second read is not worth its budget. Partial
// means one of the windows was cut short, so a ratio of times is a ratio of two
// samples of unknown size.
type CompareResult struct {
	Current     ComparedWindow
	Baseline    *ComparedWindow
	Partial     bool
	Regressions []insights.Regression
}

// currentSide is the window a comparison starts from, with what the baseline
// read takes from it: the host it covers, the diagnosis its read was narrowed
// by, and whether its groups carry the indexes their plans read.
type currentSide struct {
	window       ComparedWindow
	rows         []insights.GroupRow
	host         string
	logging      planLogging
	indexesKnown bool
}

// Compare reads the plans of the baseline window and of the current one and
// lists the statements whose plans changed for the worse.
func (s *service) Compare(ctx context.Context, q CompareQuery) (CompareResult, error) {
	if !s.insights.IsEnabled() {
		return CompareResult{}, ErrDisabled
	}

	b, err := s.resolve(ctx, q.Cluster, q.Stream)
	if err != nil {
		return CompareResult{}, err
	}

	if err := validateCompare(b, q); err != nil {
		return CompareResult{}, err
	}

	s.logRead(ctx, "log plan compare", q.Cluster, b.sourceName, q.Stream)

	// Two reads and the settings behind them answer within the budget of one
	// window; the server closes a connection that outlives it.
	ctx, cancel := withBudget(ctx, s.readBudget())
	defer cancel()

	cur, err := s.currentWindow(ctx, b, q)
	if err != nil {
		return CompareResult{}, err
	}

	res := CompareResult{
		Current:     cur.window,
		Baseline:    nil,
		Partial:     cur.window.Result.Partial,
		Regressions: nil,
	}

	if len(cur.rows) == 0 {
		return res, nil
	}

	base := q.PlansQuery
	base.From, base.To = q.BaseFrom, q.BaseTo
	// A scan holds the plans of the host it read; a baseline of the whole
	// cluster would carry shapes and indexes that host never saw.
	base.Host = cur.host

	summary, groups, err := s.scanWindow(ctx, b, base, cur.logging)
	if err != nil {
		return CompareResult{}, err
	}

	summary.ScanID = s.saveScan(Scan{ //nolint:exhaustruct
		Kind:    ScanCompare,
		Cluster: q.Cluster,
		Stream:  q.Stream,
		Host:    base.Host,
		From:    base.From,
		To:      base.To,
		Result:  summary,
	}, groups)

	res.Baseline = &ComparedWindow{From: base.From, To: base.To, Result: withoutGroups(summary)}
	res.Partial = cur.window.Result.Partial || summary.Partial
	res.Regressions = pageOfRegressions(
		insights.Compare(cur.rows, groupRows(groups), cur.indexesKnown),
		planGroupLimit(q.Limit))

	return res, nil
}

func validateCompare(b binding, q CompareQuery) error {
	if err := validateWindow(b.cluster, q.BaseFrom, q.BaseTo, q.Host); err != nil {
		return err
	}

	if err := validateQueryIDRole(b, q.PlansQuery); err != nil {
		return err
	}

	if q.ScanID == uuid.Nil {
		return validateWindow(b.cluster, q.From, q.To, q.Host)
	}

	// The stored scan carries the window it read; a second one in the same
	// request would name a window nobody read.
	if !q.From.IsZero() || !q.To.IsZero() {
		return fmt.Errorf("%w: 'from' and 'to' cannot be combined with scan_id", ErrInvalid)
	}

	return nil
}

func (s *service) readBudget() time.Duration {
	return time.Duration(s.cfg.TimeoutSeconds) * time.Second
}

func withBudget(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, d)
}

// scanWindow reads one window of a comparison on half the budget, so the
// baseline keeps a share of it whatever the current window spends.
func (s *service) scanWindow(
	ctx context.Context,
	b binding,
	q PlansQuery,
	logging planLogging,
) (ScanResult, []insights.PlanGroup, error) {
	ctx, cancel := withBudget(ctx, s.readBudget()/2)
	defer cancel()

	return s.scanPlans(ctx, b, q, logging)
}

// currentWindow is the side the comparison starts from: a stored scan when one
// was named, else a fresh read.
func (s *service) currentWindow(ctx context.Context, b binding, q CompareQuery) (currentSide, error) {
	if q.ScanID != uuid.Nil {
		return s.storedWindow(ctx, b, q)
	}

	logging := s.planLogging(ctx, b.cluster, q.Host)

	res, groups, err := s.scanWindow(ctx, b, q.PlansQuery, logging)
	if err != nil {
		return currentSide{}, err
	}

	res.ScanID = s.saveScan(Scan{ //nolint:exhaustruct
		Kind:    ScanCompare,
		Cluster: q.Cluster,
		Stream:  q.Stream,
		Host:    q.Host,
		From:    q.From,
		To:      q.To,
		Result:  res,
	}, groups)

	return currentSide{
		window:       ComparedWindow{From: q.From, To: q.To, Result: withoutGroups(res)},
		rows:         groupRows(groups),
		host:         q.Host,
		logging:      logging,
		indexesKnown: true,
	}, nil
}

func (s *service) storedWindow(ctx context.Context, b binding, q CompareQuery) (currentSide, error) {
	store, err := s.snapshotStore()
	if err != nil {
		return currentSide{}, err
	}

	if err := s.awaitSnapshot(ctx, q.ScanID); err != nil {
		return currentSide{}, err
	}

	// The comparison walks every group of the scan, so the summary is asked for
	// none of them.
	scan, err := store.GetInsightsScan(ctx, q.ScanID, 0)
	if err != nil {
		return currentSide{}, err
	}

	if err := scanCovers(q, scan); err != nil {
		return currentSide{}, err
	}

	rows, err := store.AllInsightsGroups(ctx, q.ScanID)
	if err != nil {
		return currentSide{}, err
	}

	return currentSide{
		window:       ComparedWindow{From: scan.From, To: scan.To, Result: scan.Result},
		rows:         ofStatement(rows, q.PlansQuery),
		host:         scan.Host,
		logging:      s.planLogging(ctx, b.cluster, scan.Host),
		indexesKnown: recordsIndexes(rows),
	}, nil
}

// recordsIndexes says whether the groups carry the indexes their plans read. A
// scan stored before they did carries none, and an empty list there is not an
// index the window lost.
func recordsIndexes(rows []insights.GroupRow) bool {
	return slices.ContainsFunc(rows, func(r insights.GroupRow) bool { return len(r.Indexes) > 0 })
}

// scanCovers refuses a scan that read something else: both sides of a
// comparison cover the same cluster, stream and host.
func scanCovers(q CompareQuery, scan Scan) error {
	if scan.Cluster != q.Cluster || scan.Stream != q.Stream {
		return fmt.Errorf("%w: scan %s read %s/%s", ErrInvalid, q.ScanID, scan.Cluster, scan.Stream)
	}

	if q.Host != "" && q.Host != scan.Host {
		return fmt.Errorf("%w: scan %s covers host %q", ErrInvalid, q.ScanID, scan.Host)
	}

	return nil
}

// ofStatement keeps the groups of the one statement that was asked for; the
// stored scan may have read the whole window.
func ofStatement(rows []insights.GroupRow, q PlansQuery) []insights.GroupRow {
	if !q.HasQueryID {
		return rows
	}

	kept := make([]insights.GroupRow, 0, len(rows))

	for _, r := range rows {
		if r.HasQueryID && r.QueryID == q.QueryID {
			kept = append(kept, r)
		}
	}

	return kept
}

// withoutGroups leaves the plan groups of a window to its snapshot: a
// comparison answers with two summaries, and the trees in them would double an
// answer that is about the statements which changed.
func withoutGroups(res ScanResult) ScanResult {
	res.Plans.Groups = nil

	return res
}

func groupRows(groups []insights.PlanGroup) []insights.GroupRow {
	rows := make([]insights.GroupRow, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, g.Row())
	}

	return rows
}

func pageOfRegressions(rs []insights.Regression, limit int) []insights.Regression {
	if len(rs) > limit {
		return rs[:limit]
	}

	return rs
}
