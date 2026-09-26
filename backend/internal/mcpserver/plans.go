package mcpserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/google/uuid"
)

const (
	planLogsDefaultSince = time.Hour

	planGroupsMax      = 10
	planGroupsFloor    = 3
	planTemplatesShown = 3
	planTemplateBytes  = 200

	planListDefault = 20
	planListMax     = 100
	planListFloor   = 5

	planTreesDefault = 3
	planTreesMax     = 5
	planScanGroups   = 10

	planQueryTextBytes = 2000
	planParamsBytes    = 500
	planCondBytes      = 300
	planCondFloor      = 100
	planChildCap       = 10
	planChildFloor     = 3

	regressionsDefault   = 10
	regressionsMax       = 30
	regressionsFloor     = 3
	defaultBaselineShift = 24 * time.Hour
)

const planStream = apiclient.GetLogsPlansParamsServiceTypePostgresql

type planWindow struct {
	From           time.Time                            `json:"from"`
	To             time.Time                            `json:"to"`
	Host           string                               `json:"host,omitempty"`
	CoveredFrom    *time.Time                           `json:"covered_from,omitempty"`
	CoveredTo      *time.Time                           `json:"covered_to,omitempty"`
	Scanned        int                                  `json:"scanned"`
	Partial        bool                                 `json:"partial"`
	PartialReasons []apiclient.LogInsightsPartialReason `json:"partial_reasons,omitempty"`
	NarrowedBy     []string                             `json:"narrowed_by,omitempty"`
	Caution        string                               `json:"caution,omitempty"`
}

type planTemplate struct {
	Template string `json:"template"`
	Count    int    `json:"count"`
}

type planCategory struct {
	Code      string         `json:"code"`
	Count     int            `json:"count"`
	SharePct  float64        `json:"share_pct"`
	FirstSeen time.Time      `json:"first_seen"`
	LastSeen  time.Time      `json:"last_seen"`
	Templates []planTemplate `json:"templates,omitempty"`
}

type planStats struct {
	Records         int                         `json:"records"`
	Parsed          int                         `json:"parsed"`
	WithoutQueryID  int                         `json:"without_query_id"`
	NotParsed       []apiclient.PlanNotParsed   `json:"not_parsed,omitempty"`
	TotalGroups     int                         `json:"total_groups"`
	TotalDurationMs float64                     `json:"total_duration_ms"`
	MaxDurationMs   float64                     `json:"max_duration_ms"`
	CoveredFrom     *time.Time                  `json:"covered_from,omitempty"`
	CoveredTo       *time.Time                  `json:"covered_to,omitempty"`
	EmptyReason     string                      `json:"empty_reason,omitempty"`
	Dormant         []apiclient.PlanDormantRule `json:"dormant,omitempty"`
}

type planFindingRef struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Node     string `json:"node"`
	Relation string `json:"relation,omitempty"`
}

type planGroupRow struct {
	Ord        int              `json:"ord"`
	QueryID    string           `json:"query_id,omitempty"`
	Hash       string           `json:"hash"`
	Count      int              `json:"count"`
	P50Ms      float64          `json:"p50_ms"`
	P95Ms      float64          `json:"p95_ms"`
	MaxMs      float64          `json:"max_ms"`
	SumMs      float64          `json:"sum_ms"`
	FirstSeen  time.Time        `json:"first_seen"`
	LastSeen   time.Time        `json:"last_seen"`
	QueryTrunc string           `json:"query_trunc"`
	QueryLen   int              `json:"query_len"`
	Findings   []planFindingRef `json:"findings,omitempty"`
}

func planStatsOf(p apiclient.LogPlansSummary) planStats {
	s := planStats{ //nolint:exhaustruct
		Records:         p.Records,
		Parsed:          p.Parsed,
		WithoutQueryID:  p.WithoutQueryId,
		NotParsed:       p.NotParsed,
		TotalGroups:     p.TotalGroups,
		TotalDurationMs: round1(p.TotalDurationMs),
		MaxDurationMs:   round1(p.MaxDurationMs),
		CoveredFrom:     p.CoveredFrom,
		CoveredTo:       p.CoveredTo,
		Dormant:         p.Dormant,
	}

	if p.EmptyReason != nil {
		s.EmptyReason = string(*p.EmptyReason)
	}

	return s
}

