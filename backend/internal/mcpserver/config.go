// Package mcpserver implements dasha-mcp: a read-only MCP server over the Dasha
// REST API. It exposes the fleet's PostgreSQL diagnostics (health score,
// recommendations, top queries, indexes, locks, …) as MCP tools/prompts for LLM
// assistants, forwarding each caller's identity (token / personal access token)
// to Dasha so its RBAC is preserved (per-user passthrough). It depends only on
// the generated Dasha API client, never on the backend's internal packages.
package mcpserver

import (
	"time"

	"go.uber.org/zap"
)

// Config configures the dasha-mcp server.
type Config struct {
	// DashaURL is the base URL of the Dasha API (e.g. http://localhost:8000).
	DashaURL string

	// Token is the X-API-Key used for every call in stdio mode (a single
	// identity). In HTTP mode each request carries its own token (passthrough);
	// this Token is used only as a fallback for requests that arrive without any
	// auth header, so leave it unset in HTTP mode to require per-user credentials.
	Token string

	// Timeout bounds each outbound Dasha API call.
	Timeout time.Duration

	// SlowTimeout bounds the calls Dasha builds on demand rather than serving
	// from a cache — currently the index advisor, which parses pg_stat_statements
	// on every host under its own server-side timeout. It must stay above that
	// server timeout (index_advisor.timeout, 60s by default), otherwise the
	// connection is dropped before Dasha can answer.
	SlowTimeout time.Duration

	// Logger receives per-call observability (method, tool, duration, error);
	// arguments and tokens are never logged. Nil disables logging.
	Logger *zap.Logger
}

// withDefaults fills unset fields with safe defaults.
func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = 15 * time.Second
	}

	if c.SlowTimeout <= 0 {
		c.SlowTimeout = 90 * time.Second
	}

	if c.Logger == nil {
		c.Logger = zap.NewNop()
	}

	return c
}
