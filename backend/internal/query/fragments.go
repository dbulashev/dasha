package query

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed fragments/cold_tables.sql
var coldTablesFragment string

// ColdCTE renders the cold-table CTEs (cold_target … cold_rel) reading the
// schema, table and writes arrays from $first, $first+1 and $first+2.
func ColdCTE(first int) string {
	return strings.NewReplacer(
		"$SCHEMAS", "$"+strconv.Itoa(first),
		"$TABLES", "$"+strconv.Itoa(first+1),
		"$WRITES", "$"+strconv.Itoa(first+2),
	).Replace(strings.TrimSpace(coldTablesFragment))
}

//go:embed fragments/stats_preloaded.sql
var statsPreloadedFragment string

// StatsPreloaded renders a predicate that a library declares one of the <ext>.max
// settings listed in $param. An undeclared setting is absent or a string placeholder.
func StatsPreloaded(param int) string {
	return strings.ReplaceAll(strings.TrimSpace(statsPreloadedFragment), "$SETTINGS", "$"+strconv.Itoa(param))
}
