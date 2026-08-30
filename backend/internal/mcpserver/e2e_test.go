package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestE2E_ListAndCallTool drives the real MCP server over an in-memory transport
// against a fake Dasha API, exercising the full path: client CallTool -> server
// tool handler -> DashaClient -> X-API-Key passthrough -> JSON result.
func TestE2E_ListAndCallTool(t *testing.T) {
	t.Parallel()

	const token = "dasha_pat_e2e"

	// Fake Dasha API: requires the passthrough token, serves one cluster.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != token {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if r.URL.Path == "/api/clusters" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"demo","instances":[{"host_name":"h1"}],"databases":["app"]}]`))

			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	client, err := NewDashaClient(Config{DashaURL: backend.URL, Token: token}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()
	srv := NewMCPServer(client, "test", "en")

	st, ct := mcp.NewInMemoryTransports()

	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil) //nolint:exhaustruct

	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// The read-only tools are advertised.
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(lt.Tools) == 0 {
		t.Fatalf("ListTools returned no tools")
	}

	for _, want := range []string{"list_clusters", "get_health_score", "fleet_health"} {
		if !hasTool(lt.Tools, want) {
			t.Errorf("tool %q not advertised", want)
		}
	}

	// CallTool list_clusters: token passes through and the cluster comes back.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_clusters"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if res.IsError {
		t.Fatalf("list_clusters returned IsError: %s", firstText(res))
	}

	if got := firstText(res); !strings.Contains(got, "demo") {
		t.Errorf("result = %q, want it to contain cluster 'demo'", got)
	}
}

// TestE2E_RejectsBadToken confirms an unauthorized Dasha response surfaces as a
// readable isError tool result, not a protocol failure.
func TestE2E_RejectsBadToken(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer backend.Close()

	client, err := NewDashaClient(Config{DashaURL: backend.URL, Token: "wrong"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "en").Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil) //nolint:exhaustruct

	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_clusters"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !res.IsError {
		t.Errorf("expected IsError on 401, got success")
	}

	// Asserted on the status, not the wording: 401 and 403 are worded differently
	// on purpose.
	if got := firstText(res); !strings.Contains(got, "401") {
		t.Errorf("result = %q, want a message naming the 401", got)
	}
}

// TestE2E_ResourcesAndPrompts drives the knowledge-base resources and playbook
// prompts over a real client session (lang=ru): neither touches the Dasha API,
// so no fake backend is needed.
func TestE2E_ResourcesAndPrompts(t *testing.T) {
	t.Parallel()

	client, err := NewDashaClient(Config{DashaURL: "http://unused.invalid", Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "ru").Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil) //nolint:exhaustruct

	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Every knowledge-base resource is advertised.
	lr, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	if len(lr.Resources) != len(kbResourceNames) {
		t.Fatalf("ListResources returned %d resources, want %d", len(lr.Resources), len(kbResourceNames))
	}

	for _, r := range lr.Resources {
		if !strings.HasPrefix(r.URI, "dasha://kb/") {
			t.Errorf("resource URI %q lacks the dasha://kb/ prefix", r.URI)
		}
	}

	// Reading a resource returns the language-selected markdown.
	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "dasha://kb/workflow"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	if len(rr.Contents) != 1 || rr.Contents[0].Text == "" {
		t.Fatalf("ReadResource returned no text")
	}

	if !strings.Contains(rr.Contents[0].Text, "Сценарии") {
		t.Errorf("lang=ru workflow resource is not Russian: %q…", rr.Contents[0].Text[:40])
	}

	// The playbook prompt is numbered, localized and points at the kb.
	gp, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{ //nolint:exhaustruct
		Name:      "diagnose_cluster",
		Arguments: map[string]string{"cluster": "demo", "instance": "h1"},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc, ok := gp.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("prompt content is %T, want *mcp.TextContent", gp.Messages[0].Content)
	}

	for _, want := range []string{"1. get_health_score", "dasha://kb/health-rules", "demo"} {
		if !strings.Contains(tc.Text, want) {
			t.Errorf("diagnose_cluster playbook lacks %q", want)
		}
	}
}

