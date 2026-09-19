package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
)

// GetLogsInsights summarizes one window of cluster logs: event categories and
// the auto_explain plans found in it.
func (s *Handlers) GetLogsInsights(
	ctx context.Context,
	req serverhttp.GetLogsInsightsRequestObject,
) (serverhttp.GetLogsInsightsResponseObject, error) {
	p := req.Params

	res, err := s.logs.Insights(ctx, logs.InsightsQuery{
		Cluster: string(p.ClusterName),
		Stream:  string(p.ServiceType),
		From:    p.From,
		To:      p.To,
		Host:    deref(p.Host),
	})
	if err != nil {
		switch {
		case errors.Is(err, logs.ErrNotFound), errors.Is(err, logs.ErrDisabled):
			return serverhttp.GetLogsInsights404Response{}, nil
		case errors.Is(err, logs.ErrInvalid):
			return serverhttp.GetLogsInsights400Response{}, nil
		case errors.Is(err, logs.ErrUnsupported):
			return serverhttp.GetLogsInsights501Response{}, nil
		case errors.Is(err, logs.ErrTimeout):
			return serverhttp.GetLogsInsights504Response{}, nil
		case errors.Is(err, logs.ErrUpstream):
			return serverhttp.GetLogsInsights502JSONResponse{Message: upstreamMessage}, nil
		default:
			return nil, fmt.Errorf("GetLogsInsights | %w", err)
		}
	}

	return serverhttp.GetLogsInsights200JSONResponse(mapLogInsights(res)), nil
}

func mapLogInsights(res logs.ScanResult) serverhttp.LogInsights {
	reasons := make([]serverhttp.LogInsightsPartialReason, 0, len(res.PartialReasons))
	for _, r := range res.PartialReasons {
		reasons = append(reasons, serverhttp.LogInsightsPartialReason(r))
	}

	out := serverhttp.LogInsights{ //nolint:exhaustruct
		Scanned:        res.Scanned,
		Partial:        res.Partial,
		PartialReasons: reasons,
		Plans:          mapLogPlansSummary(res.Plans, res.EmptyReason),
		Configuration:  mapLogConfiguration(res.Configuration),
	}

	// A plans scan counts no category, which is not the same as counting none.
	if res.Categories != nil {
		out.Categories = shortcut.Ptr(mapstruct.SliceMap(res.Categories, mapLogCategory))
	}

	if len(res.NarrowedBy) > 0 {
		out.NarrowedBy = &res.NarrowedBy
	}

	out.CoveredFrom, out.CoveredTo = spanBounds(res.Covered)
	out.Plans.CoveredFrom, out.Plans.CoveredTo = spanBounds(res.PlansCovered)

	if res.ScanID != uuid.Nil {
		out.ScanId = shortcut.Ptr(openapi_types.UUID(res.ScanID))
	}

	return out
}

func mapLogConfiguration(c *logs.Configuration) *serverhttp.LogPlanConfiguration {
	if c == nil {
		return nil
	}

	out := serverhttp.LogPlanConfiguration{ //nolint:exhaustruct
		Instance:    c.Instance,
		AutoExplain: c.AutoExplain,
		LogAnalyze:  c.LogAnalyze,
		LogLevel:    optString(c.LogLevel),
	}

	if c.LogMinDurationMs != nil {
		out.LogMinDurationMs = shortcut.Ptr(int(*c.LogMinDurationMs))
	}

	if c.LogFormat != "" {
		out.LogFormat = shortcut.Ptr(serverhttp.LogPlanConfigurationLogFormat(c.LogFormat))
	}

	if c.ComputeQueryID != "" {
		out.ComputeQueryId = shortcut.Ptr(serverhttp.LogPlanConfigurationComputeQueryId(c.ComputeQueryID))
	}

	return &out
}

func spanBounds(s logs.Span) (*time.Time, *time.Time) {
	if s.From.IsZero() {
		return nil, nil
	}

	return shortcut.Ptr(s.From), shortcut.Ptr(s.To)
}

func mapLogCategory(c insights.CategorySummary) serverhttp.LogCategory {
	return serverhttp.LogCategory{
		Code:      serverhttp.LogCategoryCode(c.Code),
		Count:     c.Count,
		Share:     c.Share,
		FirstSeen: c.First,
		LastSeen:  c.Last,
		Templates: mapstruct.SliceMap(c.Templates, func(t insights.TemplateCount) serverhttp.LogCategoryTemplate {
			return serverhttp.LogCategoryTemplate{
				Template:  t.Template,
				Count:     t.Count,
				FirstSeen: t.First,
				LastSeen:  t.Last,
			}
		}),
	}
}

