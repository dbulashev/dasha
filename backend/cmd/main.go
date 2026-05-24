package main

import (
	"context"
	"errors"
	"log"
	nethttp "net/http"
	"os/signal"
	"sync"
	"syscall"

	strictecho "github.com/oapi-codegen/runtime/strictmiddleware/echo"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/deps"
	"github.com/dbulashev/dasha/internal/http"
	"github.com/dbulashev/dasha/internal/storage"
	"github.com/dbulashev/dasha/internal/version"
)

func main() {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	defer cancel()

	cobra.CheckErr(Execute(ctx))
}

func Execute(ctx context.Context) error {
	var dasha = &cobra.Command{ //nolint: exhaustruct
		Use:   "",
		Short: "dasha",
		CompletionOptions: cobra.CompletionOptions{ //nolint: exhaustruct
			DisableDefaultCmd: true,
		},
		RunE: dashaExec,
	}

	dasha.AddCommand(migrateCmd())

	return dasha.ExecuteContext(ctx) //nolint:wrapcheck
}

func migrateCmd() *cobra.Command {
	return &cobra.Command{ //nolint: exhaustruct
		Use:   "migrate",
		Short: "Create snapshot storage tables and partitions",
		RunE:  migrateExec,
	}
}

func migrateExec(cmd *cobra.Command, _ []string) error {
	container := deps.NewContainer()

	cfg := container.Config()
	logger := container.Logger()

	if !cfg.Storage.Enabled() {
		logger.Fatal("storage.dsn is not configured")
	}

	return storage.Migrate(cmd.Context(), cfg.Storage.DSN, logger)
}

func dashaExec(cmd *cobra.Command, _ []string) error {
	container := deps.NewContainer()

	// _ = container.Config()
	logger := container.Logger()

	_ = zap.ReplaceGlobals(logger)

	defer func(l *zap.Logger) {
		if err := l.Sync(); err != nil {
			log.Printf("can`t sync zap logs: %s", err)
		}
	}(logger)

	serverLogger := logger.With(
		zap.String("buildNumber", version.GetBuildNumber()),
		zap.String("buildDate", version.GetBuildDate()),
	)

	serverLogger.Sugar().Infof("starting Dasha")

	if engine := container.Discovery(); engine != nil {
		if err := engine.Start(cmd.Context()); err != nil {
			serverLogger.Warn("failed to start discovery engine", zap.Error(err))
		}
	}

	var mw []strictecho.StrictEchoMiddlewareFunc

	authMW, err := container.AuthMiddlewares(cmd.Context())
	if err != nil {
		serverLogger.Fatal("failed to initialize auth", zap.Error(err))
	}

	defer authMW.Stop()

	st, err := storage.New(cmd.Context(), container.Config().Storage, serverLogger)
	if err != nil {
		serverLogger.Fatal("failed to initialize storage", zap.Error(err))
	}

	if st != nil {
		defer st.Close()
	}

	d := http.NewDashaHandlers(container.Config(), container.Repository(), st)
	svc := http.New(d, mw, authMW.RequireHTTPS, authMW.RateLimit, authMW.Auth, authMW.Casbin, logger)

	if container.Config().Auth.Mode == config.AuthModeOIDC {
		auth.RegisterBFFRoutes(svc.Echo, authMW.OIDCProvider, authMW.SessionManager, logger)
	}

	wg := sync.WaitGroup{}

	wg.Go(
		func() {
			if err := svc.Serve(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
				serverLogger.Fatal("unexpected error", zap.Error(err))
			}
		})

	<-cmd.Context().Done()

	err = svc.Echo.Shutdown(cmd.Context())
	if err != nil {
		serverLogger.Fatal("unexpected error", zap.Error(err))
	}

	wg.Wait()

	serverLogger.Info("shutdown service")

	return nil
}
