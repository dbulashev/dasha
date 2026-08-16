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