func planWindowOf(s *apiclient.LogInsights, from, to time.Time, host string) planWindow {
	w := planWindow{ //nolint:exhaustruct
		From:           from,
		To:             to,
		Host:           host,
		CoveredFrom:    s.CoveredFrom,
		CoveredTo:      s.CoveredTo,
		Scanned:        s.Scanned,
		Partial:        s.Partial,
		PartialReasons: s.PartialReasons,
		NarrowedBy:     deref(s.NarrowedBy),
	}

	if sc := s.Scan; sc != nil {
		w.From, w.To, w.Host = sc.From, sc.To, deref(sc.Host)
	}

	if w.Partial {
		w.Caution = "the window was read only in part (covered_from..covered_to): every count and total is a " +
			"lower bound, and an absent statement may simply lie in the unread part"
	}

	return w
}

func findingRefs(fs []apiclient.PlanFinding) []planFindingRef {
	out := make([]planFindingRef, 0, len(fs))
	for _, f := range fs {
		out = append(out, planFindingRef{
			Code:     string(f.Code),
			Severity: string(f.Severity),
			Node:     f.NodeType,
			Relation: deref(f.Relation),
		})
	}

	return out
}

func groupRow(ord int, qid *string, hash string, count int, d apiclient.PlanDurationStats,
	first, last time.Time, text string, omitted *int, fs []apiclient.PlanFinding,
) planGroupRow {
	return planGroupRow{
		Ord:        ord,
		QueryID:    deref(qid),
		Hash:       hash,
		Count:      count,
		P50Ms:      round1(d.P50Ms),
		P95Ms:      round1(d.P95Ms),
		MaxMs:      round1(d.MaxMs),
		SumMs:      round1(d.SumMs),
		FirstSeen:  first,
		LastSeen:   last,
		QueryTrunc: truncQuery(strings.Join(strings.Fields(text), " ")),
		QueryLen:   len(text) + deref(omitted),
		Findings:   findingRefs(fs),
	}
}

func rowOfGroup(g apiclient.LogPlanGroup) planGroupRow {
	return groupRow(g.Ord, g.QueryId, g.Hash, g.Count, g.Durations, g.FirstSeen, g.LastSeen,
		g.Plan.QueryText, g.Plan.QueryTextOmittedBytes, g.Plan.Findings)
}

func rowOfPageItem(r apiclient.LogPlanGroupRow) planGroupRow {
	return groupRow(r.Ord, r.QueryId, r.Hash, r.Count, r.Durations, r.FirstSeen, r.LastSeen,
		r.QueryText, r.QueryTextOmittedBytes, r.Findings)
}

func parseScanID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, errors.New("scan_id must be the UUID a plan_insights, query_plans or plan_regressions result carried")
	}

	return id, nil
}

func checkQueryID(s string) error {
	if _, err := strconv.ParseInt(s, 10, 64); err != nil {
		return errors.New("query_id must be a signed 64-bit integer, as pg_stat_statements prints queryid")
	}

	return nil
}

// storedScan reads a snapshot's summary and refuses one taken on another cluster.
func storedScan(ctx context.Context, c *DashaClient, op, cluster string, id uuid.UUID) (*apiclient.LogInsights, error) {
	s, err := c.LogScan(ctx, op, id)
	if err != nil {
		return nil, err
	}

	if s.Scan != nil && s.Scan.ClusterName != cluster {
		return nil, fmt.Errorf("scan %s was taken on cluster %q, not %q", id, s.Scan.ClusterName, cluster)
	}

	return s, nil
}

func scanGroup(ctx context.Context, c *DashaClient, id uuid.UUID, s *apiclient.LogInsights, ord int) (*apiclient.LogPlanGroup, error) {
	for i := range s.Plans.Groups {
		if s.Plans.Groups[i].Ord == ord {
			return &s.Plans.Groups[i], nil
		}
	}

	return c.LogScanGroup(ctx, id, ord)
}

type planInsightsResult struct {
	ScanID        string                          `json:"scan_id,omitempty"`
	Window        planWindow                      `json:"window"`
	Configuration *apiclient.LogPlanConfiguration `json:"configuration,omitempty"`
	Categories    []planCategory                  `json:"categories"`
	Plans         planStats                       `json:"plans"`
	Groups        []planGroupRow                  `json:"groups"`
	Next          string                          `json:"next"`

	cats      []apiclient.LogCategory
	templates int
	available int
}

func planInsights(ctx context.Context, c *DashaClient, a planInsightsArgs) (any, error) {
	st := apiclient.GetLogsInsightsParamsServiceType(cmp.Or(a.ServiceType, string(planStream)))
	if st != apiclient.GetLogsInsightsParamsServiceTypePostgresql && st != apiclient.GetLogsInsightsParamsServiceTypePooler {
		return nil, errors.New("service_type must be 'postgresql' or 'pooler'")
	}

	if a.Limit < 0 || a.Limit > planGroupsMax {
		return nil, fmt.Errorf("limit must be between 1 and %d", planGroupsMax)
	}

	from, to, msg := resolveWindow(a.Since, a.From, a.To, planLogsDefaultSince)
	if msg != "" {
		return nil, errors.New(msg)
	}

	s, err := c.LogInsights(ctx, &apiclient.GetLogsInsightsParams{
		ClusterName: a.Cluster,
		ServiceType: st,
		From:        from,
		To:          to,
		Host:        opt(a.Host),
	})
	if err != nil {
		return nil, err
	}

	return buildPlanInsights(s, planWindowOf(s, from, to, a.Host), cmp.Or(a.Limit, planGroupsMax)), nil
}

