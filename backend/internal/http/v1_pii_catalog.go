package http

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
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
			PairCount:      st.PairCount,
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

func (s *Handlers) GetPiiCatalogPairs(
	ctx context.Context, req serverhttp.GetPiiCatalogPairsRequestObject,
) (serverhttp.GetPiiCatalogPairsResponseObject, error) {
	if s.pii == nil {
		return nil, errPIIStorageMissing
	}

	k := piicatalog.Key{Cluster: req.Params.ClusterName, Database: req.Params.Database}

	cat, err := s.pii.Catalog(ctx, k)

	switch {
	case errors.Is(err, piicatalog.ErrInvalidKey):
		return serverhttp.GetPiiCatalogPairs400JSONResponse(
			piiBadRequest("cluster_name and database must be valid UTF-8 without NUL")), nil
	case errors.Is(err, piicatalog.ErrNotCollected):
		return serverhttp.GetPiiCatalogPairs404JSONResponse(piiError(serverhttp.PiiCatalogErrNotCollectedYet,
			"the database has not been collected yet")), nil
	case errors.Is(err, piicatalog.ErrNotFound):
		return serverhttp.GetPiiCatalogPairs404JSONResponse(piiError(serverhttp.PiiCatalogErrNotFound,
			"the database is neither configured nor collected")), nil
	case err != nil:
		return nil, fmt.Errorf("GetPiiCatalogPairs | %w", err)
	}

	items := make([]serverhttp.PiiCatalogPair, len(cat.Pairs))
	for i, p := range cat.Pairs {
		items[i] = serverhttp.PiiCatalogPair{ColumnName: p.Name, ColumnComment: p.Comment}
	}

	return serverhttp.GetPiiCatalogPairs200JSONResponse{
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
