package http

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/http/logger"
)

type API struct {
	Echo   *echo.Echo
	Logger *zap.Logger
}

type serveOptions struct {
	port                                                      int
	readTimeout, readHeaderTimeout, writeTimeout, idleTimeout time.Duration
	logger                                                    *zap.Logger
}

type ServeOptFunc func(opt *serveOptions)

func WithPort(port int) ServeOptFunc {
	return func(opt *serveOptions) {
		opt.port = port
	}
}

func New(si serverhttp.StrictServerInterface, middlewares []serverhttp.StrictMiddlewareFunc, logger *zap.Logger) *API {
	ssi := serverhttp.NewStrictHandler(si, middlewares)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Debug = true

	serverhttp.RegisterHandlers(e, ssi)

	return &API{
		Echo:   e,
		Logger: logger,
	}
}

const (
	defaultPort              = 8000
	defaultReadTimeout       = time.Second * 30
	defaultReadHeaderTimeout = time.Second * 10
	defaultWriteTimeout      = time.Second * 30
	defaultIdleTimeout       = time.Second * 30
)

func (api *API) Serve(optionalFn ...ServeOptFunc) error {
	opts := serveOptions{
		port:              defaultPort,
		readTimeout:       defaultReadTimeout,
		readHeaderTimeout: defaultReadHeaderTimeout,
		writeTimeout:      defaultWriteTimeout,
		idleTimeout:       defaultIdleTimeout,
		logger:            api.Logger,
	}

	for _, f := range optionalFn {
		f(&opts)
	}

	httpServer := &http.Server{ //nolint:exhaustruct
		Addr:              fmt.Sprintf(":%d", opts.port),
		Handler:           nil,
		TLSConfig:         nil,
		ReadTimeout:       opts.readTimeout,
		ReadHeaderTimeout: opts.readHeaderTimeout,
		WriteTimeout:      opts.writeTimeout,
		IdleTimeout:       opts.idleTimeout,
		MaxHeaderBytes:    0,
		TLSNextProto:      nil,
		ConnState:         nil,
		ErrorLog:          log.New(logger.NewZap(api.Logger), "", 0),
		BaseContext:       nil,
		ConnContext:       nil,
	}

	api.Logger.Info("starting HTTP server", zap.Int("port", opts.port))

	api.Echo.Server = httpServer

	if err := api.Echo.StartServer(httpServer); err != nil {
		return fmt.Errorf("error while start HTTP server | %w", err)
	}

	return nil
}
