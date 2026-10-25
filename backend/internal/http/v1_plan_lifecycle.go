package http

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/pathprobe"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/planlife"
)

// errPlanLifecycleFailed carries no part of the input: a 500 is logged.
var errPlanLifecycleFailed = errors.New("plan lifecycle: analysis_failed")

// AnalyzePlanLifecycle keeps nothing: the uploaded text lives only in this request.
func (s *Handlers) AnalyzePlanLifecycle(
	_ context.Context,
	req serverhttp.AnalyzePlanLifecycleRequestObject,
) (serverhttp.AnalyzePlanLifecycleResponseObject, error) {
	if req.Body == nil || strings.TrimSpace(req.Body.Trace) == "" {
		return serverhttp.AnalyzePlanLifecycle400JSONResponse{Message: "trace is required"}, nil
	}

	tr, err := pathprobe.Parse([]byte(req.Body.Trace))
	if err != nil {
		return traceParseError(err), nil
	}

	var plan *explain.Plan

	if req.Body.Plan != nil && strings.TrimSpace(*req.Body.Plan) != "" {
		p, err := explain.Parse(*req.Body.Plan, explain.SourceUpload)
		if err != nil {
			return planParseError(err), nil
		}

		plan = &p
	}

	out, err := analyzePlanLifecycle(tr, plan)
	if err != nil {
		return nil, err
	}

	return serverhttp.AnalyzePlanLifecycle200JSONResponse(out), nil
}

func analyzePlanLifecycle(tr pathprobe.Trace, plan *explain.Plan) (out serverhttp.PlanLifecycleAnalysis, err error) {
	defer func() {
		if recover() != nil {
			err = errPlanLifecycleFailed
		}
	}()

	if plan == nil {
		return mapPlanLifecycle(planlife.Analyze(tr)), nil
	}

	a := planlife.AnalyzeWithPlan(tr, plan)
	out = mapPlanLifecycle(a)
	out.Match = shortcut.Ptr(mapPlanLifecycleMatch(*a.Match))

	// no cluster here: rules that need work_mem or table sizes stay dormant
	findings, dormant := explain.Evaluate(plan, explain.Context{}) //nolint:exhaustruct
	out.Plan = shortcut.Ptr(mapPlanSummary(plan, findings, dormant))

	return out, nil
}

func traceParseError(err error) serverhttp.AnalyzePlanLifecycle422JSONResponse {
	out := serverhttp.AnalyzePlanLifecycle422JSONResponse{ //nolint:exhaustruct
		Input: serverhttp.PlanLifecycleParseErrorInputTrace,
		Code:  pathprobe.CodeInvalid,
	}

	var pe *pathprobe.Error
	if errors.As(err, &pe) {
		out.Code = pe.Code

		if pe.Field != "" {
			out.Field = shortcut.Ptr(pe.Field)
		}

		if pe.Offset > 0 {
			out.Offset = shortcut.Ptr(pe.Offset)
		}
	}

	return out
}

// planParseError drops explain.Error.Detail: it may quote the input.
func planParseError(err error) serverhttp.AnalyzePlanLifecycle422JSONResponse {
	out := serverhttp.AnalyzePlanLifecycle422JSONResponse{ //nolint:exhaustruct
		Input: serverhttp.PlanLifecycleParseErrorInputPlan,
		Code:  explain.CodeParseError,
	}

	var ee *explain.Error
	if errors.As(err, &ee) {
		out.Code = ee.Code
	}

	return out
}

func mapPlanLifecycle(a planlife.Analysis) serverhttp.PlanLifecycleAnalysis {
	rels := make([]serverhttp.PlanLifecycleRel, 0, len(a.Rels))
	for idx, alias := range a.Rels {
		rels = append(rels, serverhttp.PlanLifecycleRel{Index: idx, Alias: alias})
	}

	sort.Slice(rels, func(i, j int) bool { return rels[i].Index < rels[j].Index })

	return serverhttp.PlanLifecycleAnalysis{ //nolint:exhaustruct
		Query: a.Query,
		Rels:  rels,
		Coverage: serverhttp.PlanLifecycleCoverage{
			Known:      a.Coverage.Known,
			Survived:   a.Coverage.Survived,
			Accepted:   a.Coverage.Accepted,
			Rejected:   a.Coverage.Rejected,
			Displaced:  a.Coverage.Displaced,
			Removed:    a.Coverage.Removed,
			Precheck:   a.Coverage.Precheck,
			Excluded:   a.Coverage.Excluded,
			IndexBuild: a.Coverage.IndexBuild,
		},
		Limits: serverhttp.PlanLifecycleLimits{
			MaxEvents:        a.Limits.MaxEvents,
			EventsTruncated:  a.Limits.EventsTruncated,
			SummaryTruncated: a.Limits.SummaryTruncated,
		},
		Final:     mapPlanLifecycleFinal(a.Final),
		Paths:     mapstruct.SliceMap(a.Paths, mapPlanLifecyclePath),
		Relations: mapstruct.SliceMap(a.Relations, mapPlanLifecycleRelation),
		Indexes:   mapstruct.SliceMap(a.Indexes, mapPlanLifecycleIndex),
		Timeline:  mapstruct.SliceMap(a.Timeline, mapPlanLifecycleEvent),
		Caveats:   mapstruct.SliceMap(a.Caveats, mapPlanLifecycleCaveat),
	}
}

