package sqlparse

import (
	"regexp"
	"strings"
)

// The grammar lets a type name precede a string constant only — `timestamptz
// '2024-01-01'` — and pg_stat_statements normalizes the constant while keeping
// the type name, leaving `timestamptz $1`, which nothing will parse.
//
// The type names are a closed list on purpose: `LIMIT $1`, `LIKE $1` and
// `AT TIME ZONE $1` have the same shape and must survive untouched.
var (
	intervalParam = regexp.MustCompile(`(?i)\binterval(\s*\(\s*\d+\s*\))?\s+(\$\d+)` +
		`((?:\s+(?:year|month|day|hour|minute|second)\b(?:\s*\(\s*\d+\s*\))?` +
		`(?:\s+to\s+(?:month|hour|minute|second)\b(?:\s*\(\s*\d+\s*\))?)?)?)`)

	typedParam = regexp.MustCompile(`(?i)\b((?:[a-z_][a-z0-9_$]*\s*\.\s*)?\b(?:` +
		`timestamptz|timetz|` +
		`timestamp(?:\s*\(\s*\d+\s*\))?(?:\s+with(?:out)?\s+time\s+zone)?|` +
		`time(?:\s*\(\s*\d+\s*\))?(?:\s+with(?:out)?\s+time\s+zone)?|` +
		`double\s+precision|national\s+char(?:acter)?(?:\s+varying)?|` +
		`char(?:acter)?(?:\s+varying)?|bit(?:\s+varying)?|varbit|varchar|bpchar|nchar|` +
		`numeric|decimal|dec|float8|float4|float|real|integer|int8|int4|int2|int|` +
		`smallint|bigint|boolean|bool|date|text|uuid|jsonb|json|bytea|inet|cidr|` +
		`macaddr8|macaddr|money|xml|name|oid|regclass|tsvector|tsquery|citext|hstore` +
		`)(?:\s*\(\s*\d+\s*(?:,\s*\d+\s*)?\))?)\s+(\$\d+)\b`)
)

// RestoreParamCasts rewrites `timestamptz $1` into `$1::timestamptz` and
// `interval $1 day` into `CAST($1 AS interval day)`. The cast lands on the
// parameter, never on a column, so the plan the text describes is unchanged.
// An unlisted type name is left alone: a missed rewrite parses exactly as well
// as it does now.
func RestoreParamCasts(sql string) string {
	if !strings.Contains(sql, "$") {
		return sql
	}

	out := intervalParam.ReplaceAllString(sql, `CAST(${2} AS interval${1}${3})`)

	return typedParam.ReplaceAllString(out, `${2}::${1}`)
}
