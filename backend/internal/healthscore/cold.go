package healthscore

import (
	"context"
	"sync"
	"time"

	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/health"
	"github.com/dbulashev/dasha/internal/hotobjects"
	"github.com/dbulashev/dasha/internal/metrics"
)

const (
	coldCacheTTL    = 5 * time.Minute
	coldReadTimeout = 2 * time.Second
	coldListLimit   = 5
)

type ColdStore interface {
	GetColdTables(ctx context.Context, clusterName, instance string, since time.Time) (hotobjects.ColdSets, error)
}

type coldEntry struct {
	sets    hotobjects.ColdSets
	expires time.Time
}

type coldCache struct {
	store ColdStore
	now   func() time.Time

	mu      sync.Mutex
	entries map[metrics.TargetRef]coldEntry
}

func newColdCache(store ColdStore) *coldCache {
	return &coldCache{store: store, now: time.Now, entries: map[metrics.TargetRef]coldEntry{}}
}

// get never fails: an unavailable store is reported through the status. Errors
// are not cached.
func (c *coldCache) get(ctx context.Context, t metrics.TargetRef) hotobjects.ColdSets {
	if c == nil || c.store == nil {
		return hotobjects.ColdSets{Status: hotobjects.ColdNoStorage} //nolint:exhaustruct
	}

	now := c.now()

	c.mu.Lock()
	e, ok := c.entries[t]
	c.mu.Unlock()

	if ok && now.Before(e.expires) {
		return e.sets
	}

	rctx, cancel := context.WithTimeout(ctx, coldReadTimeout)
	defer cancel()

	sets, err := c.store.GetColdTables(rctx, t.Cluster, t.Instance, now.Add(-hotobjects.ColdMaxAge))
	if err != nil {
		return hotobjects.ColdSets{Status: hotobjects.ColdError} //nolint:exhaustruct
	}

	c.mu.Lock()
	c.entries[t] = coldEntry{sets: sets, expires: now.Add(coldCacheTTL)}
	c.mu.Unlock()

	return sets
}

// ColdArgs turns the available sets into per-database SQL arguments.
func ColdArgs(sets hotobjects.ColdSets) map[string]dto.ColdArgs {
	out := make(map[string]dto.ColdArgs, len(sets.ByDatabase))

	for db, set := range sets.ByDatabase {
		if set.Status != hotobjects.ColdAvailable || len(set.Tables) == 0 {
			continue
		}

		out[db] = ColdArgsOf(set)
	}

	return out
}

// ColdArgsOf is the SQL argument set for one database; Idx in results points into set.Tables.
func ColdArgsOf(set hotobjects.ColdSet) dto.ColdArgs {
	a := dto.ColdArgs{
		Schemas: make([]string, len(set.Tables)),
		Tables:  make([]string, len(set.Tables)),
		Writes:  make([]int64, len(set.Tables)),
	}

	for i, t := range set.Tables {
		a.Schemas[i], a.Tables[i], a.Writes[i] = t.Schema, t.Table, t.Writes
	}

	return a
}

// coldFacts reads the database's cold tables that need a manual VACUUM; nil
// when the marker is unavailable or the read fails.
func (s *Scorer) coldFacts(ctx context.Context, t metrics.TargetRef, database string, sets hotobjects.ColdSets) *health.ColdFacts {
	if !hasColdTables(sets, database) {
		return nil
	}

	set := sets.For(database)

	rows, total, err := s.repo.GetHealthScoreColdTables(ctx, t.Cluster, t.Instance, database, ColdArgsOf(set), coldListLimit)
	if err != nil {
		return nil
	}

	return buildColdFacts(set, sets.WindowDays, rows, total)
}

func buildColdFacts(set hotobjects.ColdSet, windowDays int, rows []dto.HealthScoreColdTable, total int) *health.ColdFacts {
	facts := &health.ColdFacts{WindowDays: windowDays, Tables: make([]health.ColdMaintenanceTable, 0, len(rows)), More: 0}

	for _, r := range rows {
		if r.Idx < 1 || r.Idx > len(set.Tables) {
			continue
		}

		src := set.Tables[r.Idx-1]
		facts.Tables = append(facts.Tables, health.ColdMaintenanceTable{
			Schema:          src.Schema,
			Table:           src.Table,
			SizeBytes:       r.SizeBytes,
			DeadRatio:       r.DeadRatio,
			NeverVacuumed:   r.NeverVacuumed,
			RelfrozenxidAge: r.RelfrozenxidAge,
			NoWritesDays:    int(set.CapturedAt.Sub(src.QuietSince) / (24 * time.Hour)),
		})
	}

	facts.More = max(total-len(facts.Tables), 0)

	return facts
}

// hasColdTables reports whether the scored database has cold tables excluded
// from the snapshot's dead-tuple figures.
func hasColdTables(sets hotobjects.ColdSets, database string) bool {
	set := sets.For(database)

	return set.Status == hotobjects.ColdAvailable && len(set.Tables) > 0
}

// ColdTables returns the instance's cold tables per database.
func (s *Scorer) ColdTables(ctx context.Context, t metrics.TargetRef) hotobjects.ColdSets {
	return s.cold.get(ctx, t)
}
