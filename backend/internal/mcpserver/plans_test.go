package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/google/uuid"
)

var (
	planT0     = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	planScanID = uuid.MustParse("11111111-2222-3333-4444-555555555555")
)

func planGroup(ord int, qid string) apiclient.LogPlanGroup {
	return apiclient.LogPlanGroup{
		Ord:       ord,
		QueryId:   ptr(qid),
		Hash:      "h" + strconv.Itoa(ord),
		Count:     5,
		Durations: apiclient.PlanDurationStats{P50Ms: 10, P95Ms: 20, MaxMs: 30, SumMs: 50}, //nolint:exhaustruct
		FirstSeen: planT0,
		LastSeen:  planT0.Add(time.Minute),
		Plan: apiclient.PlanSummary{ //nolint:exhaustruct
			QueryText: "select * from orders where status = $1",
			Root:      scanNode("orders"),
			Findings: []apiclient.PlanFinding{{ //nolint:exhaustruct
				Code: apiclient.SeqScanLarge, Severity: apiclient.MEDIUM, NodeType: "Seq Scan", Relation: ptr("orders"), Path: []int{},
			}},
		},
	}
}

func insightsFixture(groups int) *apiclient.LogInsights {
	s := &apiclient.LogInsights{ //nolint:exhaustruct
		Scanned: 1000,
		ScanId:  &planScanID,
		Plans:   apiclient.LogPlansSummary{TotalGroups: 40, Records: 90, Parsed: 90}, //nolint:exhaustruct
		Categories: &[]apiclient.LogCategory{{
			Code: apiclient.LogCategoryCodeConnection, Count: 700, Share: 0.7, FirstSeen: planT0, LastSeen: planT0,
			Templates: []apiclient.LogCategoryTemplate{
				{Template: strings.Repeat("t", 500), Count: 3},
				{Template: "b", Count: 2}, {Template: "c", Count: 1}, {Template: "d", Count: 1}, {Template: "e", Count: 1},
			},
		}},
	}

	for i := range groups {
		s.Plans.Groups = append(s.Plans.Groups, planGroup(i, "42"))
	}

	return s
}

func TestPlanInsights_DefaultView(t *testing.T) {
	t.Parallel()

	r := buildPlanInsights(insightsFixture(20), planWindow{From: planT0}, 10) //nolint:exhaustruct

	if len(r.Groups) != 10 || r.ScanID != planScanID.String() {
		t.Fatalf("groups=%d scan_id=%q", len(r.Groups), r.ScanID)
	}

	c := r.Categories[0]
	if c.SharePct != 70 || len(c.Templates) != planTemplatesShown {
		t.Errorf("category = %+v", c)
	}

	if !strings.Contains(c.Templates[0].Template, "truncated, 500 bytes total") {
		t.Errorf("template not clipped: %q", c.Templates[0].Template)
	}

	g := r.Groups[0]
	if g.QueryID != "42" || len(g.Findings) != 1 || g.Findings[0].Code != "seq_scan_large" || g.Findings[0].Relation != "orders" {
		t.Errorf("group row = %+v", g)
	}

	n := r.note()
	if n.Total != 40 || !strings.Contains(n.Folded, "groups past") || !strings.Contains(n.Full, "query_plans(scan_id)") {
		t.Errorf("note = %+v", n)
	}
}

func TestPlanInsights_ShrinkGroupsThenTemplates(t *testing.T) {
	t.Parallel()

	var cur shapedResult = buildPlanInsights(insightsFixture(planGroupsMax), planWindow{}, planGroupsMax) //nolint:exhaustruct

	var steps []string

	for {
		next, step, ok := cur.shrink()
		if !ok {
			if !strings.Contains(step, "shorter window") {
				t.Errorf("floor hint = %q", step)
			}

			break
		}

		steps = append(steps, step)
		cur = next
	}

	want := []string{"limit=5", "limit=3", "templates=2", "templates=1", "templates=0"}
	if strings.Join(steps, ",") != strings.Join(want, ",") {
		t.Errorf("steps = %v, want %v", steps, want)
	}

	if r := cur.(*planInsightsResult); len(r.Categories[0].Templates) != 0 || len(r.Groups) != planGroupsFloor {
		t.Errorf("floor = %+v", r)
	}
}

