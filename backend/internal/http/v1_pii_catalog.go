package http

import (
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/piicatalog"
)

// Access control and the audit log live in auth.NewPIICatalogGate; these
// handlers are reached only by an allowed, already logged caller.
type piiCatalogStore interface {
	ListPIIStatus(ctx context.Context) (map[piicatalog.Key]piicatalog.Status, error)
	PIICatalog(ctx context.Context, k piicatalog.Key) (piicatalog.Catalog, error)
}

var errPIIStorageMissing = errors.New("pii catalog: snapshot storage is not configured")

func (s *Handlers) piiConfigured(ctx context.Context) (map[piicatalog.Key]struct{}, error) {
	out := map[piicatalog.Key]struct{}{}

	if s.clusters == nil {
		return out, nil
	}

	cls, err := s.clusters.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("clusters: %w", err)
	}

	for _, cl := range cls {
		for _, db := range cl.Databases {
			out[piicatalog.Key{Cluster: string(cl.Name), Database: string(db)}] = struct{}{}
		}
	}

	return out, nil
}

func (s *Handlers) GetPiiCatalogStatus(
	ctx context.Context, req serverhttp.GetPiiCatalogStatusRequestObject,
) (serverhttp.GetPiiCatalogStatusResponseObject, error) {
	if s.pii == nil {
		return nil, errPIIStorageMissing
	}

	statuses, err := s.pii.ListPIIStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetPiiCatalogStatus | %w", err)
	}

	configured, err := s.piiConfigured(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetPiiCatalogStatus | %w", err)
	}

	keys := make(map[piicatalog.Key]struct{}, len(statuses)+len(configured))
	for k := range statuses {
		keys[k] = struct{}{}
	}

	for k := range configured {
		keys[k] = struct{}{}
	}

	items := make([]serverhttp.PiiCatalogDatabaseStatus, 0, len(keys))

	for k := range keys {
		if req.Params.ClusterName != nil && *req.Params.ClusterName != k.Cluster {
			continue
		}

		_, inConfig := configured[k]
		st := statuses[k]

		items = append(items, serverhttp.PiiCatalogDatabaseStatus{
			ClusterName:    k.Cluster,
			Database:       k.Database,
			InConfig:       inConfig,
			SetHash:        hexOrNil(st.SetHash),
			PairCount:      st.PairCount,
			LastAttemptAt:  st.LastAttemptAt,
			LastSuccessAt:  st.LastSuccessAt,
			LastChangedAt:  st.LastChangedAt,
			LastError:      st.LastError,
			SourceInstance: st.SourceInstance,
		})
	}

	slices.SortFunc(items, func(a, b serverhttp.PiiCatalogDatabaseStatus) int {
		return cmp.Or(cmp.Compare(a.ClusterName, b.ClusterName), cmp.Compare(a.Database, b.Database))
	})

	return serverhttp.GetPiiCatalogStatus200JSONResponse{
		Schedule: s.cfg.PIICatalog.Schedule,
		Items:    items,
	}, nil
}

func (s *Handlers) GetPiiCatalogPairs(
	ctx context.Context, req serverhttp.GetPiiCatalogPairsRequestObject,
) (serverhttp.GetPiiCatalogPairsResponseObject, error) {
	if s.pii == nil {
		return nil, errPIIStorageMissing
	}

	k, err := piiKey(req.Params.ClusterName, req.Params.Database)
	if err != nil {
		return serverhttp.GetPiiCatalogPairs400JSONResponse(piiBadRequest(err.Error())), nil
	}

	cat, err := s.pii.PIICatalog(ctx, k)

	switch {
	case errors.Is(err, piicatalog.ErrNotFound), errors.Is(err, piicatalog.ErrNotCollected):
		e, nfErr := s.piiNotFound(ctx, k)
		if nfErr != nil {
			return nil, fmt.Errorf("GetPiiCatalogPairs | %w", nfErr)
		}

		return serverhttp.GetPiiCatalogPairs404JSONResponse(e), nil
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

// not_collected_yet while the database is configured, otherwise not_found: a
// removed database keeps its failure-only status row until retention.
func (s *Handlers) piiNotFound(ctx context.Context, k piicatalog.Key) (serverhttp.PiiCatalogError, error) {
	configured, cfgErr := s.piiConfigured(ctx)
	if cfgErr != nil {
		return serverhttp.PiiCatalogError{}, cfgErr
	}

	if _, ok := configured[k]; ok {
		return piiError(serverhttp.PiiCatalogErrNotCollectedYet, "the database has not been collected yet"), nil
	}

	return piiError(serverhttp.PiiCatalogErrNotFound, "the database is neither configured nor collected"), nil
}

// Postgres text rejects NUL and invalid UTF-8; without this check such input
// surfaces as a 500 from the storage query.
func piiKey(cluster, database string) (piicatalog.Key, error) {
	for _, v := range []string{cluster, database} {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return piicatalog.Key{}, errors.New("cluster_name and database must be valid UTF-8 without NUL")
		}
	}

	return piicatalog.Key{Cluster: cluster, Database: database}, nil
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