func buildPlanInsights(s *apiclient.LogInsights, w planWindow, limit int) *planInsightsResult {
	r := &planInsightsResult{ //nolint:exhaustruct
		Window:        w,
		Configuration: s.Configuration,
		Plans:         planStatsOf(s.Plans),
		cats:          deref(s.Categories),
		templates:     planTemplatesShown,
		available:     len(s.Plans.Groups),
	}

	if s.ScanId != nil {
		r.ScanID = s.ScanId.String()
		r.Next = "query_plans(scan_id, query_id) for the plans of one statement, query_plans(scan_id, ord) for " +
			"one group's tree; plan_regressions(scan_id) against an earlier window"
	} else {
		r.Next = "query_plans(query_id) with the same window for one statement's plans (no snapshot storage: " +
			"it reads the logs again); plan_regressions against an earlier window"
	}

	rows := make([]planGroupRow, 0, min(limit, len(s.Plans.Groups)))
	for _, g := range s.Plans.Groups[:min(limit, len(s.Plans.Groups))] {
		rows = append(rows, rowOfGroup(g))
	}

	r.Groups = rows
	r.Categories = categoriesOf(r.cats, r.templates)

	return r
}

func categoriesOf(cats []apiclient.LogCategory, templates int) []planCategory {
	out := make([]planCategory, 0, len(cats))

	for _, c := range cats {
		pc := planCategory{ //nolint:exhaustruct
			Code:      string(c.Code),
			Count:     c.Count,
			SharePct:  round1(c.Share * 100),
			FirstSeen: c.FirstSeen,
			LastSeen:  c.LastSeen,
		}

		for _, t := range c.Templates[:min(templates, len(c.Templates))] {
			pc.Templates = append(pc.Templates, planTemplate{Template: clipTo(t.Template, planTemplateBytes), Count: t.Count})
		}

		out = append(out, pc)
	}

	return out
}

func (r *planInsightsResult) shrink() (shapedResult, string, bool) {
	if n := max(len(r.Groups)/2, planGroupsFloor); n < len(r.Groups) {
		next := *r
		next.Groups = r.Groups[:n]

		return &next, "limit=" + strconv.Itoa(n), true
	}

	if r.templates > 0 {
		next := *r
		next.templates--
		next.Categories = categoriesOf(r.cats, next.templates)

		return &next, "templates=" + strconv.Itoa(next.templates), true
	}

	return nil, "a shorter window, or host", false
}

func (r *planInsightsResult) note() *shapeNote {
	n := &shapeNote{ //nolint:exhaustruct
		Reason: shapeDefaultView,
		Folded: "plan trees; query text clipped to " + strconv.Itoa(compareQueryBytes) + " bytes; at most " +
			strconv.Itoa(r.templates) + " message templates per category",
		Total: r.Plans.TotalGroups,
		Full:  "query_plans(scan_id) pages every group; query_plans(scan_id, ord) for one tree",
	}

	if len(r.Groups) < r.available {
		n.Folded = "groups past the heaviest by total time; " + n.Folded
	}

	if r.ScanID == "" {
		n.Full = "query_plans(query_id) with the same window"
	}

	return n
}

type planDetail struct {
	Ord             int                         `json:"ord"`
	QueryID         string                      `json:"query_id,omitempty"`
	Hash            string                      `json:"hash"`
	Count           int                         `json:"count"`
	Durations       apiclient.PlanDurationStats `json:"durations"`
	FirstSeen       time.Time                   `json:"first_seen"`
	LastSeen        time.Time                   `json:"last_seen"`
	QueryText       string                      `json:"query_text"`
	QueryParams     string                      `json:"query_params,omitempty"`
	Format          string                      `json:"format"`
	Capabilities    apiclient.PlanCapabilities  `json:"capabilities"`
	DurationMs      *float64                    `json:"duration_ms,omitempty"`
	PlanningTimeMs  *float64                    `json:"planning_time_ms,omitempty"`
	ExecutionTimeMs *float64                    `json:"execution_time_ms,omitempty"`
	Settings        map[string]string           `json:"settings,omitempty"`
	Findings        []apiclient.PlanFinding     `json:"findings"`
	Dormant         []apiclient.PlanDormantRule `json:"dormant,omitempty"`
	Plan            string                      `json:"plan"`
}

