package schemalint

import (
	"testing"
)

// seqRow builds a sequence whose raw values agree with the headroom it claims.
// They must not disagree: an int4 owner makes 2147483647 the ceiling and the
// headroom is recomputed from last_value against it (see effectiveHeadroom),
// so a bigint maxvalue there would silently discard freePct.
func seqRow(schema, name string, freePct float64, ownedType string) SequenceRow {
	const minValue = 1

	// Just below math.MaxInt64, so the derived last_value cannot overflow.
	maxValue := int64(9_000_000_000_000_000_000)
	if isInt4(ownedType) {
		maxValue = int4Max
	}

	return SequenceRow{
		Schema:          schema,
		Object:          name,
		LastValue:       maxValue - int64(float64(maxValue-minValue)*freePct/100),
		LastValueKnown:  true,
		MaxValue:        maxValue,
		MinValue:        minValue,
		FreePct:         freePct,
		OwnedColumnType: ownedType,
	}
}

func findingsByCode(r Report, code string) []Finding {
	var out []Finding

	for _, f := range r.Findings {
		if f.Code == code {
			out = append(out, f)
		}
	}

	return out
}

func TestSequenceLevels_Thresholds(t *testing.T) {
	tests := []struct {
		freePct float64
		want    Level
		found   bool
	}{
		{freePct: 0, want: LevelError, found: true},
		{freePct: 4.9, want: LevelError, found: true},
		{freePct: 5, want: LevelWarning, found: true},
		{freePct: 9.9, want: LevelWarning, found: true},
		{freePct: 10, want: LevelNotice, found: true},
		{freePct: 19.9, want: LevelNotice, found: true},
		{freePct: 20, found: false},
		{freePct: 100, found: false},
	}

	for _, tt := range tests {
		in := Inputs{Sequences: []SequenceRow{seqRow("public", "s", tt.freePct, "bigint")}}

		got := findingsByCode(BuildReport(in, Config{}), CodeSequenceExhaustion)
		if !tt.found {
			if len(got) != 0 {
				t.Errorf("free %.1f%%: expected no finding, got %v", tt.freePct, got)
			}

			continue
		}

		if len(got) != 1 {
			t.Fatalf("free %.1f%%: expected 1 finding, got %d", tt.freePct, len(got))
		}

		if got[0].Level != tt.want {
			t.Errorf("free %.1f%%: level = %s, want %s", tt.freePct, got[0].Level, tt.want)
		}

		if got[0].Params.UsedPct == nil || *got[0].Params.UsedPct != 100-tt.freePct {
			t.Errorf("free %.1f%%: used_pct not reported as the complement", tt.freePct)
		}
	}
}

func TestSequenceLevels_ConfiguredThresholdsOverrideDefaults(t *testing.T) {
	cfg := Config{SequenceThresholds: map[string]float64{"error": 50, "warning": 60, "notice": 70}}
	in := Inputs{Sequences: []SequenceRow{seqRow("public", "s", 55, "bigint")}}

	got := findingsByCode(BuildReport(in, cfg), CodeSequenceExhaustion)
	if len(got) != 1 || got[0].Level != LevelWarning {
		t.Fatalf("expected one warning at 55%% free with custom thresholds, got %+v", got)
	}
}

func TestSequenceLevels_Int4OwnerRaisesOneStep(t *testing.T) {
	// Same headroom, different owner column type.
	bigint := findingsByCode(BuildReport(
		Inputs{Sequences: []SequenceRow{seqRow("public", "s", 15, "bigint")}}, Config{}), CodeSequenceExhaustion)
	int4 := findingsByCode(BuildReport(
		Inputs{Sequences: []SequenceRow{seqRow("public", "s", 15, "integer")}}, Config{}), CodeSequenceExhaustion)

	if len(bigint) != 1 || bigint[0].Level != LevelNotice {
		t.Fatalf("bigint owner at 15%% free: want notice, got %+v", bigint)
	}

	if len(int4) != 1 || int4[0].Level != LevelWarning {
		t.Fatalf("int4 owner at 15%% free: want warning, got %+v", int4)
	}
}

