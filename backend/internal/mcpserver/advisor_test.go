package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/gen/apiclient"
)

func advisorCoveredOf(fingerprint string, weight float64, query string) apiclient.IndexAdvisorCoveredQuery {
	return apiclient.IndexAdvisorCoveredQuery{
		Calls:         100,
		Fingerprint:   fingerprint,
		Hosts:         []string{"h1"},
		Query:         query,
		QueryIdByHost: map[string]string{"h1": "8123456789012345"},
		QueryIds:      []string{"8123456789012345"},
		WeightPct:     weight,
	}
}

func advisorReportOf(covered []apiclient.IndexAdvisorCoveredQuery) *apiclient.IndexAdvisorReport {
	return &apiclient.IndexAdvisorReport{
		Candidates: []apiclient.IndexAdvisorCandidate{{
			Columns:        []string{"customer_id"},
			CoveredQueries: covered,
			Ddl:            "CREATE INDEX CONCURRENTLY ON public.orders (customer_id);",
			PlannerChecked: false,
			Predicate:      "",
			Schema:         "public",
			Table:          "orders",
			TableRows:      12000000,
			Warnings:       []apiclient.IndexAdvisorWarning{},
			WeightPct:      31.4,
			Writes:         apiclient.IndexAdvisorWrites{Deleted: 0, IdxScans: 8400, Inserted: 4100, SeqScans: 12, Updated: 980},
		}},
		DurationMs:       8123,
		NotParsed:        []apiclient.IndexAdvisorNotParsed{},
		Summary:          advisorFullSummary(),
		Total:            1,
		UnreachableHosts: []string{},
	}
}

// advisorFullSummary is a report with nothing missing: every gap must be absent.
func advisorFullSummary() apiclient.IndexAdvisorSummary {
	return apiclient.IndexAdvisorSummary{
		AnalyzedQueries:   500,
		CatalogTruncated:  false,
		CollapsedGroups:   341,
		CoveredTimePct:    44.2,
		Hosts:             []string{"h1", "h2"},
		HostsWithoutStats: []string{},
		NotParsedCount:    0,
		PgssAvailable:     true,
	}
}

func TestAdvisor_QueryTextOnlyOnRequest(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", maxAdvisorQueryBytes*2)
	rep := advisorReportOf([]apiclient.IndexAdvisorCoveredQuery{advisorCoveredOf("a3f1c2b9", 22.1, long)})

	got := advisorReport("app", rep, false)
	if q := got.Candidates[0].CoveredQueries[0].Query; q != "" {
		t.Errorf("query = %q, want empty without include_queries", q)
	}

	got = advisorReport("app", rep, true)

	q := got.Candidates[0].CoveredQueries[0].Query
	if len(q) > maxAdvisorQueryBytes+64 {
		t.Errorf("query is %d bytes, want it clipped to about %d", len(q), maxAdvisorQueryBytes)
	}

	if !strings.Contains(q, "[truncated,") {
		t.Errorf("clipped query must carry the truncation marker, got tail %q", q[max(0, len(q)-40):])
	}
}

func TestAdvisor_CoveredQueriesRankedAndCapped(t *testing.T) {
	t.Parallel()

	rep := advisorReportOf([]apiclient.IndexAdvisorCoveredQuery{
		advisorCoveredOf("low", 1.5, "select 1"),
		advisorCoveredOf("top", 22.1, "select 2"),
		advisorCoveredOf("mid", 9.9, "select 3"),
		advisorCoveredOf("tail", 0.2, "select 4"),
		advisorCoveredOf("second", 12.0, "select 5"),
	})

	c := advisorReport("app", rep, false).Candidates[0]

	if c.CoveredQueriesTotal != 5 {
		t.Errorf("covered_queries_total = %d, want 5 (the count before trimming)", c.CoveredQueriesTotal)
	}

	if len(c.CoveredQueries) != maxCoveredPerCandidate {
		t.Fatalf("covered_queries = %d entries, want %d", len(c.CoveredQueries), maxCoveredPerCandidate)
	}

	want := []string{"top", "second", "mid"}
	for i, q := range c.CoveredQueries {
		if q.Fingerprint != want[i] {
			t.Errorf("covered_queries[%d] = %q, want %q (weight_pct descending)", i, q.Fingerprint, want[i])
		}
	}
}

func TestAdvisor_Limit(t *testing.T) {
	t.Parallel()

	rep := advisorReportOf([]apiclient.IndexAdvisorCoveredQuery{advisorCoveredOf("a3f1c2b9", 22.1, "select 1")})
	rep.Total = 47

	var query url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rep)
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	for _, tc := range []struct {
		in   int
		want string
	}{
		{0, "10"},
		{100, "30"},
		{7, "7"},
	} {
		got, err := indexAdvisor(context.Background(), c, indexAdvisorArgs{
			Cluster: "demo", Database: "app", ExcludeUsers: nil, Limit: tc.in, IncludeQueries: false,
		})
		if err != nil {
			t.Fatalf("indexAdvisor(limit=%d): %v", tc.in, err)
		}

		if q := query.Get("limit"); q != tc.want {
			t.Errorf("limit=%d reached the API as %q, want %q", tc.in, q, tc.want)
		}

		res, ok := got.(advisorResult)
		if !ok {
			t.Fatalf("indexAdvisor returned %T, want advisorResult", got)
		}

		if res.CandidatesTotal != 47 {
			t.Errorf("candidates_total = %d, want 47 (the endpoint's total, not the page length)", res.CandidatesTotal)
		}

		if res.CandidatesReturned != 1 {
			t.Errorf("candidates_returned = %d, want 1", res.CandidatesReturned)
		}
	}
}