func mapLogPlansSummary(p insights.PlansSummary, emptyReason string) serverhttp.LogPlansSummary {
	out := serverhttp.LogPlansSummary{ //nolint:exhaustruct
		Records: p.Records,
		Parsed:  p.Parsed,
		NotParsed: mapstruct.SliceMap(p.NotParsed, func(np insights.NotParsed) serverhttp.PlanNotParsed {
			return serverhttp.PlanNotParsed{Code: serverhttp.PlanNotParsedCode(np.Code), Count: np.Count}
		}),
		WithoutQueryId:  p.WithoutQueryID,
		TotalGroups:     p.TotalGroups,
		TotalDurationMs: p.TotalDurationMs,
		MaxDurationMs:   p.MaxDurationMs,
		Groups:          mapstruct.SliceMap(p.Groups, mapLogPlanGroup),
		Dormant: mapstruct.SliceMap(p.Dormant, func(d insights.DormantRule) serverhttp.PlanDormantRule {
			return serverhttp.PlanDormantRule{
				Code:    d.Code,
				Missing: planMissing(d.Missing),
				Groups:  shortcut.Ptr(d.Groups),
			}
		}),
	}

	if p.TotalGroups > 0 {
		out.FirstSeen = shortcut.Ptr(p.First)
		out.LastSeen = shortcut.Ptr(p.Last)
	}

	if emptyReason != "" {
		out.EmptyReason = shortcut.Ptr(serverhttp.LogPlansSummaryEmptyReason(emptyReason))
	}

	return out
}

func mapLogPlanGroup(g insights.PlanGroup) serverhttp.LogPlanGroup {
	out := serverhttp.LogPlanGroup{ //nolint:exhaustruct
		Ord:       g.Ord,
		Hash:      g.Hash,
		Count:     g.Count,
		Durations: mapPlanDurations(g.Durations),
		FirstSeen: g.First,
		LastSeen:  g.Last,
		Plan:      mapPlanSummary(&g.Sample, g.Findings, g.Dormant),
	}

	if g.HasQueryID {
		out.QueryId = shortcut.Ptr(strconv.FormatInt(g.QueryID, 10))
	}

	return out
}

func mapPlanDurations(d insights.DurationStats) serverhttp.PlanDurationStats {
	return serverhttp.PlanDurationStats{
		MinMs: d.Min,
		P50Ms: d.P50,
		P95Ms: d.P95,
		MaxMs: d.Max,
		SumMs: d.Sum,
	}
}

func mapPlanSummary(p *explain.Plan, findings []explain.Finding, dormant []explain.Dormant) serverhttp.PlanSummary {
	out := serverhttp.PlanSummary{ //nolint:exhaustruct
		Format:          serverhttp.PlanSummaryFormat(p.Format),
		Generic:         p.Generic,
		DurationMs:      p.Duration,
		PlanningTimeMs:  p.PlanningTime,
		ExecutionTimeMs: p.ExecutionTime,
		Capabilities: serverhttp.PlanCapabilities{
			Actual:   p.Caps.Actual,
			Buffers:  p.Caps.Buffers,
			Timing:   p.Caps.Timing,
			Settings: p.Caps.Settings,
		},
		Root:     mapPlanNode(&p.Root, p.Caps.Timing),
		Settings: mapOrNil(p.Settings),
		Findings: mapstruct.SliceMap(findings, mapPlanFinding),
		Dormant: mapstruct.SliceMap(dormant, func(d explain.Dormant) serverhttp.PlanDormantRule {
			return serverhttp.PlanDormantRule{Code: d.Code, Missing: planMissing(d.Missing), Groups: nil}
		}),
	}

	out.QueryText, out.QueryTextOmittedBytes = clip(p.QueryText, planQueryTextLimit)

	out.QueryParams, out.QueryParamsOmittedBytes = clipOpt(p.QueryParams, planQueryTextLimit)

	if len(p.Triggers) > 0 {
		out.Triggers = shortcut.Ptr(mapstruct.SliceMap(p.Triggers, func(t explain.Trigger) serverhttp.PlanTrigger {
			return serverhttp.PlanTrigger{Name: t.Name, Relation: optString(t.Relation), TimeMs: t.Time, Calls: t.Calls}
		}))
	}

	if p.JIT != nil {
		out.Jit = &serverhttp.PlanJIT{
			Functions:      p.JIT.Functions,
			GenerationMs:   p.JIT.Generation,
			InliningMs:     p.JIT.Inlining,
			OptimizationMs: p.JIT.Optimization,
			EmissionMs:     p.JIT.Emission,
			TotalMs:        p.JIT.Total,
		}
	}

	return out
}

