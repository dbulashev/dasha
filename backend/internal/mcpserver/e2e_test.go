package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

	if got := firstText(res); !strings.Contains(strings.ToLower(got), "access denied") {
		t.Errorf("result = %q, want an access-denied message", got)
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

	// All three knowledge-base resources are advertised.
	lr, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	if len(lr.Resources) != 3 {
		t.Fatalf("ListResources returned %d resources, want 3", len(lr.Resources))
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