func TestAdvisor_Gaps(t *testing.T) {
	t.Parallel()

	full := func() *apiclient.IndexAdvisorReport {
		return advisorReportOf([]apiclient.IndexAdvisorCoveredQuery{advisorCoveredOf("a3f1c2b9", 22.1, "select 1")})
	}

	for _, tc := range []struct {
		name string
		mut  func(r *apiclient.IndexAdvisorReport)
		want []string
	}{
		{"complete", func(*apiclient.IndexAdvisorReport) {}, []string{}},
		{
			"pgss", func(r *apiclient.IndexAdvisorReport) { r.Summary.PgssAvailable = false },
			[]string{gapPgssUnavailable},
		},
		{
			"unreachable", func(r *apiclient.IndexAdvisorReport) { r.UnreachableHosts = []string{"h3"} },
			[]string{gapHostsUnreachable},
		},
		{
			"no_stats", func(r *apiclient.IndexAdvisorReport) { r.Summary.HostsWithoutStats = []string{"h2"} },
			[]string{gapHostsNoStats},
		},
		{
			"no_workload", func(r *apiclient.IndexAdvisorReport) {
				r.Summary.AnalyzedQueries = 0
				r.Summary.CollapsedGroups = 0
			},
			[]string{gapNoWorkload},
		},
		{
			"no_workload_without_pgss", func(r *apiclient.IndexAdvisorReport) {
				r.Summary.PgssAvailable = false
				r.Summary.AnalyzedQueries = 0
				r.Summary.CollapsedGroups = 0
			},
			[]string{gapPgssUnavailable},
		},
		{
			"unparsed", func(r *apiclient.IndexAdvisorReport) {
				r.NotParsed = []apiclient.IndexAdvisorNotParsed{{ReasonCode: "truncated", Count: 100}}
			},
			[]string{gapPartlyUnparsed},
		},
		{
			"unparsed_below_threshold", func(r *apiclient.IndexAdvisorReport) {
				r.NotParsed = []apiclient.IndexAdvisorNotParsed{{ReasonCode: "truncated", Count: 99}}
			},
			[]string{},
		},
		{
			"healthy_reasons", func(r *apiclient.IndexAdvisorReport) {
				r.NotParsed = []apiclient.IndexAdvisorNotParsed{
					{ReasonCode: "already_indexed", Count: 212},
					{ReasonCode: "system_relation", Count: 288},
				}
			},
			[]string{},
		},
		{
			"analysis_stage_counted_per_group", func(r *apiclient.IndexAdvisorReport) {
				r.NotParsed = []apiclient.IndexAdvisorNotParsed{{ReasonCode: "unknown_relation", Count: 70}}
			},
			[]string{gapPartlyUnparsed},
		},
		{
			"unsupported_type_is_not_a_gap", func(r *apiclient.IndexAdvisorReport) {
				r.NotParsed = []apiclient.IndexAdvisorNotParsed{{ReasonCode: "unsupported_type", Count: 400}}
			},
			[]string{},
		},
		{
			"unknown_reason", func(r *apiclient.IndexAdvisorReport) {
				r.NotParsed = []apiclient.IndexAdvisorNotParsed{{ReasonCode: "brand_new_code", Count: 400}}
			},
			[]string{gapPartlyUnparsed},
		},
		{
			"catalog", func(r *apiclient.IndexAdvisorReport) { r.Summary.CatalogTruncated = true },
			[]string{gapCatalogTruncated},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rep := full()
			tc.mut(rep)

			if got := advisorReport("app", rep, false).Gaps; !slices.Equal(got, tc.want) {
				t.Errorf("gaps = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAdvisor_NotParsedSortedByCount(t *testing.T) {
	t.Parallel()

	rep := advisorReportOf(nil)
	rep.NotParsed = []apiclient.IndexAdvisorNotParsed{
		{ReasonCode: "system_relation", Count: 88},
		{ReasonCode: "already_indexed", Count: 212},
	}

	got := advisorReport("app", rep, false).NotParsed
	if got[0].ReasonCode != "already_indexed" {
		t.Errorf("not_parsed[0] = %q, want the largest count first", got[0].ReasonCode)
	}
}

func TestIndexAdvisor_NotFoundNamesBothCauses(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	_, err = c.IndexAdvisor(context.Background(), "demo", "app", nil, 10)
	if err == nil {
		t.Fatalf("IndexAdvisor on 404 must return an error")
	}

	for _, want := range []string{"disabled", "list_clusters", "no candidates"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("404 message %q does not mention %q", err.Error(), want)
		}
	}
}

func TestIndexAdvisor_TimeoutIsExplained(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	_, err = c.IndexAdvisor(context.Background(), "demo", "app", nil, 10)
	if err == nil || !strings.Contains(err.Error(), "never cached") {
		t.Errorf("504 error = %v, want the on-demand/never-cached explanation", err)
	}
}
