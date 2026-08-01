package schemalint

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

// int4Max is the ceiling an int4 owner column imposes regardless of the
// sequence's own maxvalue.
const int4Max = math.MaxInt32

// SequenceRow is one row of the sequences_usage query.
type SequenceRow struct {
	Schema          string
	Object          string
	LastValue       int64
	LastValueKnown  bool
	MaxValue        int64
	MinValue        int64
	FreePct         float64
	OwnedBy         string
	OwnedColumnType string
}

// RelationKeyRow is one row of relations_without_key. Every row is a table
// without a primary key — the query filters the rest out.
type RelationKeyRow struct {
	Schema         string
	Object         string
	HasUnique      bool
	UniqueNullable bool
}

// SchemaPrivilegeRow is one row of public_create_privileges.
type SchemaPrivilegeRow struct {
	Schema string
	Owner  string
}

// UnloggedRow is one row of unlogged_objects.
type UnloggedRow struct {
	Schema  string
	Object  string
	RelKind string
}

// ColumnRow is one row of uuid_like_columns.
type ColumnRow struct {
	Schema     string
	Object     string
	Column     string
	ColumnType string
}

// RelationRow is a bare relation address — what the checks that only answer
// "this table is in the list" return (relations_without_fk, _without_columns).
type RelationRow struct {
	Schema string
	Object string
}

// NameRow is one row of unsafe_names: an object whose name needs quoting, or
// collides with a reserved keyword, or both.
type NameRow struct {
	Schema   string
	Object   string
	RelKind  string
	Reserved bool
}

// ConstraintRow is one row of the invalid-constraints query the FK Analysis page
// already uses.
type ConstraintRow struct {
	Schema           string
	Object           string
	Constraint       string
	ReferencedSchema string
	ReferencedTable  string
}

// PairRow is a defect stated as two objects of one relation: a foreign key whose
// types disagree, two keys covering the same columns, two similar indexes.
type PairRow struct {
	Schema string
	Object string
	First  string
	Second string
}

// ObjectRef addresses a relation.
type ObjectRef struct {
	Schema string
	Object string
}

// Inputs is everything the repository read for one database.
type Inputs struct {
	ServerVersionNum int
	Sequences        []SequenceRow
	RelationKeys     []RelationKeyRow
	SchemaPrivileges []SchemaPrivilegeRow
	Unlogged         []UnloggedRow
	UUIDLikeColumns  []ColumnRow
	WithoutFk        []RelationRow
	WithoutColumns   []RelationRow
	UnsafeNames      []NameRow
	InvalidConstrs   []ConstraintRow
	FkTypeMismatch   []PairRow
	FkNullable       []PairRow
	FkSimilar        []PairRow
	IndexSimilar     []PairRow
	BtreeOnArray     []PairRow
	// PartitionRoots maps a partition to the root of its partition tree.
	PartitionRoots map[ObjectRef]ObjectRef
	// Skipped carries checks the repository could not run.
	Skipped   []Skip
	Truncated bool
}

// BuildReport turns raw rows into the finished report: findings with levels,
// partitions rolled up, suppressed schemas dropped, deterministic order.
func BuildReport(in Inputs, cfg Config) Report {
	findings := make([]Finding, 0, len(in.Sequences)+len(in.RelationKeys)+len(in.Unlogged))

	findings = append(findings, fromSequences(in.Sequences, cfg)...)
	findings = append(findings, fromRelationKeys(in.RelationKeys, cfg)...)
	findings = append(findings, fromSchemaPrivileges(in.SchemaPrivileges, in.ServerVersionNum, cfg)...)
	findings = append(findings, fromUnlogged(in.Unlogged, cfg)...)
	findings = append(findings, fromUUIDLikeColumns(in.UUIDLikeColumns, cfg)...)
	findings = append(findings, fromRelations(in.WithoutFk, CodeRelationWithoutFk, cfg)...)
	findings = append(findings, fromRelations(in.WithoutColumns, CodeRelationWithoutColumns, cfg)...)
	findings = append(findings, fromUnsafeNames(in.UnsafeNames, cfg)...)
	findings = append(findings, fromInvalidConstraints(in.InvalidConstrs, cfg)...)
	findings = append(findings, fromPairs(in.FkTypeMismatch, CodeFkTypeMismatch, LevelWarning, cfg)...)
	findings = append(findings, fromPairs(in.FkNullable, CodeFkNullable, LevelNotice, cfg)...)
	findings = append(findings, fromPairs(in.FkSimilar, CodeFkSimilar, LevelNotice, cfg)...)
	findings = append(findings, fromPairs(in.IndexSimilar, CodeIndexSimilar, LevelNotice, cfg)...)
	findings = append(findings, fromPairs(in.BtreeOnArray, CodeBtreeOnArray, LevelNotice, cfg)...)

	findings = collapsePartitions(findings, in.PartitionRoots)
	findings = applySuppression(findings, cfg)
	sortFindings(findings)

	skipped := append([]Skip(nil), in.Skipped...)
	skipped = append(skipped, sequencePrivilegeSkip(in.Sequences, cfg)...)
	sortSkips(skipped)

	return Report{
		Findings:  findings,
		Skipped:   skipped,
		Summary:   summarize(findings),
		Truncated: in.Truncated,
	}
}

