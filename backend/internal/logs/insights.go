package logs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// Why a summary covers less than the whole window.
const (
	PartialRecords = "records"
	PartialBytes   = "bytes"
	PartialTimeout = "timeout"
	PartialSource  = "source"
	PartialPlans   = "plans"
)

// Why a summary holds no plan groups.
const (
	EmptyNoRecords         = "no_records"
	EmptyNoPlanRecords     = "no_plan_records"
	EmptyUnsupportedFormat = "unsupported_plan_format"
	EmptyNotParsed         = "not_parsed"
	EmptyBudgetExhausted   = "budget_exhausted"
	EmptySourceUnavailable = "source_unavailable"
)

const (
	insightsTopGroups    = 10
	insightsTopTemplates = 5
)

// Span is a range of record timestamps; the zero Span holds no record.
type Span struct {
	From, To time.Time
}

func (s *Span) add(ts time.Time) {
	if s.From.IsZero() || ts.Before(s.From) {
		s.From = ts
	}

	if ts.After(s.To) {
		s.To = ts
	}
}

type InsightsQuery struct {
	Cluster  string
	Stream   string
	From, To time.Time
	Host     string
}

// ScanResult is one read of a window, by either scanning endpoint. Covered
// spans the records read; PlansCovered spans those read while the plan budget
// lasted. Categories are counted by an insights scan only. NarrowedBy names the
// filters the log store executed itself. EmptyReason is set when Plans holds no
// group. ScanID is uuid.Nil when no snapshot was stored.
type ScanResult struct {
	ScanID         uuid.UUID
	Scanned        int
	Partial        bool
	PartialReasons []string
	Covered        Span
	PlansCovered   Span
	NarrowedBy     []string
	Categories     []insights.CategorySummary
	Plans          insights.PlansSummary
	// PlanRecords counts the auto_explain records the window held, including the
	// ones a filter on the statement dropped before Plans saw them.
	PlanRecords   int
	EmptyReason   string
	Configuration *Configuration
}

// Insights reads the window once: every record is classified, and a record
// carrying a plan is also parsed. Only the host is pushed down: the categories
// need every record.
func (s *service) Insights(ctx context.Context, q InsightsQuery) (ScanResult, error) {
	if !s.insights.IsEnabled() {
		return ScanResult{}, ErrDisabled
	}

	b, err := s.resolve(ctx, q.Cluster, q.Stream)
	if err != nil {
		return ScanResult{}, err
	}

	if err := validateWindow(b.cluster, q.From, q.To, q.Host); err != nil {
		return ScanResult{}, err
	}

	s.logRead(ctx, "log insights", q.Cluster, b.sourceName, q.Stream)

	params := source.StreamParams{ //nolint:exhaustruct
		Cluster: b.cluster,
		Stream:  q.Stream,
		From:    q.From,
		To:      q.To,
		Filter:  source.Filter{Host: q.Host}, //nolint:exhaustruct
	}

	fm := b.fields
	classify := func(rec source.Record) string {
		return insights.Classify(insights.Record{
			Stream:   q.Stream,
			Severity: rec.Fields[fm.Severity],
			SQLState: rec.Fields[fm.SQLState],
			Text:     rec.Fields[fm.Text],
		})
	}

	limits := s.scanBudget(func(rec source.Record) bool {
		return classify(rec) == insights.CategoryPlan
	})

	configuration := s.configurationAsync(ctx, b.cluster, q.Host)

	scanCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	var covered, plansCovered Span

	categories := insights.NewCategoryCounts()
	plans := s.newPlans()

	st, scanErr := s.scan(scanCtx, b.provider, params, limits, func(rec source.Record) bool {
		text := rec.Fields[fm.Text]
		code := classify(rec)

		categories.Add(code, text, rec.Timestamp)

		if code == insights.CategoryPlan {
			if pr, ok := insights.Detect(text, rec.Fields[fm.QueryID]); ok {
				plans.Add(pr, rec.Timestamp)
			}
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
		return ScanResult{}, err
	}

	res := ScanResult{ //nolint:exhaustruct
		Scanned:        st.Records,
		Partial:        len(reasons) > 0,
		PartialReasons: reasons,
		Covered:        covered,
		PlansCovered:   plansCovered,
		Categories:     categories.Summary(insightsTopTemplates),
		Plans:          summary,
		PlanRecords:    summary.Records,
		EmptyReason:    emptyReason(st, scanErr != nil, summary),
		Configuration:  configuration(),
	}

	ranked := summary.Groups
	res.Plans.Groups = insights.TopGroups(ranked, insightsTopGroups)

	res.ScanID = s.saveScan(Scan{ //nolint:exhaustruct
		Kind:    ScanInsights,
		Cluster: q.Cluster,
		Stream:  q.Stream,
		Host:    q.Host,
		From:    q.From,
		To:      q.To,
		Result:  res,
	}, ranked)

	return res, nil
}

// scanBudget bounds one read by the configured limits. capRecord marks the
// records charged at most MaxPlanBytes: a plan an order of magnitude larger
// than a log line would otherwise eat the whole byte budget.
func (s *service) scanBudget(capRecord func(source.Record) bool) scanLimits {
	return scanLimits{
		MaxRecords:     s.insights.MaxRecords,
		MaxBytes:       s.insights.MaxBytes,
		MaxRecordBytes: int64(s.insights.MaxPlanBytes),
		CapRecord:      capRecord,
	}
}

func (s *service) newPlans() *insights.Plans {
	return insights.NewPlans(insights.PlanLimits{
		MaxPlanBytes: s.insights.MaxPlanBytes,
		MaxPlans:     s.insights.MaxPlans,
	})
}

// partialReasons says why a read covered less than the whole window. A failure
// that left nothing to report comes back as an error instead.
func partialReasons(st scanStats, err error, limits scanLimits, planBudgetExhausted bool) ([]string, error) {
	var reasons []string

	if err != nil {
		switch {
		case st.Partial:
			reasons = append(reasons, PartialSource)
		case errors.Is(err, ErrTimeout) && st.Records > 0:
			reasons = append(reasons, PartialTimeout)
		default:
			return nil, err
		}
	}

	if st.Capped {
		if limits.MaxRecords > 0 && st.Records >= limits.MaxRecords {
			reasons = append(reasons, PartialRecords)
		}

		if limits.MaxBytes > 0 && st.Bytes >= limits.MaxBytes {
			reasons = append(reasons, PartialBytes)
		}
	}

	if planBudgetExhausted {
		reasons = append(reasons, PartialPlans)
	}

	return reasons, nil
}

func emptyReason(st scanStats, interrupted bool, p insights.PlansSummary) string {
	switch {
	case p.TotalGroups > 0:
		return ""
	case st.Records == 0 && interrupted:
		return EmptySourceUnavailable
	case st.Records == 0:
		return EmptyNoRecords
	case p.Records == 0 && st.Capped:
		return EmptyBudgetExhausted
	case p.Records == 0 && interrupted:
		return EmptySourceUnavailable
	case p.Records == 0:
		return EmptyNoPlanRecords
	case onlyUnsupported(p.NotParsed):
		return EmptyUnsupportedFormat
	case p.BudgetExhausted:
		return EmptyBudgetExhausted
	default:
		return EmptyNotParsed
	}
}

func onlyUnsupported(notParsed []insights.NotParsed) bool {
	for _, np := range notParsed {
		if np.Code != explain.CodeUnsupportedFormat {
			return false
		}
	}

	return len(notParsed) > 0
}
