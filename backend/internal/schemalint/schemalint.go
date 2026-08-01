// Package schemalint turns raw catalog rows into a list of schema defects.
// It holds no database access: the repository reads the rows, this package
// decides what is a finding, how severe it is and how findings roll up.
package schemalint

// Level is the severity of a finding. Three levels only — a fourth would compete
// with the HIGH/MEDIUM/LOW scale health-score recommendations already use.
type Level string

const (
	LevelError   Level = "error"
	LevelWarning Level = "warning"
	LevelNotice  Level = "notice"
)

// Object types a finding can point at.
const (
	ObjectTypeSequence   = "sequence"
	ObjectTypeRelation   = "relation"
	ObjectTypeSchema     = "schema"
	ObjectTypeAttribute  = "attribute"
	ObjectTypeConstraint = "constraint"
	ObjectTypeIndex      = "index"
)

// Check codes. Texts live in the frontend i18n bundle under
// schemaLint.checks.<code>.*, keyed by these — SQL carries no prose.
const (
	CodeSequenceExhaustion     = "sequence_exhaustion"
	CodeNoPrimaryKey           = "no_primary_key"
	CodeNoUniqueKey            = "no_unique_key"
	CodePublicCreatePrivilege  = "public_create_privilege"
	CodeUnloggedRelation       = "unlogged_relation"
	CodeUnloggedSequence       = "unlogged_sequence"
	CodeUUIDInNonUUIDType      = "uuid_in_non_uuid_type"
	CodeRelationWithoutFk      = "relation_without_fk"
	CodeRelationWithoutColumns = "relation_without_columns"
	CodeReservedWordInName     = "reserved_word_in_name"
	CodeUnsafeCharsInName      = "unsafe_chars_in_name"
	CodeInvalidConstraint      = "invalid_constraint"
	CodeFkTypeMismatch         = "fk_type_mismatch"
	CodeFkNullable             = "fk_nullable"
	CodeFkSimilar              = "fk_similar"
	CodeIndexSimilar           = "index_similar"
	CodeBtreeOnArray           = "btree_on_array"
)

// Finding is one detected defect.
type Finding struct {
	Code         string
	Level        Level
	ObjectType   string
	Schema       string
	Object       string
	Params       Params
	RelatedRoute string
}

// Params carries the numbers and names a finding's phrasing quotes. Which fields
// are set depends on Code — the same reason_code/reason_params split the
// unused-index report uses.
type Params struct {
	UsedPct         *float64
	LastValue       *int64
	MaxValue        *int64
	OwnedBy         string // table.column owning a sequence
	OwnedColumnType string // an int4 owner raises the level one step
	Owner           string // schema owner, for public_create_privilege
	Column          string // attribute the finding is about, when it is not the whole relation
	ColumnType      string // declared type of that attribute
	Constraint      string // constraint the finding is about
	ReferencedBy    string // schema.table on the other side of a foreign key
	Index           string // index the finding is about
	OtherObject     string // the second object of a pair (similar index, similar key)
	Partitions      int    // >0 when children were collapsed into this row
	UniqueNullable  bool   // a unique index exists but is nullable → not a replica identity
}

// SkipReason explains why a check produced no findings.
type SkipReason string

const (
	SkipInsufficientPrivileges SkipReason = "insufficient_privileges"
	SkipUnsupportedVersion     SkipReason = "unsupported_version"
	SkipDisabled               SkipReason = "disabled"
	SkipError                  SkipReason = "error"
)

// Skip records a check that did not run, or ran without being able to see
// everything. Reported next to the findings so a missing check never reads as a
// clean result.
type Skip struct {
	Code   string
	Reason SkipReason
	// Detail is a technical marker only — a SQLSTATE or "timeout". Prose belongs
	// in the frontend i18n bundle, so anything a user reads is rendered there.
	Detail string
	// Count is how many objects the check could not look at, when it can say.
	Count int
}

// DatabaseSummary is one database's line in the instance-wide overview. Schema
// defects live in a database, not in a cluster, so the per-database report alone
// cannot answer "what is the state of this instance".
type DatabaseSummary struct {
	Database string
	Error    int
	Warning  int
	Notice   int
	// Skipped counts checks that did not run in this database. Zero findings
	// with a non-zero Skipped is not a clean schema, it is an unchecked one.
	Skipped int
	// Failed marks a database that could not be read at all. It is not zero
	// findings — reporting it as clean would be a lie.
	Failed bool
}

// Report is the finished answer for one database.
type Report struct {
	Findings   []Finding
	Skipped    []Skip
	Summary    map[Level]int
	Truncated  bool
	DurationMs int64
}

// NotRun counts the checks that produced no result for a reason other than the
// operator switching them off — what the summary has to carry so that zero
// findings cannot be read as a clean schema.
func (r Report) NotRun() int {
	n := 0

	for _, s := range r.Skipped {
		if s.Reason != SkipDisabled {
			n++
		}
	}

	return n
}

// levelRank orders severities for sorting and for stepping a level up.
func levelRank(l Level) int {
	switch l {
	case LevelError:
		return 0
	case LevelWarning:
		return 1
	case LevelNotice:
		return 2
	default:
		return 3
	}
}

// raise moves a level one step towards error. Used for defects whose blast
// radius is larger than the check's own numbers suggest (an int4 owner column).
func raise(l Level) Level {
	switch l {
	case LevelNotice:
		return LevelWarning
	case LevelWarning, LevelError:
		return LevelError
	default:
		return l
	}
}