func mapPlanLifecycleFinal(f planlife.FinalPath) serverhttp.PlanLifecycleFinal {
	out := serverhttp.PlanLifecycleFinal{ //nolint:exhaustruct
		Status:     serverhttp.PlanLifecycleFinalStatus(f.Status),
		Candidates: intsOrEmpty(f.Candidates),
		Tree:       intsOrEmpty(f.Tree),
	}

	if f.PathID > 0 {
		out.PathId = shortcut.Ptr(f.PathID)
	}

	return out
}

func mapPlanLifecyclePath(p planlife.PathView) serverhttp.PlanLifecyclePath {
	out := serverhttp.PlanLifecyclePath{ //nolint:exhaustruct
		PathId:      p.ID,
		Pathtype:    p.PathType,
		Rel:         string(p.Rel),
		Fate:        serverhttp.PlanLifecyclePathFate(p.Fate),
		SubpathId:   optID(p.SubID),
		OuterPathId: optID(p.OuterID),
		InnerPathId: optID(p.InnerID),
		Rows:        p.Rows,
		IndexName:   p.IndexName,
		By:          optID(p.By),
		BySeq:       optID(p.BySeq),
		CostStartup: p.CostStartup,
		CostTotal:   p.CostTotal,
		Label:       p.Label,
		KeptReason:  p.KeptReason,
		Events:      intsOrEmpty(p.Events),
		Parents:     intsOrEmpty(p.Parents),
		RowsRatio:   p.RowsRatio,
	}

	if p.PlanNode != nil {
		out.PlanNode = shortcut.Ptr(p.PlanNode)
	}

	if len(p.RequiredOuter) > 0 {
		out.RequiredOuter = shortcut.Ptr(p.RequiredOuter)
	}

	if p.IndexOID != 0 {
		out.IndexOid = shortcut.Ptr(int64(p.IndexOID))
	}

	if p.IndexHypo {
		out.HypotheticalIndex = shortcut.Ptr(true)
	}

	if len(p.ChildIDs) > 0 {
		out.ChildPathIds = shortcut.Ptr(p.ChildIDs)
	}

	if p.InSubquery {
		out.InSubquery = shortcut.Ptr(true)
	}

	if p.FateReason != "" {
		out.FateReason = shortcut.Ptr(serverhttp.PlanLifecyclePathFateReason(p.FateReason))
	}

	if p.Cause != nil {
		out.Cause = &serverhttp.PlanLifecycleCause{
			Code:          serverhttp.PlanLifecycleCauseCode(p.Cause.Code),
			Gap:           p.Cause.Gap,
			Startup:       p.Cause.Startup,
			WinnerStartup: p.Cause.WinnerStartup,
		}
	}

	return out
}

func mapPlanLifecycleMatch(m planlife.Match) serverhttp.PlanLifecycleMatch {
	out := serverhttp.PlanLifecycleMatch{ //nolint:exhaustruct
		Status:    serverhttp.PlanLifecycleMatchStatus(m.Status),
		QueryText: serverhttp.PlanLifecycleMatchQueryText(m.QueryText),
		Notes: mapstruct.SliceMap(m.Notes, func(n planlife.MatchNote) serverhttp.PlanLifecycleMatchNote {
			return serverhttp.PlanLifecycleMatchNote{
				Code:     serverhttp.PlanLifecycleMatchNoteCode(n.Code),
				PathId:   n.PathID,
				PlanNode: intsOrEmpty(n.PlanNode),
			}
		}),
		Unmatched: mapstruct.SliceMap(m.Unmatched, func(u planlife.Unmatched) serverhttp.PlanLifecycleUnmatched {
			return serverhttp.PlanLifecycleUnmatched{
				PlanNode: intsOrEmpty(u.PlanNode),
				Reason:   serverhttp.PlanLifecycleUnmatchedReason(u.Reason),
			}
		}),
	}

	if d := m.Mismatch; d != nil {
		out.Mismatch = &serverhttp.PlanLifecycleMatchDiff{
			PlanNode: intsOrEmpty(d.PlanNode),
			PathId:   d.PathID,
			Expected: d.Expected,
			Found:    d.Found,
		}
	}

	return out
}

