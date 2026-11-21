package http

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/piicatalog"
)

// Access control and the audit log live in auth.NewPIICatalogGate; these
// handlers are reached only by an allowed, already logged caller.

var errPIIStorageMissing = errors.New("pii catalog: snapshot storage is not configured")

func (s *Handlers) GetPiiCatalogStatus(
	ctx context.Context, req serverhttp.GetPiiCatalogStatusRequestObject,
) (serverhttp.GetPiiCatalogStatusResponseObject, error) {
	if s.pii == nil {
		return nil, errPIIStorageMissing
	}

	var cluster string
	if req.Params.ClusterName != nil {
		cluster = *req.Params.ClusterName
	}

	statuses, err := s.pii.Statuses(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("GetPiiCatalogStatus | %w", err)
	}

	items := make([]serverhttp.PiiCatalogDatabaseStatus, len(statuses))
	for i, ds := range statuses {
		st := ds.Status
		items[i] = serverhttp.PiiCatalogDatabaseStatus{
			ClusterName:    ds.Key.Cluster,
			Database:       ds.Key.Database,
			InConfig:       ds.InConfig,
			SetHash:        hexOrNil(st.SetHash),
			ColumnCount:    st.ColumnCount,
			LastAttemptAt:  st.LastAttemptAt,
			LastSuccessAt:  st.LastSuccessAt,
			LastChangedAt:  st.LastChangedAt,
			LastError:      st.LastError,
			SourceInstance: st.SourceInstance,
		}
	}

	return serverhttp.GetPiiCatalogStatus200JSONResponse{
		Schedule: s.pii.Schedule(),
		Items:    items,
	}, nil
}

func (s *Handlers) GetPiiCatalogColumns(
	ctx context.Context, req serverhttp.GetPiiCatalogColumnsRequestObject,
) (serverhttp.GetPiiCatalogColumnsResponseObject, error) {
	if s.pii == nil {
		return nil, errPIIStorageMissing
	}

	k := piicatalog.Key{Cluster: req.Params.ClusterName, Database: req.Params.Database}

	cat, err := s.pii.Catalog(ctx, k)

	switch {
	case errors.Is(err, piicatalog.ErrInvalidKey):
		return serverhttp.GetPiiCatalogColumns400JSONResponse(
			piiBadRequest("cluster_name and database must be valid UTF-8 without NUL")), nil
	case errors.Is(err, piicatalog.ErrNotCollected):
		return serverhttp.GetPiiCatalogColumns404JSONResponse(piiError(serverhttp.PiiCatalogErrNotCollectedYet,
			"the database has not been collected yet")), nil
	case errors.Is(err, piicatalog.ErrNotFound):
		return serverhttp.GetPiiCatalogColumns404JSONResponse(piiError(serverhttp.PiiCatalogErrNotFound,
			"the database is neither configured nor collected")), nil
	case err != nil:
		return nil, fmt.Errorf("GetPiiCatalogColumns | %w", err)
	}

	items := make([]serverhttp.PiiCatalogColumn, len(cat.Columns))
	for i, p := range cat.Columns {
		items[i] = serverhttp.PiiCatalogColumn{
			SchemaName: p.Schema, TableName: p.Table, ColumnName: p.Name, ColumnComment: p.Comment,
		}
	}

	return serverhttp.GetPiiCatalogColumns200JSONResponse{
		ClusterName: k.Cluster,
		Database:    k.Database,
		SetHash:     hex.EncodeToString(cat.SetHash),
		ChangedAt:   cat.ChangedAt,
		CheckedAt:   cat.CheckedAt,
		Items:       items,
	}, nil
}

func piiError(code serverhttp.PiiCatalogErrorCode, msg string) serverhttp.PiiCatalogError {
	return serverhttp.PiiCatalogError{Code: code, Message: msg} //nolint:exhaustruct
}

func piiBadRequest(msg string) serverhttp.PiiCatalogError {
	return piiError(serverhttp.PiiCatalogErrBadRequest, msg)
}

func hexOrNil(b []byte) *string {
	if b == nil {
		return nil
	}

	h := hex.EncodeToString(b)

	return &h
}

// Errors of the catalog handlers are answered with PiiCatalogError. The cause
// goes only to the log: with e.Debug the default handler would echo it.
// Errors raised before the handler (401, 429, unmatched route) keep the
// generic body.
func writePIIError(err error, c echo.Context, logger *zap.Logger) bool {
	if c.Response().Committed || errors.Is(err, context.Canceled) ||
		!strings.HasPrefix(c.Request().URL.Path, auth.PIICatalogPathPrefix) {
		return false
	}

	var (
		status int
		body   serverhttp.PiiCatalogError
	)

	he, isHTTP := errors.AsType[*echo.HTTPError](err)

	switch dbErr := mapDatabaseError(err); {
	case dbErr != nil:
		msg, _ := dbErr.Message.(string)
		status, body = dbErr.Code, piiError(serverhttp.PiiCatalogErrTimeout, msg)
		logger.Warn("request did not complete",
			zap.Int("status", status),
			zap.String("route", c.Path()),
			zap.Error(err),
		)
	case isHTTP && he.Code == http.StatusBadRequest:
		msg, _ := he.Message.(string)
		status, body = http.StatusBadRequest, piiBadRequest(msg)
	case isHTTP:
		return false
	default:
		status, body = http.StatusInternalServerError, piiError(serverhttp.PiiCatalogErrInternal, "internal error")
		logger.Error("internal server error",
			zap.String("route", c.Path()),
			zap.Error(err),
		)
	}

	if respErr := c.JSON(status, body); respErr != nil {
		logger.Error("failed to write pii catalog error response", zap.Error(respErr))
	}

	return true
}
