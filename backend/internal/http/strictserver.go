package http

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/http/logger"
	"github.com/dbulashev/dasha/internal/repository"
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

func New(
	si serverhttp.StrictServerInterface,
	middlewares []serverhttp.StrictMiddlewareFunc,
	requireHTTPSMW echo.MiddlewareFunc,
	rateLimitMW echo.MiddlewareFunc,
	logsRateLimitMW echo.MiddlewareFunc,
	authMW echo.MiddlewareFunc,
	casbinMW echo.MiddlewareFunc,
	logger *zap.Logger,
) *API {
	// The auth middleware already bridges the user into the request's
	// context.Context (auth.SetUser), so strict handlers read identity via
	// auth.UserFromContext without an extra strict middleware here.
	ssi := serverhttp.NewStrictHandler(si, middlewares)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Debug = true

	e.HTTPErrorHandler = func(err error, c echo.Context) {
		// A stalled database query is not a server fault, and "500 Internal
		// Server Error" tells the user nothing about what happened — report the
		// deadline or the lock explicitly instead.
		if dbErr := mapDatabaseError(err); dbErr != nil {
			if c.Response().Committed {
				return
			}

			logger.Warn("database query did not complete",
				zap.Int("status", dbErr.Code),
				zap.String("method", c.Request().Method),
				zap.String("path", c.Request().URL.Path),
				zap.Error(err),
			)

			// Written here rather than through DefaultHTTPErrorHandler: with
			// e.Debug the default one appends err.Error() to the body, and the
			// internal chain carries the failing SQL. The cause belongs in the
			// log above, not in the answer.
			msg, _ := dbErr.Message.(string)

			var respErr error
			if c.Request().Method == http.MethodHead {
				respErr = c.NoContent(dbErr.Code)
			} else {
				respErr = c.JSON(dbErr.Code, echo.Map{"message": msg})
			}

			if respErr != nil {
				logger.Error("failed to write database error response", zap.Error(respErr))
			}

			return
		}

		// A cancelled request context means the client disconnected — nobody
		// will receive the response, so don't log it as a server error.
		if !c.Response().Committed && !errors.Is(err, context.Canceled) {
			var he *echo.HTTPError
			if errors.As(err, &he) {
				if he.Code >= http.StatusInternalServerError {
					logger.Error("internal server error",
						zap.Int("status", he.Code),
						zap.String("method", c.Request().Method),
						zap.String("path", c.Request().URL.Path),
						zap.Error(err),
					)
				}
			} else {
				logger.Error("internal server error",
					zap.String("method", c.Request().Method),
					zap.String("path", c.Request().URL.Path),
					zap.Error(err),
				)
			}
		}

		e.DefaultHTTPErrorHandler(err, c)
	}

	skipPublic := func(mw echo.MiddlewareFunc) echo.MiddlewareFunc {
		return auth.SkipAuth(
			auth.SkipAuthPrefix(mw, "/auth/"),
			"/api/auth/info",
		)
	}

	e.Use(requireHTTPSMW)
	e.Use(skipPublic(authMW))
	e.Use(rateLimitMW)
	e.Use(logsRateLimitMW)
	e.Use(skipPublic(casbinMW))

	serverhttp.RegisterHandlers(e, ssi)

	return &API{
		Echo:   e,
		Logger: logger,
	}
}

const (
	msgQueryTimeout = "the database did not answer within the query timeout; " +
		"the table may be locked by another transaction, or the instance overloaded"
	msgObjectLocked = "another transaction holds a lock that blocks reading this table " +
		"(running ALTER TABLE, VACUUM FULL, REINDEX, or a transaction left open); " +
		"lock_timeout cancelled the query instead of waiting"
)

// mapDatabaseError turns a database stall into its own HTTP status, or returns
// nil when err is anything else.
func mapDatabaseError(err error) *echo.HTTPError {
	switch {
	case repository.IsLockTimeout(err):
		return echo.NewHTTPError(http.StatusLocked, msgObjectLocked).SetInternal(err)
	case repository.IsTimeout(err):
		return echo.NewHTTPError(http.StatusGatewayTimeout, msgQueryTimeout).SetInternal(err)
	default:
		return nil
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
