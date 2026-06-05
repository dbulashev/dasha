// Command dasha-mcp is a read-only MCP server over the Dasha REST API. It runs
// over stdio (for Claude Desktop / IDE) and exposes the fleet's PostgreSQL
// diagnostics as MCP tools, forwarding the configured token to Dasha as
// X-API-Key so its RBAC is preserved.
//
// Configure a Claude Desktop mcpServers entry, for example:
//
//	"dasha": {
//	  "command": "dasha-mcp",
//	  "args": ["--dasha-url", "http://localhost:8000"],
//	  "env": { "DASHA_MCP_TOKEN": "dasha_pat_…" }
//	}
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dbulashev/dasha/internal/mcpserver"
)

func main() {
	dashaURL := flag.String("dasha-url", "http://localhost:8000", "Dasha API base URL")
	timeout := flag.Duration("timeout", 15*time.Second, "per-request timeout for Dasha API calls")
	flag.Parse()

	client, err := mcpserver.NewDashaClient(mcpserver.Config{
		DashaURL: *dashaURL,
		Token:    os.Getenv("DASHA_MCP_TOKEN"),
		Timeout:  *timeout,
	})
	if err != nil {
		log.Fatalf("dasha-mcp: %v", err)
	}

	server := mcpserver.NewMCPServer(client, version())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("dasha-mcp: %v", err)
	}
}

func version() string {
	if v := os.Getenv("DASHA_MCP_VERSION"); v != "" {
		return v
	}

	return "dev"
}