// mapPlanNode leaves the measured times out when the plan was logged without
// timing: PostgreSQL prints rows and loops alone then.
func mapPlanNode(n *explain.Node, timing bool) serverhttp.PlanNode {
	out := serverhttp.PlanNode{ //nolint:exhaustruct
		Type:                    n.Type,
		Relation:                optString(n.Relation),
		Schema:                  optString(n.Schema),
		Alias:                   optString(n.Alias),
		IndexName:               optString(n.IndexName),
		ParentRelationship:      optString(n.ParentRel),
		SubplanName:             optString(n.SubplanName),
		Parallel:                n.Parallel,
		PartialMode:             optString(n.PartialMode),
		Strategy:                optString(n.Strategy),
		JoinType:                optString(n.JoinType),
		ScanDirection:           optString(n.ScanDirection),
		Operation:               optString(n.Operation),
		StartupCost:             n.StartupCost,
		TotalCost:               n.TotalCost,
		PlanRows:                n.PlanRows,
		PlanWidth:               n.PlanWidth,
		RowsRemovedByFilter:     n.RowsRemovedByFilter,
		RowsRemovedByJoinFilter: n.RowsRemovedByJoinFilter,
		HeapFetches:             n.HeapFetches,
		SortMethod:              optString(n.SortMethod),
		SortSpaceKb:             n.SortSpaceKB,
		SortSpaceType:           optString(n.SortSpaceType),
		WorkersPlanned:          n.WorkersPlanned,
		WorkersLaunched:         n.WorkersLaunched,
		HeapBlocksExact:         n.HeapBlocksExact,
		HeapBlocksLossy:         n.HeapBlocksLossy,
		Children:                make([]serverhttp.PlanNode, 0, len(n.Children)),
	}

	out.Filter, out.FilterOmittedBytes = clipOpt(n.Filter, planConditionLimit)
	out.IndexCond, out.IndexCondOmittedBytes = clipOpt(n.IndexCond, planConditionLimit)
	out.RecheckCond, out.RecheckCondOmittedBytes = clipOpt(n.RecheckCond, planConditionLimit)
	out.JoinCond, out.JoinCondOmittedBytes = clipOpt(n.JoinCond, planConditionLimit)

	if len(n.SortKey) > 0 {
		out.SortKey = shortcut.Ptr(n.SortKey)
	}

	if a := n.Actual; a != nil {
		out.Actual = &serverhttp.PlanNodeActual{Rows: a.Rows, Loops: a.Loops} //nolint:exhaustruct

		if timing {
			out.Actual.StartupTimeMs = shortcut.Ptr(a.StartupTime)
			out.Actual.TotalTimeMs = shortcut.Ptr(a.TotalTime)
		}
	}

	if b := n.Buffers; b != nil {
		out.Buffers = &serverhttp.PlanBuffers{
			SharedHit:     b.SharedHit,
			SharedRead:    b.SharedRead,
			SharedDirtied: b.SharedDirtied,
			SharedWritten: b.SharedWritten,
			LocalHit:      b.LocalHit,
			LocalRead:     b.LocalRead,
			LocalDirtied:  b.LocalDirtied,
			LocalWritten:  b.LocalWritten,
			TempRead:      b.TempRead,
			TempWritten:   b.TempWritten,
		}
	}

	for i := range n.Children {
		out.Children = append(out.Children, mapPlanNode(&n.Children[i], timing))
	}

	return out
}

func mapPlanFinding(f explain.Finding) serverhttp.PlanFinding {
	out := serverhttp.PlanFinding{ //nolint:exhaustruct
		Code:     serverhttp.PlanFindingCode(f.Code),
		Severity: serverhttp.PlanFindingSeverity(f.Severity),
		Path:     append([]int{}, f.Path...),
		NodeType: f.NodeType,
		Relation: optString(f.Relation),
	}

	if len(f.Params) > 0 {
		params := map[string]interface{}(f.Params)
		out.Params = &params
	}

	return out
}

func planMissing(missing []string) []serverhttp.PlanMissingRequirement {
	out := make([]serverhttp.PlanMissingRequirement, 0, len(missing))
	for _, m := range missing {
		out = append(out, serverhttp.PlanMissingRequirement(m))
	}

	return out
}

// Bytes of a plan text the response keeps; the rest is counted in the matching
// *_omitted_bytes field.
const (
	planQueryTextLimit = 8 << 10
	planConditionLimit = 4 << 10
)

// clip cuts s to at most limit bytes on a rune boundary and returns how many
// bytes it left out, nil when nothing was cut.
func clip(s string, limit int) (string, *int) {
	if len(s) <= limit {
		return s, nil
	}

	n := limit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n], shortcut.Ptr(len(s) - n)
}

func clipOpt(s string, limit int) (*string, *int) {
	if s == "" {
		return nil, nil
	}

	clipped, omitted := clip(s, limit)

	return &clipped, omitted
}

func optString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
