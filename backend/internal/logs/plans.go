package logs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// planPhrase is the phrase every auto_explain record holds. The colon is left
// out: a standard analyzer drops it, and a phrase carrying it would match
// nothing where the store tokenizes the message.
const planPhrase = "plan"

// What a scan asked the log store to filter on. Without it an empty answer
// cannot be told apart from a store that dropped the records itself.
const (
	NarrowedSeverity = "severity"
	NarrowedQueryID  = "query_id"
	NarrowedText     = "text"
)

const (
	defaultPlanGroups = 10
	maxPlanGroups     = 100
)

// narrowTimeout bounds what the provider spends deciding what it can filter
// itself: the metadata call of a store must not eat the budget of the scan.
const narrowTimeout = 5 * time.Second

// PlansQuery reads the plans of a window, of one statement when QueryID is set.
type PlansQuery struct {
	Cluster    string
	Stream     string
	From, To   time.Time
	QueryID    int64
	HasQueryID bool
	Host       string
	// Limit caps the groups the answer carries; the snapshot keeps them all.
	Limit int
}

// Plans reads a window for auto_explain records alone. The store is asked to
// narrow the read — plans are rare among ordinary log lines, and an unnarrowed
// scan spends its budget on everything else — but what it returns is filtered
// again here: a push-down may only narrow.
func (s *service) Plans(ctx context.Context, q PlansQuery) (ScanResult, error) {
	if !s.insights.IsEnabled() {
		return ScanResult{}, ErrDisabled
	}

	b, err := s.resolve(ctx, q.Cluster, q.Stream)
	if err != nil {
		return ScanResult{}, err
	}

	if err := validatePlansWindow(b, q); err != nil {
		return ScanResult{}, err
	}

	s.logRead(ctx, "log plans", q.Cluster, b.sourceName, q.Stream)

	res, groups, err := s.scanPlans(ctx, b, q, s.planLogging(ctx, b.cluster, q.Host))
	if err != nil {
		return ScanResult{}, err
	}

	res.ScanID = s.saveScan(Scan{ //nolint:exhaustruct
		Kind:    ScanPlans,
		Cluster: q.Cluster,
		Stream:  q.Stream,
		Host:    q.Host,
		From:    q.From,
		To:      q.To,
		Result:  res,
	}, groups)

	return res, nil
}

func validatePlansWindow(b binding, q PlansQuery) error {
	if err := validateWindow(b.cluster, q.From, q.To, q.Host); err != nil {
		return err
	}

	return validateQueryIDRole(b, q)
}

// validateQueryIDRole refuses a request for one statement on a stream that
// carries no query_id: a scan of the whole window would only come back empty.
// GET /api/logs/check lists the role as missing.
func validateQueryIDRole(b binding, q PlansQuery) error {
	if q.HasQueryID && b.fields.QueryID == "" {
		return fmt.Errorf("%w: stream %q carries no query_id", ErrInvalid, q.Stream)
	}

	return nil
}

// scanPlans reads one window for auto_explain records. It answers with the
// summary carrying the top groups and, beside it, the whole ranking for the
// snapshot. logging comes from the caller: the levels narrow the read, and a
// comparison reads two windows off one diagnosis.
func (s *service) scanPlans(
	ctx context.Context,
	b binding,
	q PlansQuery,
	logging planLogging,
) (ScanResult, []insights.PlanGroup, error) {
	params := source.StreamParams{ //nolint:exhaustruct
		Cluster: b.cluster,
		Stream:  q.Stream,
		From:    q.From,
		To:      q.To,
		Filter:  planFilter(q, logging.Levels, b.fields),
	}

	params.Filter = narrow(ctx, b.provider, params)

	scanCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	fm := b.fields
	detect := func(rec source.Record) (insights.PlanRecord, bool) {
		pr, ok := insights.Detect(rec.Fields[fm.Text], rec.Fields[fm.QueryID])
		if !ok || !q.HasQueryID {
			return pr, ok
		}

		return pr, pr.HasQueryID && pr.QueryID == q.QueryID
	}

	// Every plan record is charged at the plan cap, not only the ones of the
	// requested statement: what makes a record large is the plan in it.
	limits := s.scanBudget(func(rec source.Record) bool {
		_, ok := insights.Detect(rec.Fields[fm.Text], "")

		return ok
	})

	var covered, plansCovered Span

	plans := s.newPlans()

	st, scanErr := s.scan(scanCtx, b.provider, params, limits, func(rec source.Record) bool {
		if pr, ok := detect(rec); ok {
			plans.Add(pr, rec.Timestamp)
		}

		covered.add(rec.Timestamp)

		if !plans.Exhausted() {
			plansCovered.add(rec.Timestamp)
		}

		return true
	})

	summary := plans.Summary()

	reasons, err := partialReasons(st, scanErr, limits, summary.BudgetExhausted)
	if err != nil {
		return ScanResult{}, nil, err
	}

	res := ScanResult{ //nolint:exhaustruct
		Scanned:        st.Records,
		Partial:        len(reasons) > 0,
		PartialReasons: reasons,
		Covered:        covered,
		PlansCovered:   plansCovered,
		NarrowedBy:     narrowedBy(params.Filter),
		Plans:          summary,
		EmptyReason:    emptyReason(st, scanErr != nil, summary),
		Configuration:  logging.Config,
	}

	ranked := summary.Groups
	res.Plans.Groups = insights.TopGroups(ranked, planGroupLimit(q.Limit))

	return res, ranked, nil
}

// narrow asks the provider which parts of the filter it runs itself, on a
// budget of its own.
func narrow(ctx context.Context, p source.Provider, params source.StreamParams) source.Filter {
	ctx, cancel := context.WithTimeout(ctx, narrowTimeout)
	defer cancel()

	return p.Narrow(ctx, params)
}

// planFilter is what the store is asked to execute, in falling order of
// reliability: the severity auto_explain logs with, then the statement id, then
// the phrase of a plan record. The provider drops whatever it cannot run. An
// unread level narrows nothing: a guess would answer empty on a cluster that
// logs plans at another level.
func planFilter(q PlansQuery, levels []string, fm source.FieldMap) source.Filter {
	f := source.Filter{ //nolint:exhaustruct
		Host:       q.Host,
		Contains:   []string{planPhrase},
		Severities: planSeverities(levels, fm),
	}

	if q.HasQueryID {
		id := q.QueryID
		f.QueryID = &id
	}

	return f
}

func narrowedBy(f source.Filter) []string {
	var out []string

	if len(f.Severities) > 0 {
		out = append(out, NarrowedSeverity+"="+strings.Join(f.Severities, ","))
	}

	if f.QueryID != nil {
		out = append(out, NarrowedQueryID)
	}

	if len(f.Contains) > 0 {
		out = append(out, NarrowedText)
	}

	return out
}

func planGroupLimit(limit int) int {
	if limit <= 0 {
		return defaultPlanGroups
	}

	return min(limit, maxPlanGroups)
}
