package mcpserver

import (
	"context"
	"embed"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// kbFS embeds the static knowledge base served as MCP resources, one directory
// per language. Content is reference material for the model (rule thresholds,
// wait-event glossary, diagnostic workflows) — it compensates for LLMs without
// deep PostgreSQL expertise and never triggers outbound calls.
//
//go:embed kb
var kbFS embed.FS

const kbDefaultLang = "en"

// validLang reports whether the knowledge base ships in this language.
func validLang(lang string) bool {
	return lang == "en" || lang == "ru"
}

// registerResources exposes the embedded knowledge base as MCP resources. The
// Description tells the model when to read each one — that is the only nudge a
// weak model gets, so it names the tools whose output the resource explains.
func registerResources(s *mcp.Server, lang string) {
	if !validLang(lang) {
		lang = kbDefaultLang
	}

	for _, r := range []struct {
		name, title, desc string
	}{
		{
			"health-rules", "Health score rules & thresholds",
			"Read before interpreting get_health_score / get_health_recommendations results: " +
				"every rule ID with its LOW/MED/HIGH thresholds, category weights, the critical " +
				"score ceiling and the first action to take per rule.",
		},
		{
			"wait-events", "Wait events glossary",
			"Read before interpreting wait_events results: what each PostgreSQL wait event " +
				"class and frequent event means, and which tool to call next.",
		},
		{
			"workflow", "Diagnostic workflows",
			"Read when unsure which tool to call next: complaint-to-tool-chain playbooks " +
				"(slow database, everything hangs, disk filling, replica lag, app errors, fleet " +
				"triage) and API care rules (rate limits, result size).",
		},
	} {
		uri := "dasha://kb/" + r.name

		s.AddResource(&mcp.Resource{ //nolint:exhaustruct
			URI:         uri,
			Name:        r.name,
			Title:       r.title,
			Description: r.desc,
			MIMEType:    "text/markdown",
		}, kbHandler(uri, lang+"/"+r.name+".md"))
	}
}

// kbHandler serves one embedded knowledge-base file as a resource read.
func kbHandler(uri, path string) mcp.ResourceHandler {
	return func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		b, err := kbFS.ReadFile("kb/" + path)
		if err != nil {
			return nil, fmt.Errorf("mcp: read kb resource %s: %w", uri, err)
		}

		return &mcp.ReadResourceResult{ //nolint:exhaustruct
			Contents: []*mcp.ResourceContents{
				{URI: uri, MIMEType: "text/markdown", Text: string(b)}, //nolint:exhaustruct
			},
		}, nil
	}
}
