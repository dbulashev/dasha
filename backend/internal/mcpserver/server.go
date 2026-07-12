package mcpserver

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// NewMCPServer builds the MCP server with the read-only Dasha tools registered.
// Output type 'any' is used for every tool so the handler fully controls the
// (compact) text content; the input schema is still auto-derived from the typed
// args via json/jsonschema struct tags. lang selects the knowledge-base
// language ("en"/"ru"; unknown values fall back to "en").
func NewMCPServer(client *DashaClient, version, lang string) *mcp.Server {
	return newServer(client, version, lang, nil)
}

// newServer builds the server with shared options. cache is nil for stdio (one
// server for the process) or a shared *mcp.SchemaCache for HTTP (a server per
// token), where it avoids re-deriving every tool's schema by reflection. The
// instructions, prompt playbooks and knowledge-base resources come in lang
// ("en"/"ru"; unknown falls back to "en").
func newServer(client *DashaClient, version, lang string, cache *mcp.SchemaCache) *mcp.Server {
	if !validLang(lang) {
		lang = kbDefaultLang
	}

	t := textsFor(lang)

	s := mcp.NewServer(&mcp.Implementation{ //nolint:exhaustruct
		Name:    "dasha-mcp",
		Title:   "Dasha PostgreSQL diagnostics",
		Version: version,
	}, &mcp.ServerOptions{ //nolint:exhaustruct
		Instructions: t.instructions,
		SchemaCache:  cache,
	})
	registerTools(s, client)
	registerPrompts(s, t)
	registerResources(s, lang)

	if client.logger != nil {
		s.AddReceivingMiddleware(loggingMiddleware(client.logger))
	}

	return s
}

// loggingMiddleware logs every incoming MCP call — method, target (tool,
// prompt or resource), duration, outcome. Arguments are never logged: they may
// embed user-written filters or query text, and tokens must stay out of logs.
func loggingMiddleware(logger *zap.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			res, err := next(ctx, method, req)

			fields := []zap.Field{
				zap.String("method", method),
				zap.Duration("duration", time.Since(start)),
			}

			switch p := req.GetParams().(type) {
			case *mcp.CallToolParamsRaw:
				fields = append(fields, zap.String("tool", p.Name))
			case *mcp.GetPromptParams:
				fields = append(fields, zap.String("prompt", p.Name))
			case *mcp.ReadResourceParams:
				fields = append(fields, zap.String("resource", p.URI))
			}

			switch {
			case err != nil:
				logger.Warn("mcp call failed", append(fields, zap.Error(err))...)
			default:
				if r, ok := res.(*mcp.CallToolResult); ok && r.IsError {
					fields = append(fields, zap.Bool("is_error", true))
				}

				logger.Info("mcp call", fields...)
			}

			return res, err
		}
	}
}