// TestE2E_IndexAdvisor drives the index_advisor tool end to end: the trimming of
// the endpoint's report is what the model actually sees, so it is asserted over
// the wire rather than on the DTO alone.
func TestE2E_IndexAdvisor(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/indexes/advisor" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "candidates": [{
		    "schema": "public", "table": "orders", "columns": ["customer_id"],
		    "predicate": "", "ddl": "CREATE INDEX CONCURRENTLY ON public.orders (customer_id);",
		    "planner_checked": false, "weight_pct": 31.4, "table_rows": 12000000,
		    "writes": {"inserted": 4100, "updated": 980, "deleted": 0, "seq_scans": 12, "idx_scans": 8400},
		    "warnings": [{"code": "similar_index", "names": ["orders_customer_id_idx"]}],
		    "covered_queries": [
		      {"fingerprint": "a3f1c2b9", "weight_pct": 22.1, "calls": 918234, "hosts": ["h1"],
		       "query_id_by_host": {"h1": "8123456789012345"}, "query_ids": ["8123456789012345"],
		       "query": "select * from orders where customer_id = $1"},
		      {"fingerprint": "b1", "weight_pct": 4.0, "calls": 12, "hosts": ["h1"],
		       "query_id_by_host": {"h1": "1"}, "query_ids": ["1"], "query": "select 1"},
		      {"fingerprint": "c1", "weight_pct": 3.0, "calls": 12, "hosts": ["h1"],
		       "query_id_by_host": {"h1": "2"}, "query_ids": ["2"], "query": "select 2"},
		      {"fingerprint": "d1", "weight_pct": 2.0, "calls": 12, "hosts": ["h1"],
		       "query_id_by_host": {"h1": "3"}, "query_ids": ["3"], "query": "select 3"}
		    ]
		  }],
		  "not_parsed": [{"reason_code": "already_indexed", "count": 212}],
		  "unreachable_hosts": ["h3"],
		  "summary": {"pgss_available": true, "analyzed_queries": 500, "collapsed_groups": 341,
		    "not_parsed_count": 212, "covered_time_pct": 44.2, "catalog_truncated": false,
		    "hosts": ["h1", "h2"], "hosts_without_stats": []},
		  "total": 47, "duration_ms": 8123}`))
	}))
	defer backend.Close()

	client, err := NewDashaClient(Config{DashaURL: backend.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()

	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "en").Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil).Connect(ctx, ct, nil) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "index_advisor",
		Arguments: map[string]any{"cluster": "demo", "database": "app"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if res.IsError {
		t.Fatalf("index_advisor returned IsError: %s", firstText(res))
	}

	got := firstText(res)

	for _, want := range []string{
		`"candidates_total":47`,
		`"gaps":["hosts_unreachable"]`,
		`"planner_checked":false`,
		`"covered_queries_total":4`,
		`"already_indexed"`,
		"CREATE INDEX CONCURRENTLY",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("result lacks %s\n%s", want, got)
		}
	}

	if strings.Contains(got, "select * from orders") {
		t.Errorf("statement text must be absent without include_queries:\n%s", got)
	}

	if strings.Contains(got, `"query_ids"`) {
		t.Errorf("query_ids must be dropped in favour of query_id_by_host:\n%s", got)
	}

	if n := strings.Count(got, `"fingerprint"`); n != 3 {
		t.Errorf("covered_queries carries %d entries, want %d", n, maxCoveredPerCandidate)
	}
}

func hasTool(tools []*mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}

	return false
}

func firstText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}

	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}

	return ""
}

// TestE2E_IOTools drives both pg_stat_io tools over a real client session: the
// tools are advertised, arguments reach the history endpoint, and the shaped
// result comes back as readable JSON.
func TestE2E_IOTools(t *testing.T) {
	t.Parallel()

	var (
		mu        sync.Mutex
		lastQuery string
	)

	query := func() string {
		mu.Lock()
		defer mu.Unlock()

		return lastQuery
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/io/history" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		mu.Lock()
		lastQuery = r.URL.RawQuery
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ioHistoryJSON))
	}))
	defer backend.Close()

	client, err := NewDashaClient(Config{DashaURL: backend.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "en").Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil) //nolint:exhaustruct

	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, want := range []string{"io_summary", "io_trend"} {
		if !hasTool(lt.Tools, want) {
			t.Errorf("tool %q not advertised", want)
		}
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "io_summary",
		Arguments: map[string]any{"cluster": "demo", "instance": "h1", "group_by": "full", "top": 5},
	})
	if err != nil {
		t.Fatalf("CallTool(io_summary): %v", err)
	}

	if res.IsError {
		t.Fatalf("io_summary returned IsError: %s", firstText(res))
	}

	if got := firstText(res); !strings.Contains(got, `"ranked_by"`) || !strings.Contains(got, "vacuum") {
		t.Errorf("result = %q, want a ranked table naming the vacuum context", got)
	}

	if q := query(); !strings.Contains(q, "group_by=full") || !strings.Contains(q, "points=1") {
		t.Errorf("history query = %q, want group_by=full and points=1", q)
	}

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "io_trend",
		Arguments: map[string]any{"cluster": "demo", "instance": "h1", "since": "6h"},
	})
	if err != nil {
		t.Fatalf("CallTool(io_trend): %v", err)
	}

	if res.IsError {
		t.Fatalf("io_trend returned IsError: %s", firstText(res))
	}

	if q := query(); !strings.Contains(q, "points=24") || !strings.Contains(q, "group_by=context") {
		t.Errorf("history query = %q, want the trend defaults", q)
	}
}

// TestE2E_IOToolsRejectBadArgs confirms local validation answers as a readable
// isError result without ever reaching Dasha.
func TestE2E_IOToolsRejectBadArgs(t *testing.T) {
	t.Parallel()

	var called atomic.Bool

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)

		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	client, err := NewDashaClient(Config{DashaURL: backend.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "en").Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil) //nolint:exhaustruct

	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "io_summary",
		Arguments: map[string]any{"cluster": "demo", "instance": "h1", "group_by": "object"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !res.IsError {
		t.Errorf("an unknown group_by must be refused")
	}

	if called.Load() {
		t.Errorf("invalid arguments must not reach Dasha")
	}
}