type queryPlansResult struct {
	ScanID      string         `json:"scan_id,omitempty"`
	Window      planWindow     `json:"window"`
	QueryID     string         `json:"query_id,omitempty"`
	GroupsTotal int            `json:"groups_total"`
	EmptyReason string         `json:"empty_reason,omitempty"`
	Plans       []planDetail   `json:"plans"`
	OtherGroups []planGroupRow `json:"other_groups,omitempty"`
	Next        string         `json:"next"`

	src   []apiclient.LogPlanGroup
	extra []planGroupRow
	trees int
	opts  planTextOpts
}

type planListResult struct {
	ScanID       string         `json:"scan_id"`
	Window       planWindow     `json:"window"`
	Order        string         `json:"order"`
	WithFindings bool           `json:"with_findings"`
	Total        int            `json:"total"`
	Offset       int            `json:"offset"`
	Groups       []planGroupRow `json:"groups"`
	Next         string         `json:"next"`
}

var planOrders = map[string]apiclient.GetLogsScanGroupsParamsOrder{
	"sum":   apiclient.Sum,
	"max":   apiclient.Max,
	"count": apiclient.Count,
}

func queryPlans(ctx context.Context, c *DashaClient, a queryPlansArgs) (any, error) {
	if a.QueryID != "" {
		if err := checkQueryID(a.QueryID); err != nil {
			return nil, err
		}
	}

	switch {
	case a.Ord != nil:
		return planByOrd(ctx, c, a)
	case a.QueryID != "" && a.ScanID != "":
		return plansFromScan(ctx, c, a)
	case a.QueryID != "":
		return plansFromLogs(ctx, c, a)
	case a.ScanID != "":
		return planList(ctx, c, a)
	default:
		return nil, errors.New("pass query_id for one statement's plans, scan_id to page every group of a " +
			"stored scan, or scan_id with ord for one group's tree; plan_insights gives the overview of a window")
	}
}

func planTrees(a queryPlansArgs) (int, error) {
	if a.Trees < 0 || a.Trees > planTreesMax {
		return 0, fmt.Errorf("trees must be between 1 and %d", planTreesMax)
	}

	return cmp.Or(a.Trees, planTreesDefault), nil
}

func planByOrd(ctx context.Context, c *DashaClient, a queryPlansArgs) (any, error) {
	if a.ScanID == "" {
		return nil, errors.New("ord names a group of a stored scan: pass scan_id with it")
	}

	if *a.Ord < 1 {
		return nil, errors.New("ord starts at 1: take it from a groups row of plan_insights or query_plans(scan_id)")
	}

	id, err := parseScanID(a.ScanID)
	if err != nil {
		return nil, err
	}

	s, err := storedScan(ctx, c, "query_plans", a.Cluster, id)
	if err != nil {
		return nil, err
	}

	g, err := scanGroup(ctx, c, id, s, *a.Ord)
	if err != nil {
		return nil, err
	}

	if a.QueryID != "" && deref(g.QueryId) != a.QueryID {
		return nil, fmt.Errorf("group %d of scan %s belongs to query_id %q, not %q: drop ord to list the "+
			"groups of that query_id", *a.Ord, id, deref(g.QueryId), a.QueryID)
	}

	r := newQueryPlans(id.String(), planWindowOf(s, time.Time{}, time.Time{}, ""), deref(g.QueryId), 1)
	r.src = []apiclient.LogPlanGroup{*g}
	r.trees = 1
	r.render()

	return r, nil
}

func plansFromScan(ctx context.Context, c *DashaClient, a queryPlansArgs) (any, error) {
	trees, err := planTrees(a)
	if err != nil {
		return nil, err
	}

	id, err := parseScanID(a.ScanID)
	if err != nil {
		return nil, err
	}

	s, err := storedScan(ctx, c, "query_plans", a.Cluster, id)
	if err != nil {
		return nil, err
	}

	order := apiclient.Sum
	limit := planListMax

	page, err := c.LogScanGroups(ctx, id, &apiclient.GetLogsScanGroupsParams{ //nolint:exhaustruct
		QueryId: &a.QueryID,
		Order:   &order,
		Limit:   &limit,
	})
	if err != nil {
		return nil, err
	}

	r := newQueryPlans(id.String(), planWindowOf(s, time.Time{}, time.Time{}, ""), a.QueryID, page.Total)

	for _, it := range page.Items[:min(trees, len(page.Items))] {
		g, gErr := scanGroup(ctx, c, id, s, it.Ord)
		if gErr != nil {
			return nil, gErr
		}

		r.src = append(r.src, *g)
	}

	for _, it := range page.Items[len(r.src):] {
		r.extra = append(r.extra, rowOfPageItem(it))
	}

	if page.Total == 0 {
		r.EmptyReason = "not_in_scan"
	}

	r.trees = len(r.src)
	r.render()

	return r, nil
}

