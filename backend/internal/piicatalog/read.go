package piicatalog

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dbulashev/dasha/internal/config"
)

var (
	ErrNotFound     = errors.New("piicatalog: database is neither configured nor collected")
	ErrNotCollected = errors.New("piicatalog: database has not been collected yet")
	ErrInvalidKey   = errors.New("piicatalog: cluster and database must be valid UTF-8 without NUL")

	// ErrNoCatalog is the ReadStore answer for a database without a collected
	// set; Reader turns it into ErrNotFound or ErrNotCollected.
	ErrNoCatalog = errors.New("piicatalog: no collected catalog in storage")
)

// Catalog is the current set of one database, ordered by (schema, table, column).
type Catalog struct {
	SetHash   []byte
	ChangedAt time.Time
	CheckedAt time.Time
	Columns   []Column
}

type DatabaseStatus struct {
	Key      Key
	InConfig bool
	// Zero for a configured database that has no status row yet.
	Status Status
}

type ReadStore interface {
	ListPIIStatus(ctx context.Context) (map[Key]Status, error)
	PIICatalog(ctx context.Context, k Key) (Catalog, error)
}

type Reader struct {
	store    ReadStore
	clusters config.Clusters
	schedule string
}

func NewReader(store ReadStore, clusters config.Clusters, schedule string) *Reader {
	return &Reader{store: store, clusters: clusters, schedule: schedule}
}

func (r *Reader) Schedule() string { return r.schedule }

// Statuses returns the union of configured and stored databases ordered by
// key; an empty cluster means every cluster.
func (r *Reader) Statuses(ctx context.Context, cluster string) ([]DatabaseStatus, error) {
	statuses, err := r.store.ListPIIStatus(ctx)
	if err != nil {
		return nil, err
	}

	configured, err := r.configured(ctx)
	if err != nil {
		return nil, err
	}

	keys := make(map[Key]struct{}, len(statuses)+len(configured))
	for k := range statuses {
		keys[k] = struct{}{}
	}

	for k := range configured {
		keys[k] = struct{}{}
	}

	out := make([]DatabaseStatus, 0, len(keys))

	for k := range keys {
		if cluster != "" && k.Cluster != cluster {
			continue
		}

		_, inConfig := configured[k]
		out = append(out, DatabaseStatus{Key: k, InConfig: inConfig, Status: statuses[k]})
	}

	slices.SortFunc(out, func(a, b DatabaseStatus) int {
		return cmp.Or(cmp.Compare(a.Key.Cluster, b.Key.Cluster), cmp.Compare(a.Key.Database, b.Key.Database))
	})

	return out, nil
}

// Catalog returns ErrNotCollected while the database is configured and
// ErrNotFound otherwise: a removed database keeps its failure-only status row
// until retention.
func (r *Reader) Catalog(ctx context.Context, k Key) (Catalog, error) {
	if !k.valid() {
		return Catalog{}, ErrInvalidKey
	}

	cat, err := r.store.PIICatalog(ctx, k)
	if !errors.Is(err, ErrNoCatalog) {
		return cat, err
	}

	configured, cfgErr := r.configured(ctx)
	if cfgErr != nil {
		return Catalog{}, cfgErr
	}

	if _, ok := configured[k]; ok {
		return Catalog{}, ErrNotCollected
	}

	return Catalog{}, ErrNotFound
}

func (r *Reader) configured(ctx context.Context) (map[Key]struct{}, error) {
	out := map[Key]struct{}{}

	if r.clusters == nil {
		return out, nil
	}

	cls, err := r.clusters.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("clusters: %w", err)
	}

	for _, cl := range cls {
		for _, db := range cl.Databases {
			out[Key{Cluster: string(cl.Name), Database: string(db)}] = struct{}{}
		}
	}

	return out, nil
}

// Postgres text rejects NUL and invalid UTF-8; without this check such input
// surfaces as a storage error.
func (k Key) valid() bool {
	for _, v := range []string{k.Cluster, k.Database} {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return false
		}
	}

	return true
}