func mapPlanLifecycleRelation(r planlife.Relation) serverhttp.PlanLifecycleRelation {
	counts := make(map[string]int, len(r.Counts))
	for f, n := range r.Counts {
		counts[string(f)] = n
	}

	return serverhttp.PlanLifecycleRelation{
		Key:     string(r.Key),
		Label:   r.Label,
		PathIds: intsOrEmpty(r.PathIDs),
		Counts:  counts,
		Precheck: mapstruct.SliceMap(r.Precheck, func(g planlife.PrecheckGroup) serverhttp.PlanLifecyclePrecheckGroup {
			return serverhttp.PlanLifecyclePrecheckGroup{
				Method: g.Method, Reason: g.Reason, Count: g.Count, MinCost: g.MinCost, MaxCost: g.MaxCost,
			}
		}),
	}
}

func mapPlanLifecycleIndex(c planlife.IndexChain) serverhttp.PlanLifecycleIndexChain {
	out := serverhttp.PlanLifecycleIndexChain{ //nolint:exhaustruct
		Index:        c.Index,
		Hypothetical: c.Hypothetical,
		Outcome:      serverhttp.PlanLifecycleIndexChainOutcome(c.Outcome),
		Leaves:       mapstruct.SliceMap(c.Leaves, mapPlanLifecycleChainNode),
		Truncated:    c.Truncated,
	}

	if c.OID != 0 {
		out.Oid = shortcut.Ptr(int64(c.OID))
	}

	if c.Best != 0 {
		out.BestPathId = shortcut.Ptr(c.Best)
		out.BestAtFinalLevel = shortcut.Ptr(c.BestAtFinalLevel)
	}

	return out
}

func mapPlanLifecycleChainNode(n planlife.ChainNode) serverhttp.PlanLifecycleChainNode {
	return serverhttp.PlanLifecycleChainNode{
		PathId: n.PathID,
		Up:     mapstruct.SliceMap(n.Up, mapPlanLifecycleChainNode),
	}
}

func mapPlanLifecycleEvent(e pathprobe.Event) serverhttp.PlanLifecycleEvent {
	out := serverhttp.PlanLifecycleEvent{ //nolint:exhaustruct
		Seq:         e.Seq,
		Src:         e.Src,
		Rel:         string(e.Rel),
		Kind:        string(e.Kind),
		Pathtype:    e.PathType,
		PathId:      optID(e.PathID),
		CostStartup: e.CostStartup,
		CostTotal:   e.CostTotal,
		Gap:         e.Gap,
		Param:       e.Param,
		Label:       e.Label,
		KeptReason:  e.KeptReason,
		Origin:      e.Origin,
		SkipReason:  e.SkipReason,
		ByPathId:    optID(e.ByID),
		BySeq:       optID(e.BySeq),
		ByPathtype:  e.ByType,
	}

	if e.DisabledNodes > 0 {
		out.DisabledNodes = shortcut.Ptr(e.DisabledNodes)
	}

	return out
}

func mapPlanLifecycleCaveat(c planlife.Caveat) serverhttp.PlanLifecycleCaveat {
	out := serverhttp.PlanLifecycleCaveat{ //nolint:exhaustruct
		Code:   c.Code,
		Scope:  serverhttp.PlanLifecycleCaveatScope(c.Scope),
		PathId: optID(c.PathID),
	}

	if c.Rel != "" {
		out.Rel = shortcut.Ptr(string(c.Rel))
	}

	if len(c.Params) > 0 {
		out.Params = shortcut.Ptr(c.Params)
	}

	return out
}

// optID: 0 means "none" in planlife and is left out of the JSON.
func optID(id int) *int {
	if id <= 0 {
		return nil
	}

	return &id
}

func intsOrEmpty(s []int) []int {
	if s == nil {
		return []int{}
	}

	return s
}