func plansFromLogs(ctx context.Context, c *DashaClient, a queryPlansArgs) (any, error) {
	trees, err := planTrees(a)
	if err != nil {
		return nil, err
	}

	from, to, msg := resolveWindow(a.Since, a.From, a.To, planLogsDefaultSince)
	if msg != "" {
		return nil, errors.New(msg)
	}

	limit := planScanGroups

	s, err := c.LogPlans(ctx, &apiclient.GetLogsPlansParams{
		ClusterName: a.Cluster,
		ServiceType: planStream,
		From:        from,
		To:          to,
		Host:        opt(a.Host),
		QueryId:     &a.QueryID,
		Limit:       &limit,
	})
	if err != nil {
		return nil, err
	}

	scanID := ""
	if s.ScanId != nil {
		scanID = s.ScanId.String()
	}

	r := newQueryPlans(scanID, planWindowOf(s, from, to, a.Host), a.QueryID, s.Plans.TotalGroups)
	r.src = s.Plans.Groups
	r.trees = min(trees, len(r.src))

	if s.Plans.EmptyReason != nil {
		r.EmptyReason = string(*s.Plans.EmptyReason)
	}

	r.render()

	return r, nil
}

func newQueryPlans(scanID string, w planWindow, queryID string, total int) *queryPlansResult {
	r := &queryPlansResult{ //nolint:exhaustruct
		ScanID:      scanID,
		Window:      w,
		QueryID:     queryID,
		GroupsTotal: total,
		opts:        planTextOpts{condBytes: planCondBytes, childCap: planChildCap},
		Next: "top_queries / query_report(queryid) for the statement's whole load: these plans are only its " +
			"runs slower than auto_explain.log_min_duration; index_advisor for an index on the scanned table",
	}

	if scanID != "" {
		r.Next = "query_plans(scan_id, ord) for a group listed without a tree; " + r.Next
	}

	return r
}

func (r *queryPlansResult) render() {
	r.Plans = make([]planDetail, 0, r.trees)
	for _, g := range r.src[:r.trees] {
		r.Plans = append(r.Plans, planDetailOf(g, r.opts))
	}

	r.OtherGroups = nil
	for _, g := range r.src[r.trees:] {
		r.OtherGroups = append(r.OtherGroups, rowOfGroup(g))
	}

	r.OtherGroups = append(r.OtherGroups, r.extra...)
}

func planDetailOf(g apiclient.LogPlanGroup, o planTextOpts) planDetail {
	p := g.Plan

	d := planDetail{ //nolint:exhaustruct
		Ord:             g.Ord,
		QueryID:         deref(g.QueryId),
		Hash:            g.Hash,
		Count:           g.Count,
		Durations:       g.Durations,
		FirstSeen:       g.FirstSeen,
		LastSeen:        g.LastSeen,
		QueryText:       clipOmitted(p.QueryText, planQueryTextBytes, deref(p.QueryTextOmittedBytes)),
		Format:          string(p.Format),
		Capabilities:    p.Capabilities,
		DurationMs:      p.DurationMs,
		PlanningTimeMs:  p.PlanningTimeMs,
		ExecutionTimeMs: p.ExecutionTimeMs,
		Settings:        deref(p.Settings),
		Findings:        p.Findings,
		Dormant:         p.Dormant,
		Plan:            renderPlan(p.Root, p.Findings, o),
	}

	if p.QueryParams != nil {
		d.QueryParams = clipOmitted(*p.QueryParams, planParamsBytes, deref(p.QueryParamsOmittedBytes))
	}

	return d
}

func (r *queryPlansResult) shrink() (shapedResult, string, bool) {
	next := *r

	switch {
	case r.trees > 1:
		next.trees = r.trees / 2
		next.render()

		return &next, "trees=" + strconv.Itoa(next.trees), true
	case r.opts.condBytes > planCondFloor:
		next.opts.condBytes = planCondFloor
		next.render()

		return &next, "conditions clipped to " + strconv.Itoa(planCondFloor) + " bytes", true
	case r.opts.childCap > planChildFloor:
		next.opts.childCap = planChildFloor
		next.render()

		return &next, "children per node=" + strconv.Itoa(planChildFloor), true
	}

	return nil, "scan_id with ord for one group", false
}