func TestSequenceLevels_Int4OwnerCapsAtInt4Max(t *testing.T) {
	// A bigint sequence owned by an int4 column: its own maxvalue leaves it
	// looking untouched, but the column breaks at 2147483647.
	row := SequenceRow{
		Schema:          "public",
		Object:          "s",
		LastValue:       2_100_000_000,
		LastValueKnown:  true,
		MaxValue:        9223372036854775807,
		MinValue:        1,
		FreePct:         99.9,
		OwnedBy:         "public.t.id",
		OwnedColumnType: "integer",
	}

	got := findingsByCode(BuildReport(Inputs{Sequences: []SequenceRow{row}}, Config{}), CodeSequenceExhaustion)
	if len(got) != 1 {
		t.Fatalf("expected the int4 ceiling to be applied, got %d findings", len(got))
	}

	if got[0].Level != LevelError {
		t.Errorf("level = %s, want error (2.2%% of int4 range left, raised for int4)", got[0].Level)
	}

	if got[0].Params.MaxValue == nil || *got[0].Params.MaxValue != int4Max {
		t.Errorf("max_value should report the ceiling that applies (int4), got %v", got[0].Params.MaxValue)
	}
}

func TestSequences_UnreadableLastValueBecomesSkipNotFinding(t *testing.T) {
	row := seqRow("public", "s", 0, "bigint")
	row.LastValueKnown = false

	rep := BuildReport(Inputs{Sequences: []SequenceRow{row}}, Config{})

	if len(rep.Findings) != 0 {
		t.Errorf("a sequence that could not be read must not produce a finding, got %+v", rep.Findings)
	}

	if len(rep.Skipped) != 1 ||
		rep.Skipped[0].Code != CodeSequenceExhaustion ||
		rep.Skipped[0].Reason != SkipInsufficientPrivileges {
		t.Fatalf("want one insufficient_privileges skip, got %+v", rep.Skipped)
	}

	// The count travels as a number: the sentence is built in the UI, in the
	// user's language.
	if rep.Skipped[0].Count != 1 {
		t.Errorf("skip must carry how many objects were unreadable, got %d", rep.Skipped[0].Count)
	}
}

func TestRelationKeys_SplitsCodesAndKeepsNullableFlag(t *testing.T) {
	in := Inputs{RelationKeys: []RelationKeyRow{
		{Schema: "public", Object: "with_unique", HasUnique: true, UniqueNullable: true},
		// No unique index at all: the query reports unique_nullable = true here
		// too, since none of them is usable — there are none.
		{Schema: "public", Object: "no_key", UniqueNullable: true},
	}}

	rep := BuildReport(in, Config{})

	pk := findingsByCode(rep, CodeNoPrimaryKey)
	if len(pk) != 1 || pk[0].Object != "with_unique" {
		t.Fatalf("a table with a unique index but no PK → no_primary_key, got %+v", pk)
	}

	if !pk[0].Params.UniqueNullable {
		t.Error("unique_nullable must reach params: a nullable unique index is no replica identity")
	}

	uk := findingsByCode(rep, CodeNoUniqueKey)
	if len(uk) != 1 || uk[0].Object != "no_key" {
		t.Fatalf("a table without any unique key → no_unique_key, got %+v", uk)
	}

	// The query reports "no usable unique index" as unique_nullable for such a
	// table too; the flag only means something when one exists.
	if uk[0].Params.UniqueNullable {
		t.Error("a table with no unique index at all must not carry the nullable-index caveat")
	}

	for _, f := range rep.Findings {
		if f.Level != LevelError {
			t.Errorf("%s: level = %s, want error", f.Code, f.Level)
		}
	}
}

