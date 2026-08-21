package indexadvisor

import (
	"strconv"
	"strings"
	"testing"
)

// partRel builds a relation the way the catalog query fills one: Root is the top of
// the tree, Parent the level directly above — the two differ from the second
// level down, and the DDL walks Parent.
func partRel(name, kind string, parent, root RelKey) Relation {
	return Relation{
		RelKey: RelKey{Schema: testSchema, Name: name},
		Kind:   kind,
		Rows:   testRows,
		Pages:  100,
		Root:   root,
		Parent: parent,
	}
}

func relKey(name string) RelKey { return RelKey{Schema: testSchema, Name: name} }

func catalogOf(rels ...Relation) Catalog {
	cat := NewCatalog()
	for _, r := range rels {
		cat.AddRelation(r)
	}

	return cat
}

func TestDDLAttachesEveryPartitionConcurrently(t *testing.T) {
	root := relKey("events")
	cat := catalogOf(
		partRel("events", "p", RelKey{}, RelKey{}),
		partRel("events_01", "r", root, root),
		partRel("events_02", "r", root, root),
	)

	ddl := ddlFor(root, []string{"tenant_id"}, nil, newDDLScope(cat))

	// The root index is created invalid and turns valid with the last ATTACH:
	// PostgreSQL rejects CREATE INDEX CONCURRENTLY on a partitioned table, and a
	// suggestion that cannot run is worse advice than a slow one.
	want := []string{
		`CREATE INDEX "events_tenant_id_idx" ON ONLY "public"."events" ("tenant_id");`,
		`CREATE INDEX CONCURRENTLY "events_01_tenant_id_idx" ON "public"."events_01" ("tenant_id");`,
		`ALTER INDEX "public"."events_tenant_id_idx" ATTACH PARTITION "public"."events_01_tenant_id_idx";`,
		`CREATE INDEX CONCURRENTLY "events_02_tenant_id_idx" ON "public"."events_02" ("tenant_id");`,
		`ALTER INDEX "public"."events_tenant_id_idx" ATTACH PARTITION "public"."events_02_tenant_id_idx";`,
	}

	for _, stmt := range want {
		if !strings.Contains(ddl, stmt) {
			t.Errorf("DDL missing %q:\n%s", stmt, ddl)
		}
	}
}

// An index may only be attached to the index of the table its own table is a
// direct partition of, so a tree deeper than one level needs a level of its own.
func TestDDLAttachesLevelByLevel(t *testing.T) {
	root := relKey("events")
	mid := relKey("events_2026")
	cat := catalogOf(
		partRel("events", "p", RelKey{}, RelKey{}),
		partRel("events_2026", "p", root, root),
		partRel("events_2026_01", "r", mid, root),
	)

	ddl := ddlFor(root, []string{"tenant_id"}, nil, newDDLScope(cat))

	want := []string{
		`CREATE INDEX "events_2026_tenant_id_idx" ON ONLY "public"."events_2026" ("tenant_id");`,
		`ALTER INDEX "public"."events_2026_tenant_id_idx" ATTACH PARTITION "public"."events_2026_01_tenant_id_idx";`,
		`ALTER INDEX "public"."events_tenant_id_idx" ATTACH PARTITION "public"."events_2026_tenant_id_idx";`,
	}

	for _, stmt := range want {
		if !strings.Contains(ddl, stmt) {
			t.Errorf("DDL missing %q:\n%s", stmt, ddl)
		}
	}

	// Only the level that holds data can be built concurrently.
	if got := strings.Count(ddl, "CONCURRENTLY"); got != 1 {
		t.Errorf("CONCURRENTLY appears %d times, want 1 (the single leaf):\n%s", got, ddl)
	}
}