func (r *queryPlansResult) note() *shapeNote {
	n := &shapeNote{ //nolint:exhaustruct
		Reason: shapeDefaultView,
		Folded: "the slowest plan of each group only; conditions clipped to " + strconv.Itoa(r.opts.condBytes) +
			" bytes; at most " + strconv.Itoa(r.opts.childCap) + " children per node besides those with a finding",
		Total: r.GroupsTotal,
		Full:  "query_plans(scan_id, ord) for a group listed in other_groups",
	}

	if len(r.OtherGroups) > 0 {
		n.Folded = "trees of other_groups; " + n.Folded
	}

	if r.ScanID == "" {
		n.Full = "a narrower window or host"
	}

	return n
}

func planList(ctx context.Context, c *DashaClient, a queryPlansArgs) (any, error) {
	orderName := cmp.Or(a.Order, "sum")

	order, ok := planOrders[orderName]
	if !ok {
		return nil, errors.New("order must be 'sum', 'max' or 'count'")
	}

	if a.Limit < 0 || a.Limit > planListMax {
		return nil, fmt.Errorf("limit must be between 1 and %d", planListMax)
	}

	if a.Offset < 0 {
		return nil, errors.New("offset must not be negative")
	}

	id, err := parseScanID(a.ScanID)
	if err != nil {
		return nil, err
	}

	s, err := storedScan(ctx, c, "query_plans", a.Cluster, id)
	if err != nil {
		return nil, err
	}

	limit := cmp.Or(a.Limit, planListDefault)

	page, err := c.LogScanGroups(ctx, id, &apiclient.GetLogsScanGroupsParams{
		WithFindings: &a.WithFindings,
		Order:        &order,
		Limit:        &limit,
		Offset:       &a.Offset,
	})
	if err != nil {
		return nil, err
	}

	r := &planListResult{
		ScanID:       id.String(),
		Window:       planWindowOf(s, time.Time{}, time.Time{}, ""),
		Order:        orderName,
		WithFindings: a.WithFindings,
		Total:        page.Total,
		Offset:       a.Offset,
		Groups:       make([]planGroupRow, 0, len(page.Items)),
		Next:         "query_plans(scan_id, ord) for a group's tree; query_plans(scan_id, query_id) for every shape of one statement",
	}

	for _, it := range page.Items {
		r.Groups = append(r.Groups, rowOfPageItem(it))
	}

	return r, nil
}

func (r *planListResult) shrink() (shapedResult, string, bool) {
	n := max(len(r.Groups)/2, planListFloor)
	if n >= len(r.Groups) {
		return nil, "a smaller limit", false
	}

	next := *r
	next.Groups = r.Groups[:n]

	return &next, "limit=" + strconv.Itoa(n), true
}

func (r *planListResult) note() *shapeNote {
	n := &shapeNote{ //nolint:exhaustruct
		Reason: shapeDefaultView,
		Folded: "plan trees; query text clipped to " + strconv.Itoa(compareQueryBytes) + " bytes",
		Total:  r.Total,
		Full:   "query_plans(scan_id, ord) for one tree",
	}

	if shown := r.Offset + len(r.Groups); shown < r.Total {
		n.Folded = "groups past offset " + strconv.Itoa(shown) + "; " + n.Folded
		n.Full = "offset=" + strconv.Itoa(shown) + "; " + n.Full
	}

	return n
}

type regressionWindow struct {
	From           time.Time                            `json:"from"`
	To             time.Time                            `json:"to"`
	ScanID         string                               `json:"scan_id,omitempty"`
	Records        int                                  `json:"records"`
	Parsed         int                                  `json:"parsed"`
	Groups         int                                  `json:"groups"`
	CoveredFrom    *time.Time                           `json:"covered_from,omitempty"`
	CoveredTo      *time.Time                           `json:"covered_to,omitempty"`
	Partial        bool                                 `json:"partial"`
	PartialReasons []apiclient.LogInsightsPartialReason `json:"partial_reasons,omitempty"`
	EmptyReason    string                               `json:"empty_reason,omitempty"`
}

type regressionSide struct {
	Count int     `json:"count"`
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	MaxMs float64 `json:"max_ms"`
}

type regressionRow struct {
	QueryID       string         `json:"query_id,omitempty"`
	Severity      string         `json:"severity"`
	Reasons       []string       `json:"reasons"`
	QueryTrunc    string         `json:"query_trunc"`
	QueryLen      int            `json:"query_len"`
	P50Ratio      float64        `json:"p50_ratio"`
	P95Ratio      float64        `json:"p95_ratio"`
	Current       regressionSide `json:"current"`
	Baseline      regressionSide `json:"baseline"`
	LostIndexes   []string       `json:"lost_indexes,omitempty"`
	AddedIndexes  []string       `json:"added_indexes,omitempty"`
	AddedHashes   []string       `json:"added_hashes,omitempty"`
	RemovedHashes []string       `json:"removed_hashes,omitempty"`
}