func TestPublicCreatePrivilege_LevelDependsOnServerVersion(t *testing.T) {
	rows := []SchemaPrivilegeRow{{Schema: "public", Owner: "postgres"}}

	pg14 := BuildReport(Inputs{ServerVersionNum: 140000, SchemaPrivileges: rows}, Config{})
	if len(pg14.Findings) != 1 || pg14.Findings[0].Level != LevelWarning {
		t.Fatalf("PG14: want one warning (open public schema is the factory default), got %+v", pg14.Findings)
	}

	pg15 := BuildReport(Inputs{ServerVersionNum: 150000, SchemaPrivileges: rows}, Config{})
	if len(pg15.Findings) != 1 || pg15.Findings[0].Level != LevelError {
		t.Fatalf("PG15+: want one error, got %+v", pg15.Findings)
	}

	if pg15.Findings[0].Params.Owner != "postgres" {
		t.Errorf("owner must reach params, got %q", pg15.Findings[0].Params.Owner)
	}
}

func TestUnlogged_CodePickedByRelkind(t *testing.T) {
	in := Inputs{Unlogged: []UnloggedRow{
		{Schema: "public", Object: "t", RelKind: "r"},
		{Schema: "public", Object: "s", RelKind: "S"},
	}}

	rep := BuildReport(in, Config{})

	if got := findingsByCode(rep, CodeUnloggedRelation); len(got) != 1 || got[0].Object != "t" {
		t.Errorf("relkind r → unlogged_relation, got %+v", got)
	}

	if got := findingsByCode(rep, CodeUnloggedSequence); len(got) != 1 || got[0].Object != "s" {
		t.Errorf("relkind S → unlogged_sequence, got %+v", got)
	}
}

func TestCollapsePartitions_RollsChildrenUpToRoot(t *testing.T) {
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		RelationKeys: []RelationKeyRow{
			{Schema: "public", Object: "events"},
			{Schema: "public", Object: "events_01"},
			{Schema: "public", Object: "events_02"},
			{Schema: "public", Object: "events_03"},
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_01"}: root,
			{Schema: "public", Object: "events_02"}: root,
			{Schema: "public", Object: "events_03"}: root,
		},
	}

	got := findingsByCode(BuildReport(in, Config{}), CodeNoUniqueKey)
	if len(got) != 1 {
		t.Fatalf("one defect on a partitioned table → one row, got %d", len(got))
	}

	if got[0].Object != "events" {
		t.Errorf("finding must sit on the root table, got %q", got[0].Object)
	}

	if got[0].Params.Partitions != 3 {
		t.Errorf("partitions = %d, want 3", got[0].Params.Partitions)
	}
}

func TestCollapsePartitions_MultiLevelFoldsToRootNotParent(t *testing.T) {
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		Unlogged: []UnloggedRow{
			{Schema: "public", Object: "events_2026_01_a", RelKind: "r"},
			{Schema: "public", Object: "events_2026_01_b", RelKind: "r"},
		},
		// Both are sub-partitions of an intermediate level; the map already
		// resolves them to the root of the tree.
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_2026_01"}:   root,
			{Schema: "public", Object: "events_2026_01_a"}: root,
			{Schema: "public", Object: "events_2026_01_b"}: root,
		},
	}

	got := findingsByCode(BuildReport(in, Config{}), CodeUnloggedRelation)
	if len(got) != 1 || got[0].Object != "events" || got[0].Params.Partitions != 2 {
		t.Fatalf("sub-partitions must fold to the root, got %+v", got)
	}
}

func TestCollapsePartitions_WorstChildWins(t *testing.T) {
	// Sequences do not collapse, so use a check that does: two partitions of the
	// same table, one with a nullable unique index and one without.
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		RelationKeys: []RelationKeyRow{
			{Schema: "public", Object: "events_01", HasUnique: true},
			{Schema: "public", Object: "events_02", HasUnique: true, UniqueNullable: true},
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_01"}: root,
			{Schema: "public", Object: "events_02"}: root,
		},
	}

	got := findingsByCode(BuildReport(in, Config{}), CodeNoPrimaryKey)
	if len(got) != 1 {
		t.Fatalf("expected one rolled-up finding, got %d", len(got))
	}

	if !got[0].Params.UniqueNullable {
		t.Error("a nullable unique index on any partition must survive the rollup")
	}
}