// enabledCheck resolves a code to its registry entry, honouring the config.
func enabledCheck(code string, cfg Config) (Check, bool) {
	chk, ok := lookup(code)
	if !ok || !cfg.enabled(chk) {
		return Check{}, false
	}

	return chk, true
}

func fromSequences(rows []SequenceRow, cfg Config) []Finding {
	chk, ok := enabledCheck(CodeSequenceExhaustion, cfg)
	if !ok {
		return nil
	}

	var out []Finding

	for _, r := range rows {
		// A sequence whose last_value is hidden is reported as a skip, not as a
		// healthy sequence — see sequencePrivilegeSkip.
		if !r.LastValueKnown {
			continue
		}

		freePct, maxValue := effectiveHeadroom(r)

		level, found := cfg.sequenceLevel(freePct)
		if !found {
			continue
		}

		// An int4 owner column cannot be widened without a maintenance window,
		// so the same headroom is worth more attention there.
		if isInt4(r.OwnedColumnType) {
			level = raise(level)
		}

		usedPct := 100 - freePct
		lastValue := r.LastValue
		out = append(out, Finding{
			Code:       chk.Code,
			Level:      level,
			ObjectType: chk.ObjectType,
			Schema:     r.Schema,
			Object:     r.Object,
			Params: Params{
				UsedPct:         &usedPct,
				LastValue:       &lastValue,
				MaxValue:        &maxValue,
				OwnedBy:         r.OwnedBy,
				OwnedColumnType: r.OwnedColumnType,
			},
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

// WorstSequenceUsage returns the largest consumed share (0..1) among the
// sequences the role can actually read, and false when there is nothing to go
// on. Feeds the health score, which needs one number rather than a report.
// Sequences whose last_value is hidden are excluded: counting them as empty
// would turn a missing privilege into a permanently healthy metric.
func WorstSequenceUsage(rows []SequenceRow, cfg Config) (float64, bool) {
	var (
		worst float64
		known bool
	)

	for _, r := range rows {
		if !r.LastValueKnown || cfg.ignoredSchema(r.Schema) {
			continue
		}

		known = true

		freePct, _ := effectiveHeadroom(r)
		if used := (100 - freePct) / 100; used > worst {
			worst = used
		}
	}

	return worst, known
}

// effectiveHeadroom recomputes the headroom against the ceiling that actually
// applies. A bigint sequence owned by an int4 column overflows the column at
// 2147483647 while its own maxvalue still looks nearly untouched, which would
// otherwise report a sequence about to break the table as healthy.
func effectiveHeadroom(r SequenceRow) (freePct float64, maxValue int64) {
	if !isInt4(r.OwnedColumnType) || r.MaxValue <= int4Max {
		return r.FreePct, r.MaxValue
	}

	span := float64(int4Max) - float64(r.MinValue)
	if span <= 0 {
		return r.FreePct, r.MaxValue
	}

	free := 100 * (float64(int4Max) - float64(r.LastValue)) / span

	return math.Max(free, 0), int4Max
}

func isInt4(columnType string) bool {
	switch strings.ToLower(strings.TrimSpace(columnType)) {
	case "integer", "int4", "int":
		return true
	default:
		return false
	}
}

// sequencePrivilegeSkip reports sequences whose last_value the role cannot read.
// Without it they would silently look 100% free — "not checked" must not read as
// "clean".
func sequencePrivilegeSkip(rows []SequenceRow, cfg Config) []Skip {
	if _, ok := enabledCheck(CodeSequenceExhaustion, cfg); !ok {
		return nil
	}

	var hidden int

	for _, r := range rows {
		if !r.LastValueKnown && !cfg.ignoredSchema(r.Schema) {
			hidden++
		}
	}

	if hidden == 0 {
		return nil
	}

	return []Skip{{
		Code:   CodeSequenceExhaustion,
		Reason: SkipInsufficientPrivileges,
		Count:  hidden,
	}}
}

func fromRelationKeys(rows []RelationKeyRow, cfg Config) []Finding {
	noPK, pkEnabled := enabledCheck(CodeNoPrimaryKey, cfg)
	noUnique, uniqueEnabled := enabledCheck(CodeNoUniqueKey, cfg)

	var out []Finding

	for _, r := range rows {
		chk, enabled := noUnique, uniqueEnabled
		if r.HasUnique {
			chk, enabled = noPK, pkEnabled
		}

		if !enabled {
			continue
		}

		out = append(out, Finding{
			Code:       chk.Code,
			Level:      LevelError,
			ObjectType: chk.ObjectType,
			Schema:     r.Schema,
			Object:     r.Object,
			// Only meaningful when a unique index exists at all: it says none of
			// them can serve as a REPLICA IDENTITY, so the advice must not point
			// at "you already have one".
			Params:       Params{UniqueNullable: r.HasUnique && r.UniqueNullable},
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

func fromSchemaPrivileges(rows []SchemaPrivilegeRow, serverVersionNum int, cfg Config) []Finding {
	chk, ok := enabledCheck(CodePublicCreatePrivilege, cfg)
	if !ok {
		return nil
	}

	// PG 15 revoked the open public schema. Before that it is the factory
	// default — same exposure, but not something the owner chose.
	level := LevelError
	if serverVersionNum > 0 && serverVersionNum < 150000 {
		level = LevelWarning
	}

	out := make([]Finding, 0, len(rows))

	for _, r := range rows {
		out = append(out, Finding{
			Code:         chk.Code,
			Level:        level,
			ObjectType:   chk.ObjectType,
			Schema:       r.Schema,
			Object:       r.Schema,
			Params:       Params{Owner: r.Owner},
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

func fromUnlogged(rows []UnloggedRow, cfg Config) []Finding {
	relation, relationEnabled := enabledCheck(CodeUnloggedRelation, cfg)
	sequence, sequenceEnabled := enabledCheck(CodeUnloggedSequence, cfg)

	var out []Finding

	for _, r := range rows {
		chk, enabled := relation, relationEnabled
		if r.RelKind == "S" {
			chk, enabled = sequence, sequenceEnabled
		}

		if !enabled {
			continue
		}

		out = append(out, Finding{
			Code:         chk.Code,
			Level:        LevelWarning,
			ObjectType:   chk.ObjectType,
			Schema:       r.Schema,
			Object:       r.Object,
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

func fromUUIDLikeColumns(rows []ColumnRow, cfg Config) []Finding {
	chk, ok := enabledCheck(CodeUUIDInNonUUIDType, cfg)
	if !ok {
		return nil
	}

	out := make([]Finding, 0, len(rows))

	for _, r := range rows {
		out = append(out, Finding{
			Code:       chk.Code,
			Level:      LevelNotice,
			ObjectType: chk.ObjectType,
			Schema:     r.Schema,
			Object:     r.Object,
			Params: Params{
				Column:     r.Column,
				ColumnType: r.ColumnType,
			},
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

// fromRelations covers the checks whose whole answer is "this relation is in the
// list": there is nothing to measure, only the address.
func fromRelations(rows []RelationRow, code string, cfg Config) []Finding {
	chk, ok := enabledCheck(code, cfg)
	if !ok {
		return nil
	}

	out := make([]Finding, 0, len(rows))

	for _, r := range rows {
		out = append(out, Finding{
			Code:         chk.Code,
			Level:        LevelNotice,
			ObjectType:   chk.ObjectType,
			Schema:       r.Schema,
			Object:       r.Object,
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

// fromUnsafeNames splits one query into two codes. A name can be both a keyword
// and unquotable; the keyword is the harder failure (statements that parse as
// something else), so it wins and the object is reported once.
func fromUnsafeNames(rows []NameRow, cfg Config) []Finding {
	reserved, reservedEnabled := enabledCheck(CodeReservedWordInName, cfg)
	unsafe, unsafeEnabled := enabledCheck(CodeUnsafeCharsInName, cfg)

	var out []Finding

	for _, r := range rows {
		chk, enabled, level := unsafe, unsafeEnabled, LevelNotice
		if r.Reserved {
			chk, enabled, level = reserved, reservedEnabled, LevelWarning
		}

		if !enabled {
			continue
		}

		// Unlike every other check this one spans relkinds: an index or a sequence
		// with a keyword for a name is the same defect. Reporting it as a relation
		// would point the client at a table page that has nothing to show for it,
		// and the check's route — the tables page — is wrong there for the same
		// reason, so the finding goes without one.
		objectType := objectTypeForRelKind(r.RelKind, chk.ObjectType)

		route := chk.RelatedRoute
		if objectType != chk.ObjectType {
			route = ""
		}

		out = append(out, Finding{
			Code:         chk.Code,
			Level:        level,
			ObjectType:   objectType,
			Schema:       r.Schema,
			Object:       r.Object,
			RelatedRoute: route,
		})
	}

	return out
}

// objectTypeForRelKind names what a pg_class row actually is. Views and
// materialized views stay relations — they are addressed like tables — so only
// indexes and sequences, which are not, are separated out.
func objectTypeForRelKind(relKind, fallback string) string {
	switch relKind {
	case "i", "I":
		return ObjectTypeIndex
	case "S":
		return ObjectTypeSequence
	default:
		return fallback
	}
}

func fromInvalidConstraints(rows []ConstraintRow, cfg Config) []Finding {
	chk, ok := enabledCheck(CodeInvalidConstraint, cfg)
	if !ok {
		return nil
	}

	out := make([]Finding, 0, len(rows))

	for _, r := range rows {
		referenced := r.ReferencedTable
		if r.ReferencedSchema != "" && referenced != "" {
			referenced = r.ReferencedSchema + "." + referenced
		}

		out = append(out, Finding{
			Code:       chk.Code,
			Level:      LevelWarning,
			ObjectType: chk.ObjectType,
			Schema:     r.Schema,
			Object:     r.Object,
			Params: Params{
				Constraint:   r.Constraint,
				ReferencedBy: referenced,
			},
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

// fromPairs converts the checks whose finding is "these two things on this
// relation disagree". The first name is what the finding is addressed by, so two
// pairs on one table stay two rows through the partition rollup.
func fromPairs(rows []PairRow, code string, level Level, cfg Config) []Finding {
	chk, ok := enabledCheck(code, cfg)
	if !ok {
		return nil
	}

	out := make([]Finding, 0, len(rows))

	for _, r := range rows {
		params := Params{OtherObject: r.Second}
		if chk.ObjectType == ObjectTypeIndex {
			params.Index = r.First
		} else {
			params.Constraint = r.First
		}

		out = append(out, Finding{
			Code:         chk.Code,
			Level:        level,
			ObjectType:   chk.ObjectType,
			Schema:       r.Schema,
			Object:       r.Object,
			Params:       params,
			RelatedRoute: chk.RelatedRoute,
		})
	}

	return out
}

// collapsePartitions folds findings on partitions into one finding on the root
// table. One defect on a table partitioned by day would otherwise produce
// hundreds of rows and drown the rest of the report.
func collapsePartitions(findings []Finding, roots map[ObjectRef]ObjectRef) []Finding {
	if len(findings) == 0 {
		return findings
	}

	// What the finding is about within the relation is part of the key: two
	// columns (or two constraints) of one table are two defects, and folding
	// them together would report one and hide the other.
	type groupKey struct {
		code   string
		root   ObjectRef
		within string
	}

	var (
		out    = make([]Finding, 0, len(findings))
		groups = make(map[groupKey]int, len(findings))
	)

	for _, f := range findings {
		chk, ok := lookup(f.Code)
		if !ok || !chk.CollapseParts {
			out = append(out, f)
			continue
		}

		self := ObjectRef{Schema: f.Schema, Object: f.Object}

		root, isChild := roots[self]
		if !isChild {
			root = self
		}

		key := groupKey{
			code:   f.Code,
			root:   root,
			within: f.Params.Column + "\x00" + f.Params.Constraint + "\x00" + f.Params.Index,
		}

		idx, seen := groups[key]
		if !seen {
			f.Schema, f.Object = root.Schema, root.Object
			if isChild {
				f.Params.Partitions = 1
			}

			groups[key] = len(out)
			out = append(out, f)

			continue
		}

		out[idx] = mergePartitionFinding(out[idx], f, isChild)
	}

	return out
}

// mergePartitionFinding keeps the worst child's numbers on the rolled-up row, so
// collapsing never hides the partition closest to breaking.
func mergePartitionFinding(base, child Finding, childIsPartition bool) Finding {
	partitions := base.Params.Partitions
	if childIsPartition {
		partitions++
	}

	// Any nullable unique index in the tree makes the "you already have a unique
	// index" advice unsafe for the whole table.
	uniqueNullable := base.Params.UniqueNullable || child.Params.UniqueNullable

	out := base
	if worseThan(child, base) {
		out = child
		out.Schema, out.Object = base.Schema, base.Object
	}

	out.Params.Partitions = partitions
	out.Params.UniqueNullable = uniqueNullable

	return out
}

// worseThan compares two findings of the same check: higher level first, then
// the larger share of the resource consumed.
func worseThan(a, b Finding) bool {
	if levelRank(a.Level) != levelRank(b.Level) {
		return levelRank(a.Level) < levelRank(b.Level)
	}

	return deref(a.Params.UsedPct) > deref(b.Params.UsedPct)
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}

	return *v
}

// applySuppression drops findings in schemas the operator masked out. System
// schemas are already excluded in SQL; this covers extension and tenant schemas.
func applySuppression(findings []Finding, cfg Config) []Finding {
	if len(cfg.IgnoreSchemas) == 0 {
		return findings
	}

	out := findings[:0]

	for _, f := range findings {
		if cfg.ignoredSchema(f.Schema) {
			continue
		}

		out = append(out, f)
	}

	return out
}

// sortFindings gives a stable order: errors first, then by check, then by
// object. Pagination over an unstable order would repeat and drop rows.
func sortFindings(findings []Finding) {
	slices.SortFunc(findings, func(a, b Finding) int {
		if c := cmp.Compare(levelRank(a.Level), levelRank(b.Level)); c != 0 {
			return c
		}

		if c := cmp.Compare(a.Code, b.Code); c != 0 {
			return c
		}

		if c := cmp.Compare(a.Schema, b.Schema); c != 0 {
			return c
		}

		if c := cmp.Compare(a.Object, b.Object); c != 0 {
			return c
		}

		// Several findings of one check can share an object — one per column,
		// constraint or index. Without these the order between them would rest
		// on the order the rows arrived in, and pagination would repeat or drop
		// rows between requests.
		if c := cmp.Compare(a.Params.Column, b.Params.Column); c != 0 {
			return c
		}

		if c := cmp.Compare(a.Params.Constraint, b.Params.Constraint); c != 0 {
			return c
		}

		return cmp.Compare(a.Params.Index, b.Params.Index)
	})
}

func sortSkips(skips []Skip) {
	slices.SortFunc(skips, func(a, b Skip) int {
		if c := cmp.Compare(a.Code, b.Code); c != 0 {
			return c
		}

		return cmp.Compare(string(a.Reason), string(b.Reason))
	})
}

func summarize(findings []Finding) map[Level]int {
	summary := map[Level]int{LevelError: 0, LevelWarning: 0, LevelNotice: 0}
	for _, f := range findings {
		summary[f.Level]++
	}

	return summary
}