type planRegressionsResult struct {
	Current         regressionWindow  `json:"current"`
	Baseline        *regressionWindow `json:"baseline,omitempty"`
	BaselineSkipped string            `json:"baseline_skipped,omitempty"`
	Partial         bool              `json:"partial"`
	Caution         string            `json:"caution,omitempty"`
	Returned        int               `json:"returned"`
	Regressions     []regressionRow   `json:"regressions"`
	Next            string            `json:"next"`

	limit int
}

func planRegressions(ctx context.Context, c *DashaClient, a planRegressionsArgs) (any, error) {
	if a.Limit < 0 || a.Limit > regressionsMax {
		return nil, fmt.Errorf("limit must be between 1 and %d", regressionsMax)
	}

	if a.QueryID != "" {
		if err := checkQueryID(a.QueryID); err != nil {
			return nil, err
		}
	}

	limit := cmp.Or(a.Limit, regressionsDefault)
	p := &apiclient.GetLogsPlansCompareParams{ //nolint:exhaustruct
		ClusterName: a.Cluster,
		ServiceType: apiclient.GetLogsPlansCompareParamsServiceTypePostgresql,
		Host:        opt(a.Host),
		QueryId:     opt(a.QueryID),
		Limit:       &limit,
	}

	curFrom, curTo, err := regressionCurrent(ctx, c, a, p)
	if err != nil {
		return nil, err
	}

	if p.BaselineFrom, p.BaselineTo, err = regressionBaseline(a, curFrom, curTo); err != nil {
		return nil, err
	}

	res, err := c.LogPlansCompare(ctx, p)
	if err != nil {
		return nil, err
	}

	return buildRegressions(res, limit), nil
}