func TestCollapsePartitions_RootFindingMergesWithItsChildren(t *testing.T) {
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		Unlogged: []UnloggedRow{
			{Schema: "public", Object: "events", RelKind: "p"},
			{Schema: "public", Object: "events_01", RelKind: "r"},
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_01"}: root,
		},
	}

	got := findingsByCode(BuildReport(in, Config{}), CodeUnloggedRelation)
	if len(got) != 1 {
		t.Fatalf("root and its partition are one problem, got %d rows", len(got))
	}

	if got[0].Params.Partitions != 1 {
		t.Errorf("partitions = %d, want 1 (the root itself is not counted)", got[0].Params.Partitions)
	}
}

func TestCollapsePartitions_SequencesAreNotCollapsed(t *testing.T) {
	in := Inputs{
		Sequences: []SequenceRow{
			seqRow("public", "a_seq", 1, "bigint"),
			seqRow("public", "b_seq", 1, "bigint"),
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "a_seq"}: {Schema: "public", Object: "b_seq"},
		},
	}

	if got := findingsByCode(BuildReport(in, Config{}), CodeSequenceExhaustion); len(got) != 2 {
		t.Fatalf("sequences are addressed individually, got %d findings", len(got))
	}
}

func TestUUIDLikeColumns_AddressesTheColumn(t *testing.T) {
	in := Inputs{UUIDLikeColumns: []ColumnRow{
		{Schema: "public", Object: "users", Column: "external_id", ColumnType: "character varying(36)"},
	}}

	got := findingsByCode(BuildReport(in, Config{}), CodeUUIDInNonUUIDType)
	if len(got) != 1 {
		t.Fatalf("expected one finding, got %d", len(got))
	}

	if got[0].Level != LevelNotice {
		t.Errorf("a heuristic check must stay a notice, got %s", got[0].Level)
	}

	if got[0].Object != "users" || got[0].Params.Column != "external_id" {
		t.Errorf("the relation goes in object and the column in params, got %+v", got[0])
	}

	if got[0].ObjectType != ObjectTypeAttribute {
		t.Errorf("object type = %s, want attribute", got[0].ObjectType)
	}
}

func TestUUIDLikeColumns_TwoColumnsOfOneTableStayApart(t *testing.T) {
	// Collapsing by (code, table) alone would report one column and hide the other.
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		UUIDLikeColumns: []ColumnRow{
			{Schema: "public", Object: "events_01", Column: "uid", ColumnType: "character varying(36)"},
			{Schema: "public", Object: "events_01", Column: "owner_id", ColumnType: "character varying(36)"},
			{Schema: "public", Object: "events_02", Column: "uid", ColumnType: "character varying(36)"},
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_01"}: root,
			{Schema: "public", Object: "events_02"}: root,
		},
	}

	got := findingsByCode(BuildReport(in, Config{}), CodeUUIDInNonUUIDType)
	if len(got) != 2 {
		t.Fatalf("two distinct columns → two findings, got %d: %+v", len(got), got)
	}

	columns := map[string]int{}
	for _, f := range got {
		if f.Object != "events" {
			t.Errorf("partition finding must sit on the root, got %s", f.Object)
		}

		columns[f.Params.Column] = f.Params.Partitions
	}

	if columns["uid"] != 2 {
		t.Errorf("uid appears on 2 partitions, got partitions = %d", columns["uid"])
	}

	if _, ok := columns["owner_id"]; !ok {
		t.Error("the second column of the same table must not be swallowed")
	}
}

func TestRelationWithoutFk_IsOffUntilEnabled(t *testing.T) {
	in := Inputs{WithoutFk: []RelationRow{{Schema: "public", Object: "orphan"}}}

	if got := findingsByCode(BuildReport(in, Config{}), CodeRelationWithoutFk); len(got) != 0 {
		t.Fatalf("the check is noisy and must stay off by default, got %+v", got)
	}

	cfg := Config{EnabledChecks: []string{CodeRelationWithoutFk}}

	got := findingsByCode(BuildReport(in, cfg), CodeRelationWithoutFk)
	if len(got) != 1 || got[0].Level != LevelNotice {
		t.Fatalf("opting in must produce one notice, got %+v", got)
	}
}