func TestDDLStopsListingPastTheCap(t *testing.T) {
	root := relKey("events")
	rels := []Relation{partRel("events", "p", RelKey{}, RelKey{})}

	const partitions = ddlMaxPartitions + 5
	for i := range partitions {
		rels = append(rels, partRel("events_"+strconv.Itoa(i), "r", root, root))
	}

	ddl := ddlFor(root, []string{"tenant_id"}, nil, newDDLScope(catalogOf(rels...)))

	if got := strings.Count(ddl, "CREATE INDEX CONCURRENTLY"); got != ddlMaxPartitions {
		t.Errorf("%d concurrent builds listed, want the cap of %d", got, ddlMaxPartitions)
	}

	// The rest is not silently dropped: an unattached partition leaves the root
	// index invalid, and the script has to say so.
	if !strings.Contains(ddl, "-- 5 more partitions are not listed") {
		t.Errorf("DDL does not account for the partitions it left out:\n%s", ddl)
	}
}

func TestDDLNamesAnIndexNothingElseAnswersTo(t *testing.T) {
	root := relKey("events")
	cat := catalogOf(
		partRel("events", "p", RelKey{}, RelKey{}),
		partRel("events_01", "r", root, root),
	)

	cat.AddIndex(root, Index{
		Name: "events_tenant_id_idx", Method: "btree", Unique: false, Primary: false,
		Valid: true, Partial: true, Expression: false, Columns: []string{"tenant_id"},
	})

	ddl := ddlFor(root, []string{"tenant_id"}, nil, newDDLScope(cat))

	if !strings.Contains(ddl, `CREATE INDEX "events_tenant_id_idx1" ON ONLY`) {
		t.Errorf("DDL reuses a name the schema already holds:\n%s", ddl)
	}
}

// A partitioned table with no partitions holds no rows, so the plain form locks
// nothing and leaves no index invalid.
func TestDDLOfAnEmptyPartitionTree(t *testing.T) {
	root := relKey("events")
	ddl := ddlFor(root, []string{"tenant_id"}, nil, newDDLScope(catalogOf(partRel("events", "p", RelKey{}, RelKey{}))))

	if ddl != `CREATE INDEX ON "public"."events" ("tenant_id");` {
		t.Errorf("DDL = %q, want the plain form", ddl)
	}
}

func TestReserveKeepsNamesInsideNamedatalen(t *testing.T) {
	long := strings.Repeat("e", 60)
	s := newDDLScope(catalogOf(partRel(long, "p", RelKey{}, RelKey{})))

	first := s.reserve(relKey(long), []string{"tenant_id"})
	second := s.reserve(relKey(long), []string{"tenant_id"})

	if len(first.Name) > maxIdentLen || len(second.Name) > maxIdentLen {
		t.Errorf("names %q and %q, want at most %d bytes", first.Name, second.Name, maxIdentLen)
	}

	// A name PostgreSQL would truncate to one the schema already holds makes the
	// ATTACH point at the wrong index.
	if first.Name == second.Name {
		t.Errorf("both reservations returned %q", first.Name)
	}
}

// Every statement of the script carries the predicate: ATTACH needs it to match.
func TestDDLCarriesThePredicate(t *testing.T) {
	plain := ddlFor(relKey("orders"), []string{"tenant_id"}, []string{"processed_at"},
		newDDLScope(catalogOf(partRel("orders", "r", RelKey{}, RelKey{}))))

	want := `CREATE INDEX CONCURRENTLY ON "public"."orders" ("tenant_id") WHERE "processed_at" IS NULL;`
	if plain != want {
		t.Errorf("DDL = %q, want %q", plain, want)
	}

	root := relKey("events")
	cat := catalogOf(
		partRel("events", "p", RelKey{}, RelKey{}),
		partRel("events_01", "r", root, root),
	)

	script := ddlFor(root, []string{"tenant_id"}, []string{"closed_at", "processed_at"}, newDDLScope(cat))

	for _, stmt := range []string{
		`ON ONLY "public"."events" ("tenant_id") WHERE "closed_at" IS NULL AND "processed_at" IS NULL;`,
		`ON "public"."events_01" ("tenant_id") WHERE "closed_at" IS NULL AND "processed_at" IS NULL;`,
	} {
		if !strings.Contains(script, stmt) {
			t.Errorf("DDL missing %q:\n%s", stmt, script)
		}
	}
}