func regressionCurrent(
	ctx context.Context, c *DashaClient, a planRegressionsArgs, p *apiclient.GetLogsPlansCompareParams,
) (time.Time, time.Time, error) {
	if a.ScanID == "" {
		from, to, msg := resolveWindow(a.Since, a.From, a.To, planLogsDefaultSince)
		if msg != "" {
			return time.Time{}, time.Time{}, errors.New(msg)
		}

		p.From, p.To = &from, &to

		return from, to, nil
	}

	if a.Since != "" || a.From != "" || a.To != "" {
		return time.Time{}, time.Time{}, errors.New("scan_id already fixes the current window: drop since/from/to")
	}

	id, err := parseScanID(a.ScanID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	p.ScanId = &id

	s, err := storedScan(ctx, c, "plan_regressions", a.Cluster, id)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	if err := regressionScanMatches(s.Scan, a.Host); err != nil {
		return time.Time{}, time.Time{}, err
	}

	if s.Scan == nil {
		if a.BaselineFrom != "" {
			return time.Time{}, time.Time{}, nil
		}

		return time.Time{}, time.Time{}, errors.New("the stored scan does not say its window: pass baseline_from and baseline_to")
	}

	return s.Scan.From, s.Scan.To, nil
}

func regressionScanMatches(info *apiclient.LogScanInfo, host string) error {
	if info == nil {
		return nil
	}

	if info.ServiceType != apiclient.LogScanInfoServiceTypePostgresql {
		return fmt.Errorf("the scan read the %s stream: plan_regressions compares postgresql plans, take a "+
			"scan_id from plan_insights without service_type", info.ServiceType)
	}

	if scanHost := deref(info.Host); host != "" && scanHost != host {
		if scanHost == "" {
			return fmt.Errorf("the scan covers every host of the cluster, not %q: drop host, or take a scan_id "+
				"from plan_insights(host=%q)", host, host)
		}

		return fmt.Errorf("the scan covers host %q, not %q: drop host, or take a scan_id from plan_insights(host=%q)",
			scanHost, host, host)
	}

	return nil
}

func regressionBaseline(a planRegressionsArgs, curFrom, curTo time.Time) (time.Time, time.Time, error) {
	if a.BaselineFrom != "" || a.BaselineTo != "" {
		if a.BaselineShift != "" {
			return time.Time{}, time.Time{}, errors.New("pass baseline_shift or baseline_from/baseline_to, not both")
		}

		from, to, msg := resolveWindow("", a.BaselineFrom, a.BaselineTo, 0)
		if msg != "" {
			return time.Time{}, time.Time{}, errors.New("baseline: " + msg)
		}

		if !curTo.IsZero() && from.Before(curTo) && to.After(curFrom) {
			return time.Time{}, time.Time{}, errors.New("the baseline window overlaps the current one: the two " +
				"would share their plans; end the baseline at or before the current window's start")
		}

		return from, to, nil
	}

	shift := defaultBaselineShift

	if a.BaselineShift != "" {
		d, err := parseSince(a.BaselineShift)
		if err != nil || d <= 0 {
			return time.Time{}, time.Time{}, errors.New("baseline_shift must be a positive duration like '24h' or '7d'")
		}

		shift = d
	}

	if shift < curTo.Sub(curFrom) {
		return time.Time{}, time.Time{}, errors.New("baseline_shift is shorter than the window: the two windows " +
			"would overlap and share their plans; shift by at least the window length")
	}

	return curFrom.Add(-shift), curTo.Add(-shift), nil
}

func regressionWindowOf(w apiclient.LogComparedWindow) regressionWindow {
	s := w.Summary
	out := regressionWindow{ //nolint:exhaustruct
		From:           w.From,
		To:             w.To,
		Records:        s.Plans.Records,
		Parsed:         s.Plans.Parsed,
		Groups:         s.Plans.TotalGroups,
		CoveredFrom:    s.CoveredFrom,
		CoveredTo:      s.CoveredTo,
		Partial:        s.Partial,
		PartialReasons: s.PartialReasons,
	}

	if s.ScanId != nil {
		out.ScanID = s.ScanId.String()
	}

	if s.Plans.EmptyReason != nil {
		out.EmptyReason = string(*s.Plans.EmptyReason)
	}

	return out
}

func buildRegressions(res *apiclient.LogPlanComparison, limit int) *planRegressionsResult {
	r := &planRegressionsResult{ //nolint:exhaustruct
		Current: regressionWindowOf(res.Current),
		Partial: res.Partial,
		limit:   limit,
		Next: "query_plans(scan_id=current.scan_id, query_id) and the same with baseline.scan_id to read " +
			"both plans of a statement; top_queries / query_report(queryid) for its whole load",
	}

	if res.Baseline != nil {
		b := regressionWindowOf(*res.Baseline)
		r.Baseline = &b
	} else {
		r.BaselineSkipped = "the current window holds no plan, so there was nothing to compare and the baseline was not read"
	}

	if r.Partial {
		r.Caution = "a window was read only in part: the ratios compare samples of unknown size — never state " +
			"them as a multiple; new_shape and lost_index stand"
	}

	r.Regressions = make([]regressionRow, 0, len(res.Regressions))
	for _, g := range res.Regressions {
		r.Regressions = append(r.Regressions, regressionRowOf(g))
	}

	r.Returned = len(r.Regressions)

	return r
}

func regressionRowOf(g apiclient.LogPlanRegression) regressionRow {
	q := strings.Join(strings.Fields(g.QueryText), " ")

	reasons := make([]string, 0, len(g.Reasons))
	for _, x := range g.Reasons {
		reasons = append(reasons, string(x))
	}

	side := func(d apiclient.PlanDurationStats, n int) regressionSide {
		return regressionSide{Count: n, P50Ms: round1(d.P50Ms), P95Ms: round1(d.P95Ms), MaxMs: round1(d.MaxMs)}
	}

	return regressionRow{
		QueryID:       deref(g.QueryId),
		Severity:      string(g.Severity),
		Reasons:       reasons,
		QueryTrunc:    truncQuery(q),
		QueryLen:      len(g.QueryText) + deref(g.QueryTextOmittedBytes),
		P50Ratio:      round1(g.P50Ratio),
		P95Ratio:      round1(g.P95Ratio),
		Current:       side(g.Current, g.CurrentCount),
		Baseline:      side(g.Baseline, g.BaselineCount),
		LostIndexes:   g.LostIndexes,
		AddedIndexes:  g.AddedIndexes,
		AddedHashes:   g.AddedHashes,
		RemovedHashes: g.RemovedHashes,
	}
}

func (r *planRegressionsResult) shrink() (shapedResult, string, bool) {
	n := max(len(r.Regressions)/2, regressionsFloor)
	if n >= len(r.Regressions) {
		return nil, "a smaller limit, or query_id", false
	}

	next := *r
	next.Regressions = r.Regressions[:n]
	next.Returned = n

	return &next, "limit=" + strconv.Itoa(n), true
}

func (r *planRegressionsResult) note() *shapeNote {
	n := &shapeNote{ //nolint:exhaustruct
		Reason: shapeDefaultView,
		Folded: "plan trees; query text clipped to " + strconv.Itoa(compareQueryBytes) + " bytes",
		Full:   "query_plans(scan_id, query_id) on either window",
	}

	if r.Returned == r.limit {
		n.Folded = "regressions past limit, worst first; " + n.Folded
		n.Full = "limit=" + strconv.Itoa(regressionsMax) + "; " + n.Full
	}

	return n
}
