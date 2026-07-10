// Package mcpserver implements dasha-mcp: a read-only MCP server over the Dasha
// REST API. It exposes the fleet's PostgreSQL diagnostics (health score,
// recommendations, top queries, indexes, locks, …) as MCP tools/prompts for LLM
// assistants, forwarding each caller's identity (token / personal access token)
// to Dasha so its RBAC is preserved (per-user passthrough). It depends only on
// the generated Dasha API client, never on the backend's internal packages.
package mcpserver

import "time"

// Config configures the dasha-mcp server.
type Config struct {
	// DashaURL is the base URL of the Dasha API (e.g. http://localhost:8000).
	DashaURL string

	// Token is the X-API-Key used for every call in stdio mode (a single
	// identity). In HTTP mode each request carries its own token (passthrough),
	// so this is ignored there.
	Token string

	// Timeout bounds each outbound Dasha API call.
	Timeout time.Duration

	// Lang selects the language of the knowledge-base resources ("en" or "ru").
	// Tool schemas and results stay English regardless.
	Lang string
}

// withDefaults fills unset fields with safe defaults.
func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = 15 * time.Second
	}

	if c.Lang == "" {
		c.Lang = kbDefaultLang
	}

	return c
}
