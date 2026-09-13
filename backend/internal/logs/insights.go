package logs

import (
	"context"
	"errors"
	"time"

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

// InsightsResult is one read of a window. Covered spans the records read;
// PlansCovered spans those read while the plan budget lasted. EmptyReason is
// set when Plans holds no group.
type InsightsResult struct {
	Scanned        int
	Partial        bool
	PartialReasons []string
	Covered        Span
	PlansCovered   Span
	Categories     []insights.CategorySummary
	Plans          insights.PlansSummary
	EmptyReason    string
}

// Insights reads the window once: every record is classified, and a record
// carrying a plan is also parsed. Only the host is pushed down: the categories
// need every record.
func (s *service) Insights(ctx context.Context, q InsightsQuery) (InsightsResult, error) {
	if !s.insights.IsEnabled() {
		return InsightsResult{}, ErrDisabled
	}

	b, err := s.resolve(ctx, q.Cluster, q.Stream)
	if err != nil {
		return InsightsResult{}, err
	}

	if err := validateWindow(b.cluster, q.From, q.To, q.Host); err != nil {
		return InsightsResult{}, err
	}

	s.logRead(ctx, "log insights", q.Cluster, b.sourceName, q.Stream)

	params := source.StreamParams{
		Cluster: b.cluster,
		Stream:  q.Stream,
		From:    q.From,
		To:      q.To,
		Filter:  source.Filter{Severities: nil, Host: q.Host},
		Token:   "",
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

	limits := scanLimits{
		MaxRecords:     s.insights.MaxRecords,
		MaxBytes:       s.insights.MaxBytes,
		MaxRecordBytes: int64(s.insights.MaxPlanBytes),
		CapRecord: func(rec source.Record) bool {
			return classify(rec) == insights.CategoryPlan
		},
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	var covered, plansCovered Span

	categories := insights.NewCategoryCounts()
	plans := insights.NewPlans(insights.PlanLimits{
		MaxPlanBytes: s.insights.MaxPlanBytes,
		MaxPlans:     s.insights.MaxPlans,
	})

	st, err := s.scan(ctx, b.provider, params, limits, func(rec source.Record) bool {
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

	var reasons []string

	if err != nil {
		switch {
		case st.Partial:
			reasons = append(reasons, PartialSource)
		case errors.Is(err, ErrTimeout) && st.Records > 0:
			reasons = append(reasons, PartialTimeout)
		default:
			return InsightsResult{}, err
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

	summary := plans.Summary(insightsTopGroups)
	if summary.BudgetExhausted {
		reasons = append(reasons, PartialPlans)
	}

	return InsightsResult{
		Scanned:        st.Records,
		Partial:        len(reasons) > 0,
		PartialReasons: reasons,
		Covered:        covered,
		PlansCovered:   plansCovered,
		Categories:     categories.Summary(insightsTopTemplates),
		Plans:          summary,
		EmptyReason:    emptyReason(st, err != nil, summary),
	}, nil
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
