package indexadvisor

import (
	"slices"
	"testing"
)

func TestCatalogByNameTracksRelations(t *testing.T) {
	cat := NewCatalog()

	cat.AddRelation(Relation{RelKey: RelKey{Schema: "public", Name: "orders"}, Kind: "r", Rows: 10})
	cat.AddRelation(Relation{RelKey: RelKey{Schema: "billing", Name: "orders"}, Kind: "r", Rows: 20})
	cat.AddRelation(Relation{RelKey: RelKey{Schema: "public", Name: "customers"}, Kind: "r"})

	if got := len(cat.ByName["orders"]); got != 2 {
		t.Errorf("orders resolves to %d relations, want 2 — an ambiguous name must stay ambiguous", got)
	}

	if got := len(cat.ByName["customers"]); got != 1 {
		t.Errorf("customers resolves to %d relations, want 1", got)
	}
}

// A relation read twice — the same row seen again, or a re-read of the catalog —
// must not make an unambiguous name look ambiguous, which would silently drop
// every candidate on that table.
func TestCatalogByNameIgnoresRepeatedRelations(t *testing.T) {
	cat := NewCatalog()
	key := RelKey{Schema: "public", Name: "orders"}

	cat.AddRelation(Relation{RelKey: key, Kind: "r", Rows: 10})
	cat.AddRelation(Relation{RelKey: key, Kind: "r", Rows: 4000})

	if got := len(cat.ByName["orders"]); got != 1 {
		t.Errorf("ByName holds %d entries for one relation, want 1", got)
	}

	if got := cat.Relations[key].Rows; got != 4000 {
		t.Errorf("Rows = %d, want the later value 4000", got)
	}
}

// A row cap can stop inside a relation, and what is left of it is not a smaller
// truth but a wrong one: half an index list is what makes a candidate that
// duplicates an existing index look new.
func TestCatalogForgetDropsEveryTraceOfARelation(t *testing.T) {
	cat := NewCatalog()
	cut := RelKey{Schema: "public", Name: "orders"}
	kept := RelKey{Schema: "billing", Name: "orders"}

	for _, key := range []RelKey{cut, kept} {
		cat.AddRelation(Relation{RelKey: key, Kind: "r", Rows: 10})
		cat.AddColumn(key, Column{Name: "id", StatsKnown: true})
		cat.AddIndex(key, Index{Name: key.Name + "_pkey", Columns: []string{"id"}})
		cat.AddWrites(key, Writes{Inserted: 1})
	}

	cat.Forget(cut)

	if _, ok := cat.Relations[cut]; ok {
		t.Error("a relation read in part must not stay in the catalog")
	}

	if len(cat.Columns[cut]) != 0 || len(cat.Indexes[cut]) != 0 {
		t.Error("the columns and indexes of a forgotten relation must go with it")
	}

	if _, ok := cat.Writes[cut]; ok {
		t.Error("the write counters of a forgotten relation must go with it")
	}

	// The name is shared, so forgetting one schema's table must leave the other
	// resolvable rather than take the name down with it.
	if got := cat.ByName["orders"]; !slices.Equal(got, []RelKey{kept}) {
		t.Errorf("ByName[orders] = %v, want only %v", got, kept)
	}

	if _, ok := cat.Relations[kept]; !ok {
		t.Error("forgetting one relation must not touch another")
	}
}

// The last relation of a name goes with it: a name mapping to nothing would make
// every lookup answer "known, but empty".
func TestCatalogForgetDropsTheNameWithItsLastRelation(t *testing.T) {
	cat := NewCatalog()
	key := RelKey{Schema: "public", Name: "orders"}

	cat.AddRelation(Relation{RelKey: key, Kind: "r"})
	cat.Forget(key)

	if _, ok := cat.ByName["orders"]; ok {
		t.Error("a name with no relations left must be gone from ByName")
	}
}

func TestRelationIsPartition(t *testing.T) {
	part := Relation{
		RelKey: RelKey{Schema: "public", Name: "events_01"},
		Kind:   "r",
		Root:   RelKey{Schema: "public", Name: "events"},
	}

	if !part.IsPartition() {
		t.Error("a relation with a root is a partition")
	}

	plain := Relation{RelKey: RelKey{Schema: "public", Name: "orders"}, Kind: "r"}
	if plain.IsPartition() {
		t.Error("a relation without a root is not a partition")
	}
}

func TestWorkloadCountsNotParsedByReason(t *testing.T) {
	var w Workload

	w.CountNotParsed("truncated")
	w.CountNotParsed("truncated")
	w.CountNotParsed("parse_error")

	if got := w.NotParsed["truncated"]; got != 2 {
		t.Errorf("truncated = %d, want 2", got)
	}

	if got := w.NotParsed["parse_error"]; got != 1 {
		t.Errorf("parse_error = %d, want 1", got)
	}
}

// pg_stat_user_tables is per-instance and is not replicated, so the read side of a
// table only adds up once every host has been counted. A table read entirely on a
// replica shows no index scans at all on the primary.
func TestCatalogAddWritesSumsEveryHost(t *testing.T) {
	cat := NewCatalog()
	orders := RelKey{Schema: "public", Name: "orders"}

	cat.AddWrites(orders, Writes{Inserted: 100, Updated: 10, SeqScans: 5, IdxScans: 1000})
	cat.AddWrites(orders, Writes{SeqScans: 2, IdxScans: 7000})

	got := cat.Writes[orders]

	if got.IdxScans != 8000 {
		t.Errorf("IdxScans = %d, want 8000 — reads on every host count", got.IdxScans)
	}

	if got.SeqScans != 7 {
		t.Errorf("SeqScans = %d, want 7", got.SeqScans)
	}

	if got.Inserted != 100 {
		t.Errorf("Inserted = %d, want 100 — the replica writes nothing", got.Inserted)
	}
}

func TestWorkloadMergeCombinesHosts(t *testing.T) {
	var w Workload

	w.Merge(Workload{
		Entries:   []WorkloadEntry{{QueryIDs: []int64{1}, Hosts: []string{"primary"}}},
		NotParsed: map[string]int{"truncated": 2},
		Collected: 10,
		Available: true,
		Hosts:     []string{"primary"},
	})
	w.Merge(Workload{
		Entries:   []WorkloadEntry{{QueryIDs: []int64{1}, Hosts: []string{"replica-1"}}},
		NotParsed: map[string]int{"truncated": 1, "parse_error": 3},
		Collected: 4,
		Available: true,
		Hosts:     []string{"replica-1"},
	})
	// A host without the extension is up and simply invisible here.
	w.Merge(Workload{NoStats: []string{"replica-2"}})

	if len(w.Entries) != 2 {
		t.Errorf("Entries = %d, want 2 — merging keeps them apart, collapse folds them", len(w.Entries))
	}

	if w.Collected != 14 {
		t.Errorf("Collected = %d, want 14", w.Collected)
	}

	if w.NotParsed["truncated"] != 3 || w.NotParsed["parse_error"] != 3 {
		t.Errorf("NotParsed = %v, want the tallies of both hosts added", w.NotParsed)
	}

	if !slices.Equal(w.Hosts, []string{"primary", "replica-1"}) {
		t.Errorf("Hosts = %v, want both hosts that answered", w.Hosts)
	}

	if !slices.Equal(w.NoStats, []string{"replica-2"}) {
		t.Errorf("NoStats = %v, want the host without pg_stat_statements", w.NoStats)
	}

	if !w.Available {
		t.Error("Available must be an OR: one host with the extension makes the report possible")
	}
}