func TestPlanInsights_PartialCaution(t *testing.T) {
	t.Parallel()

	s := insightsFixture(1)
	s.Partial = true

	if w := planWindowOf(s, planT0, planT0.Add(time.Hour), ""); w.Caution == "" {
		t.Error("a partial window must carry a caution")
	}

	s.Partial = false
	s.Scan = &apiclient.LogScanInfo{From: planT0.Add(-time.Hour), To: planT0, Host: ptr("h2")} //nolint:exhaustruct

	if w := planWindowOf(s, time.Time{}, time.Time{}, ""); !w.From.Equal(planT0.Add(-time.Hour)) || w.Host != "h2" || w.Caution != "" {
		t.Errorf("window from a stored scan = %+v", w)
	}
}

type planAPI struct {
	mu       sync.Mutex
	requests []string
	cluster  string
	inScan   int
	groups   map[int]apiclient.LogPlanGroup
	status   int
	compare  *apiclient.LogPlanComparison
}

func (p *planAPI) server(t *testing.T) *DashaClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests = append(p.requests, r.URL.Path+"?"+r.URL.RawQuery)
		p.mu.Unlock()

		if p.status != 0 {
			w.WriteHeader(p.status)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		prefix := "/api/logs/scans/" + planScanID.String()

		switch {
		case r.URL.Path == prefix:
			s := insightsFixture(p.inScan)
			s.Scan = &apiclient.LogScanInfo{ //nolint:exhaustruct
				ClusterName: p.cluster, ServiceType: apiclient.LogScanInfoServiceTypePostgresql, From: planT0.Add(-time.Hour), To: planT0,
			}
			_ = json.NewEncoder(w).Encode(s)
		case r.URL.Path == prefix+"/groups":
			page := apiclient.LogPlanGroupPage{Total: len(p.groups)} //nolint:exhaustruct
			for i := range len(p.groups) {
				g := p.groups[i]
				page.Items = append(page.Items, apiclient.LogPlanGroupRow{ //nolint:exhaustruct
					Ord: g.Ord, QueryId: g.QueryId, Hash: g.Hash, QueryText: g.Plan.QueryText, Findings: g.Plan.Findings,
				})
			}

			_ = json.NewEncoder(w).Encode(page)
		case strings.HasPrefix(r.URL.Path, prefix+"/groups/"):
			ord, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, prefix+"/groups/"))
			_ = json.NewEncoder(w).Encode(p.groups[ord])
		case r.URL.Path == "/api/logs/plans/compare":
			_ = json.NewEncoder(w).Encode(p.compare)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func (p *planAPI) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.requests...)
}