func TestRelationWithoutColumns_IsReported(t *testing.T) {
	in := Inputs{WithoutColumns: []RelationRow{{Schema: "public", Object: "empty"}}}

	got := findingsByCode(BuildReport(in, Config{}), CodeRelationWithoutColumns)
	if len(got) != 1 || got[0].Level != LevelNotice {
		t.Fatalf("expected one notice, got %+v", got)
	}
}

func TestUnsafeNames_KeywordWinsOverQuoting(t *testing.T) {
	in := Inputs{UnsafeNames: []NameRow{
		{Schema: "public", Object: "order", RelKind: "r", Reserved: true},
		{Schema: "public", Object: "my table", RelKind: "r"},
	}}

	rep := BuildReport(in, Config{})

	reserved := findingsByCode(rep, CodeReservedWordInName)
	if len(reserved) != 1 || reserved[0].Object != "order" {
		t.Fatalf("a keyword name → reserved_word_in_name, got %+v", reserved)
	}

	if reserved[0].Level != LevelWarning {
		t.Errorf("a keyword breaks parsing, not just quoting: want warning, got %s", reserved[0].Level)
	}

	unsafe := findingsByCode(rep, CodeUnsafeCharsInName)
	if len(unsafe) != 1 || unsafe[0].Object != "my table" || unsafe[0].Level != LevelNotice {
		t.Fatalf("a name needing quotes → unsafe_chars_in_name (notice), got %+v", unsafe)
	}
}

func TestInvalidConstraints_AddressTheConstraint(t *testing.T) {
	in := Inputs{InvalidConstrs: []ConstraintRow{
		{Schema: "public", Object: "orders", Constraint: "fk_user", ReferencedSchema: "public", ReferencedTable: "users"},
		{Schema: "public", Object: "orders", Constraint: "fk_item", ReferencedSchema: "public", ReferencedTable: "items"},
	}}

	got := findingsByCode(BuildReport(in, Config{}), CodeInvalidConstraint)
	if len(got) != 2 {
		t.Fatalf("two constraints of one table are two findings, got %d", len(got))
	}

	for _, f := range got {
		if f.Level != LevelWarning || f.ObjectType != ObjectTypeConstraint {
			t.Errorf("%s: want a warning on a constraint, got %s/%s", f.Params.Constraint, f.Level, f.ObjectType)
		}

		if f.Params.ReferencedBy == "" {
			t.Errorf("%s: the referenced table is what makes the finding actionable", f.Params.Constraint)
		}
	}
}

func TestCollapsePartitions_KeepsConstraintsApart(t *testing.T) {
	// Same defect on two constraints of two partitions: two findings on the
	// root, not one.
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		InvalidConstrs: []ConstraintRow{
			{Schema: "public", Object: "events_01", Constraint: "fk_a", ReferencedTable: "a"},
			{Schema: "public", Object: "events_02", Constraint: "fk_a", ReferencedTable: "a"},
			{Schema: "public", Object: "events_01", Constraint: "fk_b", ReferencedTable: "b"},
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_01"}: root,
			{Schema: "public", Object: "events_02"}: root,
		},
	}

	got := findingsByCode(BuildReport(in, Config{}), CodeInvalidConstraint)
	if len(got) != 2 {
		t.Fatalf("two distinct constraints → two findings, got %d: %+v", len(got), got)
	}

	for _, f := range got {
		if f.Object != "events" {
			t.Errorf("finding must sit on the root, got %s", f.Object)
		}
	}
}

