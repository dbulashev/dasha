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

	"github.com/dbulashev/dasha/internal/deps"
	"github.com/dbulashev/dasha/internal/http"
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

	return dasha.ExecuteContext(ctx) //nolint:wrapcheck
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

	// Start service discovery if configured.
	if engine := container.Discovery(); engine != nil {
		if err := engine.Start(cmd.Context()); err != nil {
			serverLogger.Warn("failed to start discovery engine", zap.Error(err))
		}
	}

	var mw []strictecho.StrictEchoMiddlewareFunc

	d := http.NewDashaHandlers(container.Config(), container.Repository())
	svc := http.New(d, mw, logger)

	wg := sync.WaitGroup{}

	wg.Go(
		func() {
			if err := svc.Serve(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
				serverLogger.Fatal("unexpected error", zap.Error(err))
			}
		})

	<-cmd.Context().Done()

	err := svc.Echo.Shutdown(cmd.Context())
	if err != nil {
		serverLogger.Fatal("unexpected error", zap.Error(err))
	}

	wg.Wait()

	serverLogger.Info("shutdown service")

	return nil
}