func TestQueryPlans_FromScanFetchesTopTrees(t *testing.T) {
	t.Parallel()

	api := &planAPI{cluster: "demo", groups: map[int]apiclient.LogPlanGroup{}} //nolint:exhaustruct
	for i := range 4 {
		api.groups[i] = planGroup(i, "42")
	}

	c := api.server(t)

	out, err := queryPlans(context.Background(), c, queryPlansArgs{ //nolint:exhaustruct
		Cluster: "demo", ScanID: planScanID.String(), QueryID: "42", Trees: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := out.(*queryPlansResult)
	if len(r.Plans) != 2 || len(r.OtherGroups) != 2 || r.GroupsTotal != 4 {
		t.Fatalf("plans=%d other=%d total=%d", len(r.Plans), len(r.OtherGroups), r.GroupsTotal)
	}

	if !strings.Contains(r.Plans[0].Plan, "Seq Scan on public.orders") || !strings.Contains(r.Plans[0].Plan, "!! seq_scan_large") {
		t.Errorf("tree = %q", r.Plans[0].Plan)
	}

	if !r.Window.From.Equal(planT0.Add(-time.Hour)) {
		t.Errorf("window must come from the stored scan: %+v", r.Window)
	}

	trees := 0

	for _, q := range api.seen() {
		if strings.Contains(q, "/groups/") {
			trees++
		}

		if strings.Contains(q, "/groups?") && !strings.Contains(q, "query_id=42") {
			t.Errorf("groups page without query_id: %s", q)
		}
	}

	if trees != 2 {
		t.Errorf("fetched %d trees, want 2: %v", trees, api.seen())
	}
}

func TestQueryPlans_RefusesScanOfAnotherCluster(t *testing.T) {
	t.Parallel()

	api := &planAPI{cluster: "other", groups: map[int]apiclient.LogPlanGroup{1: planGroup(1, "1")}} //nolint:exhaustruct
	c := api.server(t)

	_, err := queryPlans(context.Background(), c, queryPlansArgs{Cluster: "demo", ScanID: planScanID.String(), Ord: ptr(1)}) //nolint:exhaustruct
	if err == nil || !strings.Contains(err.Error(), `"other"`) {
		t.Errorf("err = %v", err)
	}
}

func TestQueryPlans_ReusesTreesOfScanSummary(t *testing.T) {
	t.Parallel()

	api := &planAPI{cluster: "demo", inScan: 2, groups: map[int]apiclient.LogPlanGroup{}} //nolint:exhaustruct
	for i := range 4 {
		api.groups[i] = planGroup(i, "42")
	}

	c := api.server(t)

	out, err := queryPlans(context.Background(), c, queryPlansArgs{ //nolint:exhaustruct
		Cluster: "demo", ScanID: planScanID.String(), QueryID: "42", Trees: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if r := out.(*queryPlansResult); len(r.Plans) != 3 {
		t.Fatalf("plans = %d", len(r.Plans))
	}

	var fetched []string

	for _, q := range api.seen() {
		if strings.Contains(q, "/groups/") {
			fetched = append(fetched, q)
		}
	}

	if len(fetched) != 1 || !strings.Contains(fetched[0], "/groups/2?") {
		t.Errorf("tree requests = %v, want only the group missing from the scan summary", fetched)
	}
}

func TestQueryPlans_OrdChecks(t *testing.T) {
	t.Parallel()

	api := &planAPI{cluster: "demo", groups: map[int]apiclient.LogPlanGroup{3: planGroup(3, "7")}} //nolint:exhaustruct
	c := api.server(t)

	_, err := queryPlans(context.Background(), c, queryPlansArgs{Cluster: "demo", ScanID: planScanID.String(), Ord: ptr(0)}) //nolint:exhaustruct
	if err == nil || !strings.Contains(err.Error(), "ord starts at 1") {
		t.Errorf("ord 0: err = %v", err)
	}

	_, err = queryPlans(context.Background(), c, queryPlansArgs{ //nolint:exhaustruct
		Cluster: "demo", ScanID: planScanID.String(), Ord: ptr(3), QueryID: "42",
	})
	if err == nil || !strings.Contains(err.Error(), `query_id "7"`) {
		t.Errorf("foreign group: err = %v", err)
	}

	if _, err = queryPlans(context.Background(), c, queryPlansArgs{ //nolint:exhaustruct
		Cluster: "demo", ScanID: planScanID.String(), Ord: ptr(3), QueryID: "7",
	}); err != nil {
		t.Errorf("own group: %v", err)
	}
}

func TestGroupRow_QueryLenCountsRawText(t *testing.T) {
	t.Parallel()

	text := "select *\n  from orders\n  where id = $1"
	r := groupRow(1, nil, "h", 1, apiclient.PlanDurationStats{}, planT0, planT0, text, ptr(100), nil) //nolint:exhaustruct

	if r.QueryTrunc != "select * from orders where id = $1" || r.QueryLen != len(text)+100 {
		t.Errorf("trunc = %q, len = %d", r.QueryTrunc, r.QueryLen)
	}
}

func TestRegressionScanMatches(t *testing.T) {
	t.Parallel()

	pg := apiclient.LogScanInfoServiceTypePostgresql

	for _, tc := range []struct {
		info *apiclient.LogScanInfo
		host string
		ok   bool
	}{
		{nil, "h1", true},
		{&apiclient.LogScanInfo{ServiceType: pg}, "", true},                                      //nolint:exhaustruct
		{&apiclient.LogScanInfo{ServiceType: pg, Host: ptr("h1")}, "h1", true},                   //nolint:exhaustruct
		{&apiclient.LogScanInfo{ServiceType: pg, Host: ptr("h1")}, "", true},                     //nolint:exhaustruct
		{&apiclient.LogScanInfo{ServiceType: pg, Host: ptr("h1")}, "h2", false},                  //nolint:exhaustruct
		{&apiclient.LogScanInfo{ServiceType: pg}, "h2", false},                                   //nolint:exhaustruct
		{&apiclient.LogScanInfo{ServiceType: apiclient.LogScanInfoServiceTypePooler}, "", false}, //nolint:exhaustruct
	} {
		if err := regressionScanMatches(tc.info, tc.host); (err == nil) != tc.ok {
			t.Errorf("%+v host=%q: err = %v", tc.info, tc.host, err)
		}
	}
}

func TestQueryPlans_ArgumentErrors(t *testing.T) {
	t.Parallel()

	c := (&planAPI{}).server(t) //nolint:exhaustruct

	for _, a := range []queryPlansArgs{
		{Cluster: "demo"},                          //nolint:exhaustruct
		{Cluster: "demo", Ord: ptr(1)},             //nolint:exhaustruct
		{Cluster: "demo", QueryID: "abc"},          //nolint:exhaustruct
		{Cluster: "demo", ScanID: "not-a-uuid"},    //nolint:exhaustruct
		{Cluster: "demo", QueryID: "1", Trees: 99}, //nolint:exhaustruct
	} {
		if _, err := queryPlans(context.Background(), c, a); err == nil {
			t.Errorf("%+v: want an argument error", a)
		}
	}
}

func TestQueryPlans_GoneScanIsNotANameMiss(t *testing.T) {
	t.Parallel()

	c := (&planAPI{status: http.StatusNotFound}).server(t) //nolint:exhaustruct

	_, err := queryPlans(context.Background(), c, queryPlansArgs{Cluster: "demo", ScanID: planScanID.String()}) //nolint:exhaustruct
	if err == nil || !strings.Contains(err.Error(), "plan_insights again") || strings.HasPrefix(err.Error(), errNotFound.Error()) {
		t.Errorf("err = %v", err)
	}
}

func TestQueryPlans_ShrinkOrder(t *testing.T) {
	t.Parallel()

	r := newQueryPlans("", planWindow{}, "42", 4) //nolint:exhaustruct
	for i := range 4 {
		r.src = append(r.src, planGroup(i, "42"))
	}

	r.trees = 4
	r.render()

	var cur shapedResult = r

	var steps []string

	for {
		next, step, ok := cur.shrink()
		if !ok {
			if !strings.Contains(step, "ord") {
				t.Errorf("floor hint = %q", step)
			}

			break
		}

		steps = append(steps, step)
		cur = next
	}

	if len(steps) != 4 || steps[0] != "trees=2" || steps[1] != "trees=1" ||
		!strings.HasPrefix(steps[2], "conditions") || !strings.HasPrefix(steps[3], "children") {
		t.Errorf("steps = %v", steps)
	}

	if f := cur.(*queryPlansResult); len(f.Plans) != 1 || len(f.OtherGroups) != 3 {
		t.Errorf("floor: plans=%d other=%d", len(f.Plans), len(f.OtherGroups))
	}
}

func TestRegressionBaseline(t *testing.T) {
	t.Parallel()

	from, to := planT0.Add(-time.Hour), planT0

	bf, bt, err := regressionBaseline(planRegressionsArgs{}, from, to) //nolint:exhaustruct
	if err != nil || !bf.Equal(from.Add(-24*time.Hour)) || !bt.Equal(to.Add(-24*time.Hour)) {
		t.Errorf("default shift: %v %v %v", bf, bt, err)
	}

	if _, _, err := regressionBaseline(planRegressionsArgs{BaselineShift: "7d"}, from, to); err != nil { //nolint:exhaustruct
		t.Errorf("7d: %v", err)
	}

	adjacent := planRegressionsArgs{ //nolint:exhaustruct
		BaselineFrom: from.Add(-time.Hour).Format(time.RFC3339), BaselineTo: from.Format(time.RFC3339),
	}
	if _, _, err := regressionBaseline(adjacent, from, to); err != nil {
		t.Errorf("adjacent: %v", err)
	}

	for _, a := range []planRegressionsArgs{
		{BaselineShift: "30m"}, //nolint:exhaustruct
		{BaselineFrom: from.Add(-time.Hour).Format(time.RFC3339), BaselineTo: from.Add(time.Minute).Format(time.RFC3339)}, //nolint:exhaustruct
		{BaselineShift: "24h", BaselineFrom: "2026-09-24T09:00:00Z", BaselineTo: "2026-09-24T10:00:00Z"},                  //nolint:exhaustruct
		{BaselineFrom: "2026-09-24T09:00:00Z"}, //nolint:exhaustruct
	} {
		if _, _, err := regressionBaseline(a, from, to); err == nil {
			t.Errorf("%+v: want an error", a)
		}
	}
}

func TestPlanRegressions_ScanWindowShiftsBaseline(t *testing.T) {
	t.Parallel()

	api := &planAPI{cluster: "demo", compare: &apiclient.LogPlanComparison{ //nolint:exhaustruct
		Partial: true,
		Regressions: []apiclient.LogPlanRegression{{ //nolint:exhaustruct
			QueryId: ptr("42"), Severity: apiclient.LogPlanRegressionSeverityHIGH,
			Reasons: []apiclient.LogPlanRegressionReason{apiclient.LostIndex}, LostIndexes: []string{"orders_status_idx"},
			QueryText: "select  *\n from orders", CurrentCount: 30, BaselineCount: 25,
		}},
	}}
	c := api.server(t)

	out, err := planRegressions(context.Background(), c, planRegressionsArgs{Cluster: "demo", ScanID: planScanID.String(), BaselineShift: "7d"}) //nolint:exhaustruct
	if err != nil {
		t.Fatal(err)
	}

	r := out.(*planRegressionsResult)
	if r.Caution == "" || r.Baseline != nil || r.BaselineSkipped == "" {
		t.Errorf("partial/baseline: %+v", r)
	}

	if g := r.Regressions[0]; g.QueryTrunc != "select * from orders" || g.Current.Count != 30 || g.LostIndexes[0] != "orders_status_idx" {
		t.Errorf("row = %+v", g)
	}

	var cmpQuery string

	for _, q := range api.seen() {
		if strings.HasPrefix(q, "/api/logs/plans/compare?") {
			cmpQuery = q
		}
	}

	wantFrom := planT0.Add(-time.Hour - 7*24*time.Hour).Format(time.RFC3339)
	if !strings.Contains(cmpQuery, "scan_id="+planScanID.String()) || !strings.Contains(cmpQuery, "baseline_from="+strings.ReplaceAll(wantFrom, ":", "%3A")) ||
		strings.Contains(cmpQuery, "&from=") {
		t.Errorf("compare query = %s", cmpQuery)
	}
}

func TestPlanRegressions_ScanRejectsWindow(t *testing.T) {
	t.Parallel()

	c := (&planAPI{}).server(t) //nolint:exhaustruct

	_, err := planRegressions(context.Background(), c, planRegressionsArgs{Cluster: "demo", ScanID: planScanID.String(), Since: "1h"}) //nolint:exhaustruct
	if err == nil || !strings.Contains(err.Error(), "scan_id") {
		t.Errorf("err = %v", err)
	}
}

func TestAdvisorCandidate_CarriesEvidence(t *testing.T) {
	t.Parallel()

	c := advisorCandidateOf(apiclient.IndexAdvisorCandidate{ //nolint:exhaustruct
		Evidence: apiclient.IndexAdvisorEvidence{State: apiclient.Found, Plans: ptr(47)}, //nolint:exhaustruct
	}, false)

	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), `"evidence":{"plans":47,"state":"found"}`) {
		t.Errorf("candidate = %s", b)
	}
}