func TestPairChecks_AddressTheirOwnObject(t *testing.T) {
	in := Inputs{
		FkTypeMismatch: []PairRow{{Schema: "public", Object: "orders", First: "fk_user", Second: "public.users"}},
		FkNullable:     []PairRow{{Schema: "public", Object: "orders", First: "fk_item", Second: "item_id"}},
		BtreeOnArray:   []PairRow{{Schema: "public", Object: "orders", First: "orders_tags_idx"}},
	}

	rep := BuildReport(in, Config{})

	mismatch := findingsByCode(rep, CodeFkTypeMismatch)
	if len(mismatch) != 1 || mismatch[0].Params.Constraint != "fk_user" || mismatch[0].Level != LevelWarning {
		t.Fatalf("fk type mismatch: want one warning naming the constraint, got %+v", mismatch)
	}

	if mismatch[0].Params.OtherObject != "public.users" {
		t.Errorf("the referenced table must reach params, got %q", mismatch[0].Params.OtherObject)
	}

	nullable := findingsByCode(rep, CodeFkNullable)
	if len(nullable) != 1 || nullable[0].Level != LevelNotice {
		t.Fatalf("nullable fk: want one notice, got %+v", nullable)
	}

	btree := findingsByCode(rep, CodeBtreeOnArray)
	if len(btree) != 1 || btree[0].ObjectType != ObjectTypeIndex || btree[0].Params.Index != "orders_tags_idx" {
		t.Fatalf("btree on array: the index belongs in params.Index, got %+v", btree)
	}
}

func TestPairChecks_SimilarityChecksAreOptIn(t *testing.T) {
	in := Inputs{
		FkSimilar:    []PairRow{{Schema: "public", Object: "orders", First: "fk_a", Second: "fk_b"}},
		IndexSimilar: []PairRow{{Schema: "public", Object: "orders", First: "idx_a", Second: "idx_b"}},
	}

	rep := BuildReport(in, Config{})
	if len(rep.Findings) != 0 {
		t.Fatalf("candidate lists for a human to judge must stay off by default, got %+v", rep.Findings)
	}

	cfg := Config{EnabledChecks: []string{CodeFkSimilar, CodeIndexSimilar}}
	if got := BuildReport(in, cfg); len(got.Findings) != 2 {
		t.Fatalf("opting in must surface both, got %d", len(got.Findings))
	}
}

func TestCollapsePartitions_KeepsIndexPairsApart(t *testing.T) {
	root := ObjectRef{Schema: "public", Object: "events"}
	in := Inputs{
		BtreeOnArray: []PairRow{
			{Schema: "public", Object: "events_01", First: "events_01_tags_idx"},
			{Schema: "public", Object: "events_02", First: "events_02_tags_idx"},
		},
		PartitionRoots: map[ObjectRef]ObjectRef{
			{Schema: "public", Object: "events_01"}: root,
			{Schema: "public", Object: "events_02"}: root,
		},
	}

	// Per-partition indexes carry distinct names, so they stay distinct rows on
	// the root rather than collapsing into one and hiding a partition.
	got := findingsByCode(BuildReport(in, Config{}), CodeBtreeOnArray)
	if len(got) != 2 {
		t.Fatalf("two differently named indexes → two findings, got %d: %+v", len(got), got)
	}

	for _, f := range got {
		if f.Object != "events" {
			t.Errorf("finding must sit on the root table, got %s", f.Object)
		}
	}
}

func TestSuppression_DisabledCheckProducesSkipAndNoFindings(t *testing.T) {
	in := Inputs{Unlogged: []UnloggedRow{{Schema: "public", Object: "t", RelKind: "r"}}}
	cfg := Config{DisabledChecks: []string{CodeUnloggedRelation}}

	rep := BuildReport(in, cfg)
	if len(findingsByCode(rep, CodeUnloggedRelation)) != 0 {
		t.Error("a disabled check must produce no findings")
	}

	plans, skips := Plan(cfg, 160000)
	for _, p := range plans {
		for _, code := range p.Codes {
			if code == CodeUnloggedRelation {
				t.Error("a disabled check must not be planned")
			}
		}
	}

	var found bool

	for _, s := range skips {
		if s.Code == CodeUnloggedRelation && s.Reason == SkipDisabled {
			found = true
		}
	}

	if !found {
		t.Error("a disabled check must be reported as skipped, not silently missing")
	}
}

