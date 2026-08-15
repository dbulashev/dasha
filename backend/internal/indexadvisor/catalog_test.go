package indexadvisor

import "testing"

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
