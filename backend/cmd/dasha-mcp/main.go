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
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/mcpserver"
	"github.com/dbulashev/dasha/internal/version"
)

// httpShutdownTimeout bounds the graceful drain of in-flight requests once
// SIGTERM/SIGINT arrives (Kubernetes sends SIGTERM on pod stop).
const httpShutdownTimeout = 10 * time.Second

func main() {
	dashaURL := flag.String("dasha-url", "http://localhost:8000", "Dasha API base URL")
	httpAddr := flag.String("http", "", "listen address for HTTP/SSE transport (e.g. :8765); empty = stdio")
	timeout := flag.Duration("timeout", 15*time.Second, "per-request timeout for Dasha API calls")
	langFlag := flag.String("lang", "", "knowledge-base language: en|ru (default: $DASHA_MCP_LANG or en)")
	flag.Parse()

	// Logs go to stderr (zap's production default): stdout belongs to the MCP
	// stdio transport and must stay protocol-clean.
	logger := zap.Must(zap.NewProduction())
	defer func() { _ = logger.Sync() }()

	lang := *langFlag
	if lang == "" {
		lang = os.Getenv("DASHA_MCP_LANG")
	}

	if lang == "" {
		lang = "en"
	}

	if lang != "en" && lang != "ru" {
		logger.Fatal("unsupported lang (want en or ru)", zap.String("lang", lang))
	}

	client, err := mcpserver.NewDashaClient(mcpserver.Config{
		DashaURL: *dashaURL,
		Token:    os.Getenv("DASHA_MCP_TOKEN"),
		Timeout:  *timeout,
		Lang:     lang,
		Logger:   logger,
	})
	if err != nil {
		logger.Fatal("failed to build Dasha client", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *httpAddr != "" {
		// HTTP/SSE: each request carries its own token (passthrough); no shared
		// server token is used.
		serveHTTP(ctx, *httpAddr, client, lang, logger)

		return
	}

	// stdio: single identity from DASHA_MCP_TOKEN.
	logger.Info("stdio transport", zap.String("dasha", *dashaURL), zap.String("version", serverVersion()))

	server := mcpserver.NewMCPServer(client, serverVersion(), lang)
	// A cancelled context (SIGINT/SIGTERM) is a clean shutdown: the SDK's Run
	// returns ctx.Err() then, mirroring how the HTTP path treats ErrServerClosed.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal("stdio transport failed", zap.Error(err))
	}
}

// serveHTTP runs the streamable-HTTP transport until the context is cancelled,
// then drains in-flight requests before exiting.
func serveHTTP(ctx context.Context, addr string, client *mcpserver.DashaClient, lang string, logger *zap.Logger) {
	srv := &http.Server{ //nolint:exhaustruct
		Addr:              addr,
		Handler:           mcpserver.HTTPHandler(client, serverVersion(), lang),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("HTTP transport listening", zap.String("addr", addr), zap.String("version", serverVersion()))

	errCh := make(chan error, 1)

	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP transport failed", zap.Error(err))
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("shutdown incomplete", zap.Error(err))
		}

		logger.Info("stopped")
	}
}

// serverVersion resolves the advertised MCP server version: an explicit
// DASHA_MCP_VERSION override, else the release build number stamped via
// ldflags (same internal/version scheme as the backend), else "dev".
func serverVersion() string {
	if v := os.Getenv("DASHA_MCP_VERSION"); v != "" {
		return v
	}

	if v := version.GetBuildNumber(); v != "BUILD_NUMBER" {
		return v
	}

	return "dev"
}