func TestSuppression_IgnoredSchemaMasks(t *testing.T) {
	in := Inputs{Unlogged: []UnloggedRow{
		{Schema: "_timescaledb_internal", Object: "chunk", RelKind: "r"},
		{Schema: "cron", Object: "job_run_details", RelKind: "r"},
		{Schema: "app", Object: "t", RelKind: "r"},
	}}

	rep := BuildReport(in, Config{IgnoreSchemas: []string{"_timescaledb*", "cron"}})

	if len(rep.Findings) != 1 || rep.Findings[0].Schema != "app" {
		t.Fatalf("masked schemas must be dropped, got %+v", rep.Findings)
	}
}

func TestSuppression_IgnoredSchemaAlsoSilencesSequenceSkip(t *testing.T) {
	row := seqRow("cron", "s", 0, "bigint")
	row.LastValueKnown = false

	rep := BuildReport(Inputs{Sequences: []SequenceRow{row}}, Config{IgnoreSchemas: []string{"cron"}})
	if len(rep.Skipped) != 0 {
		t.Errorf("unreadable sequences in ignored schemas must not raise a skip, got %+v", rep.Skipped)
	}
}

func TestSortFindings_DeterministicOrder(t *testing.T) {
	in := Inputs{
		Unlogged: []UnloggedRow{
			{Schema: "b", Object: "t2", RelKind: "r"},
			{Schema: "a", Object: "t1", RelKind: "r"},
		},
		RelationKeys: []RelationKeyRow{
			{Schema: "z", Object: "t3"},
		},
		Sequences: []SequenceRow{seqRow("a", "s", 15, "bigint")},
	}

	rep := BuildReport(in, Config{})

	want := []struct {
		level  Level
		code   string
		schema string
	}{
		{LevelError, CodeNoUniqueKey, "z"},
		{LevelWarning, CodeUnloggedRelation, "a"},
		{LevelWarning, CodeUnloggedRelation, "b"},
		{LevelNotice, CodeSequenceExhaustion, "a"},
	}

	if len(rep.Findings) != len(want) {
		t.Fatalf("got %d findings, want %d", len(rep.Findings), len(want))
	}

	for i, w := range want {
		got := rep.Findings[i]
		if got.Level != w.level || got.Code != w.code || got.Schema != w.schema {
			t.Errorf("position %d: got %s/%s/%s, want %s/%s/%s",
				i, got.Level, got.Code, got.Schema, w.level, w.code, w.schema)
		}
	}
}

func TestNotRun_CountsOnlyChecksThatShouldHaveRun(t *testing.T) {
	rep := BuildReport(Inputs{Skipped: []Skip{
		{Code: CodeNoPrimaryKey, Reason: SkipError},
		{Code: CodeSequenceExhaustion, Reason: SkipInsufficientPrivileges},
		{Code: CodeUUIDInNonUUIDType, Reason: SkipUnsupportedVersion},
		// Switched off on purpose — a decision, not a gap in coverage.
		{Code: CodeRelationWithoutFk, Reason: SkipDisabled},
	}}, Config{})

	if got := rep.NotRun(); got != 3 {
		t.Errorf("NotRun() = %d, want 3 (disabled checks excluded)", got)
	}
}

func TestSummary_CountsEveryLevel(t *testing.T) {
	in := Inputs{
		RelationKeys: []RelationKeyRow{{Schema: "public", Object: "t"}},
		Unlogged:     []UnloggedRow{{Schema: "public", Object: "u", RelKind: "r"}},
		Sequences:    []SequenceRow{seqRow("public", "s", 15, "bigint")},
	}

	rep := BuildReport(in, Config{})

	for level, want := range map[Level]int{LevelError: 1, LevelWarning: 1, LevelNotice: 1} {
		if rep.Summary[level] != want {
			t.Errorf("summary[%s] = %d, want %d", level, rep.Summary[level], want)
		}
	}
}

func TestBuildReport_CarriesRepositorySkipsThrough(t *testing.T) {
	in := Inputs{Skipped: []Skip{{Code: CodeNoPrimaryKey, Reason: SkipError, Detail: "timeout"}}}

	rep := BuildReport(in, Config{})
	if len(rep.Skipped) != 1 || rep.Skipped[0].Reason != SkipError {
		t.Fatalf("repository skips must survive, got %+v", rep.Skipped)
	}
}
