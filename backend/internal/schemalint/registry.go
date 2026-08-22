package schemalint

import (
	"slices"

	"github.com/dbulashev/dasha/internal/enums"
)

// MinSupportedVersion is the baseline every check is written against (PG 14,
// the oldest version in the CI matrix). A check needing more declares it.
const MinSupportedVersion = 140000

// Check describes one defect the report can produce. It is a description, not
// an executor: several checks may share one query when the catalog rows differ
// only by a flag, and the code is picked while converting rows to findings.
type Check struct {
	Code           string
	ObjectType     string
	Query          enums.Query
	MinVersion     int
	DefaultEnabled bool
	RelatedRoute   string
	CollapseParts  bool // roll findings up to the root partitioned table
	// RequiredPrivilege names the grant a monitoring role needs beyond the
	// basics, so the page can say what to give when the check is skipped.
	RequiredPrivilege string
}

// registry is the single source of truth for the API, the page and MCP.
var registry = []Check{
	{
		Code:           CodeSequenceExhaustion,
		ObjectType:     ObjectTypeSequence,
		Query:          enums.QuerySchemaLintSequencesUsage,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/schema-lint",
		// pg_monitor / pg_read_all_stats do NOT cover this: last_value is gated on
		// SELECT or USAGE on the sequence itself, or ownership.
		RequiredPrivilege: "SELECT or USAGE on the sequences (GRANT SELECT ON ALL SEQUENCES IN SCHEMA ... TO <role>)",
	},
	{
		Code:           CodeNoPrimaryKey,
		ObjectType:     ObjectTypeRelation,
		Query:          enums.QuerySchemaLintRelationsWithoutKey,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
		CollapseParts:  true,
	},
	{
		Code:           CodeNoUniqueKey,
		ObjectType:     ObjectTypeRelation,
		Query:          enums.QuerySchemaLintRelationsWithoutKey,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
		CollapseParts:  true,
	},
	{
		Code:           CodePublicCreatePrivilege,
		ObjectType:     ObjectTypeSchema,
		Query:          enums.QuerySchemaLintPublicCreatePrivileges,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/schema-lint",
	},
	{
		Code:           CodeUnloggedRelation,
		ObjectType:     ObjectTypeRelation,
		Query:          enums.QuerySchemaLintUnloggedObjects,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
		CollapseParts:  true,
	},
	{
		Code:           CodeUnloggedSequence,
		ObjectType:     ObjectTypeSequence,
		Query:          enums.QuerySchemaLintUnloggedObjects,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/schema-lint",
	},
	{
		Code:           CodeUUIDInNonUUIDType,
		ObjectType:     ObjectTypeAttribute,
		Query:          enums.QuerySchemaLintUuidLikeColumns,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
		CollapseParts:  true,
	},
	{
		Code:       CodeRelationWithoutFk,
		ObjectType: ObjectTypeRelation,
		Query:      enums.QuerySchemaLintRelationsWithoutFk,
		MinVersion: MinSupportedVersion,
		// Off by default: on schemas that deliberately avoid foreign keys this
		// fires on every table and drowns the report.
		DefaultEnabled: false,
		RelatedRoute:   "/fk-analysis",
		CollapseParts:  true,
	},
	{
		Code:           CodeRelationWithoutColumns,
		ObjectType:     ObjectTypeRelation,
		Query:          enums.QuerySchemaLintRelationsWithoutColumns,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
		CollapseParts:  true,
	},
	{
		Code:           CodeReservedWordInName,
		ObjectType:     ObjectTypeRelation,
		Query:          enums.QuerySchemaLintUnsafeNames,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
	},
	{
		Code:           CodeUnsafeCharsInName,
		ObjectType:     ObjectTypeRelation,
		Query:          enums.QuerySchemaLintUnsafeNames,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/tables",
	},
	// The checks below already power pages of their own. The report reuses their
	// queries rather than reimplementing the logic, so the two views cannot drift
	// apart
	{
		Code:           CodeInvalidConstraint,
		ObjectType:     ObjectTypeConstraint,
		Query:          enums.QueryConstraintsInvalidConstraints,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/fk-analysis",
		CollapseParts:  true,
	},
	{
		Code:           CodeFkTypeMismatch,
		ObjectType:     ObjectTypeConstraint,
		Query:          enums.QueryFksTypeMismatch,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/fk-analysis",
		CollapseParts:  true,
	},
	{
		Code:           CodeFkNullable,
		ObjectType:     ObjectTypeConstraint,
		Query:          enums.QueryFksPossibleNulls,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/fk-analysis",
		CollapseParts:  true,
	},
	{
		// "Possibly similar" is a candidate list for a human to judge, not a
		// defect — off until asked for, like relation_without_fk.
		Code:           CodeFkSimilar,
		ObjectType:     ObjectTypeConstraint,
		Query:          enums.QueryFksPossibleSimilar1,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: false,
		RelatedRoute:   "/fk-analysis",
		CollapseParts:  true,
	},
	{
		Code:           CodeIndexSimilar,
		ObjectType:     ObjectTypeIndex,
		Query:          enums.QueryIndexesSimilar3,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: false,
		RelatedRoute:   "/indexes-problems",
		CollapseParts:  true,
	},
	{
		Code:           CodeBtreeOnArray,
		ObjectType:     ObjectTypeIndex,
		Query:          enums.QueryIndexesBtreeOnArray,
		MinVersion:     MinSupportedVersion,
		DefaultEnabled: true,
		RelatedRoute:   "/indexes-problems",
		CollapseParts:  true,
	},
}

// Registry returns every known check, in report order.
func Registry() []Check {
	return slices.Clone(registry)
}

// lookup finds a check by code.
func lookup(code string) (Check, bool) {
	for _, c := range registry {
		if c.Code == code {
			return c, true
		}
	}

	return Check{}, false
}

// QueryPlan is one query to run plus the checks it feeds.
type QueryPlan struct {
	Query enums.Query
	Codes []string
}

// Plan splits the registry into what to run and what to report as skipped.
// Queries are deduplicated: one statement serves every check reading the same
// catalog rows. A query runs while at least one of its checks is enabled.
func Plan(cfg Config, serverVersionNum int) ([]QueryPlan, []Skip) {
	var (
		plans []QueryPlan
		skips []Skip
	)

	index := make(map[enums.Query]int, len(registry))

	for _, chk := range registry {
		switch {
		case !cfg.enabled(chk):
			skips = append(skips, Skip{Code: chk.Code, Reason: SkipDisabled})
			continue
		case serverVersionNum > 0 && serverVersionNum < chk.MinVersion:
			skips = append(skips, Skip{Code: chk.Code, Reason: SkipUnsupportedVersion})
			continue
		}

		if i, ok := index[chk.Query]; ok {
			plans[i].Codes = append(plans[i].Codes, chk.Code)
			continue
		}

		index[chk.Query] = len(plans)
		plans = append(plans, QueryPlan{Query: chk.Query, Codes: []string{chk.Code}})
	}

	return plans, skips
}

// RequiredPrivilege returns the grant a check needs beyond the basics, empty
// when it reads what any role can read.
func RequiredPrivilege(code string) string {
	chk, ok := lookup(code)
	if !ok {
		return ""
	}

	return chk.RequiredPrivilege
}
