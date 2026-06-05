package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewMCPServer builds the MCP server with the read-only Dasha tools registered.
// Output type 'any' is used for every tool so the handler fully controls the
// (compact) text content; the input schema is still auto-derived from the typed
// args via json/jsonschema struct tags.
func NewMCPServer(client *DashaClient, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "dasha-mcp", Version: version}, nil)
	registerTools(s, client)

	return s
}

// noArgs is the empty argument set for tools that take no parameters.
type noArgs struct{}

type instanceArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name (from list_clusters)"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
}

type recommendationsArgs struct {
	Cluster  string `json:"cluster" jsonschema:"Dasha cluster name"`
	Instance string `json:"instance" jsonschema:"Dasha instance / host name"`
	Database string `json:"database,omitempty" jsonschema:"Optional: restrict to one database (per-database drill-down)"`
}

func registerTools(s *mcp.Server, c *DashaClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_clusters",
		Description: "List the PostgreSQL clusters Dasha manages, with their hosts. " +
			"Use this first to choose a (cluster, instance) target for the other tools.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		out, err := c.Clusters(ctx)

		return jsonResult(out, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_health_score",
		Description: "Get the instance-level health score (0-100) with per-category breakdown and " +
			"its source (snapshot or metrics) for a cluster/instance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a instanceArgs) (*mcp.CallToolResult, any, error) {
		out, err := c.HealthScore(ctx, a.Cluster, a.Instance)

		return jsonResult(out, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_health_recommendations",
		Description: "Get prioritized health-score recommendations (rule_id, category, severity, " +
			"metric_value) for a cluster/instance. Pass database for the per-database drill-down.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a recommendationsArgs) (*mcp.CallToolResult, any, error) {
		var db *string
		if a.Database != "" {
			db = &a.Database
		}

		out, err := c.Recommendations(ctx, a.Cluster, a.Instance, db)

		return jsonResult(out, err)
	})
}

// jsonResult renders a payload as compact JSON text, or maps an error to an
// isError tool result the model can read and react to (rather than a protocol
// error it cannot see).
func jsonResult(payload any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return errResult(err.Error()), nil, nil
	}

	b, mErr := json.Marshal(payload)
	if mErr != nil {
		return errResult(fmt.Sprintf("mcp: encode result: %v", mErr)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}
