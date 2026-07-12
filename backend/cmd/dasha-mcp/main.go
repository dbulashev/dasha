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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/mcpserver"
	"github.com/dbulashev/dasha/internal/version"
)

// httpShutdownTimeout bounds the graceful drain of in-flight requests once
// SIGTERM/SIGINT arrives (Kubernetes sends SIGTERM on pod stop).
const httpShutdownTimeout = 10 * time.Second

// options carries the CLI flags; secrets stay in env (DASHA_MCP_TOKEN) so they
// never appear in process listings.
type options struct {
	dashaURL string
	httpAddr string
	timeout  time.Duration
	lang     string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cobra.CheckErr(Execute(ctx))
}

// Execute builds and runs the root command (Cobra, matching the backend CLI).
func Execute(ctx context.Context) error {
	var opts options

	cmd := &cobra.Command{ //nolint:exhaustruct
		Use:   "dasha-mcp",
		Short: "Read-only MCP server over the Dasha REST API for AI assistants",
		Long: "dasha-mcp exposes Dasha's PostgreSQL fleet diagnostics as MCP tools, prompts and\n" +
			"knowledge-base resources. It runs over stdio (single identity from DASHA_MCP_TOKEN)\n" +
			"or HTTP/SSE (--http; per-user token passthrough).",
		SilenceUsage: true,
		// cobra.CheckErr in main is the single error printer; without this the
		// error would be printed twice (once by Execute, once by CheckErr).
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.dashaURL, "dasha-url", "http://localhost:8000", "Dasha API base URL")
	f.StringVar(&opts.httpAddr, "http", "", "listen address for HTTP/SSE transport (e.g. :8765); empty = stdio")
	f.DurationVar(&opts.timeout, "timeout", 15*time.Second, "per-request timeout for Dasha API calls")
	f.StringVar(&opts.lang, "lang", "", "knowledge-base language: en|ru (default: $DASHA_MCP_LANG or en)")

	return cmd.ExecuteContext(ctx)
}

func run(ctx context.Context, opts options) error {
	// Logs go to stderr (zap's production default): stdout belongs to the MCP
	// stdio transport and must stay protocol-clean.
	logger := zap.Must(zap.NewProduction())
	defer func() { _ = logger.Sync() }()

	lang := opts.lang
	if lang == "" {
		lang = os.Getenv("DASHA_MCP_LANG")
	}

	if lang == "" {
		lang = "en"
	}

	if !slices.Contains(mcpserver.SupportedLangs(), lang) {
		return fmt.Errorf("unsupported lang %q (supported: %s)", lang, strings.Join(mcpserver.SupportedLangs(), ", "))
	}

	client, err := mcpserver.NewDashaClient(mcpserver.Config{
		DashaURL: opts.dashaURL,
		Token:    os.Getenv("DASHA_MCP_TOKEN"),
		Timeout:  opts.timeout,
		Logger:   logger,
	})
	if err != nil {
		return fmt.Errorf("build Dasha client: %w", err)
	}

	if opts.httpAddr != "" {
		// HTTP/SSE: each request carries its own token (passthrough). A request
		// without an auth header falls back to DASHA_MCP_TOKEN when set; warn so
		// the shared-identity exposure is not a surprise.
		if os.Getenv("DASHA_MCP_TOKEN") != "" {
			logger.Warn("DASHA_MCP_TOKEN is set in HTTP mode: requests without an Authorization or X-API-Key header will use this shared identity; unset it to require per-user credentials")
		}

		return serveHTTP(ctx, opts.httpAddr, client, lang, logger)
	}

	// stdio: single identity from DASHA_MCP_TOKEN.
	logger.Info("stdio transport", zap.String("dasha", opts.dashaURL), zap.String("version", serverVersion()))

	server := mcpserver.NewMCPServer(client, serverVersion(), lang)
	// A cancelled context (SIGINT/SIGTERM) is a clean shutdown: the SDK's Run
	// returns ctx.Err() then, mirroring how the HTTP path treats ErrServerClosed.
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("stdio transport: %w", err)
	}

	return nil
}

// serveHTTP runs the streamable-HTTP transport until the context is cancelled,
// then drains in-flight requests before exiting.
func serveHTTP(ctx context.Context, addr string, client *mcpserver.DashaClient, lang string, logger *zap.Logger) error {
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
			return fmt.Errorf("HTTP transport: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("shutdown incomplete", zap.Error(err))
		}

		logger.Info("stopped")
	}

	return nil
}

// serverVersion resolves the advertised MCP server version: an explicit
// DASHA_MCP_VERSION override, else the release build number stamped via ldflags
// (same internal/version scheme as the backend), else "dev".
func serverVersion() string {
	if v := os.Getenv("DASHA_MCP_VERSION"); v != "" {
		return v
	}

	return version.Resolved()
}
