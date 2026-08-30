package mcpserver

import (
	"cmp"
	"context"
	"slices"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const (
	defaultAdvisorLimit = 10
	maxAdvisorLimit     = 30

	// maxCoveredPerCandidate caps the statements shown per candidate: the top few
	// identify the workload, the tail only costs context.
	maxCoveredPerCandidate = 3

	// maxAdvisorQueryBytes caps one statement's text, and applies only when the
	// caller asked for text at all.
	maxAdvisorQueryBytes = 1000

	// advisorUnparsedGapPct is the share of analyzed statements that must be left
	// unresolved before the candidate list counts as built on a partial workload.
	advisorUnparsedGapPct = 20.0
)

// Gap codes: what part of the workload or catalog never reached the analysis.
const (
	gapPgssUnavailable  = "pgss_unavailable"
	gapNoWorkload       = "no_workload"
	gapHostsUnreachable = "hosts_unreachable"
	gapHostsNoStats     = "hosts_without_stats"
	gapPartlyUnparsed   = "workload_partly_unparsed"
	gapCatalogTruncated = "catalog_truncated"
)

// advisorReasonGap classifies every not_parsed code: true when the statement was
// never resolved (its share of the workload is invisible to the analysis), false
// when it was read to the end and simply yielded no candidate. An unrecognized
// code counts as a gap — over-warning is cheaper than a false all-clear.
var advisorReasonGap = map[string]bool{
	"truncated":              true,
	"too_long":               true,
	"parse_error":            true,
	"unsupported_syntax":     true,
	"insufficient_privilege": true,
	"empty":                  true,
	"unknown_relation":       true,
	"ambiguous_name":         true,
	"ambiguous_column":       true,
	"unknown_column":         true,

	"unsupported_type":       false,
	"already_indexed":        false,
	"system_relation":        false,
	"table_too_small":        false,
	"no_indexable_predicate": false,
	"or_predicate":           false,
	"expression_predicate":   false,
}

// advisorRowReasons are the codes the collector tallies once per
// pg_stat_statements row. Every other code is counted once per collapsed group,
// and a group holds a row per host it ran on, so the two shares are measured
// against different totals.
var advisorRowReasons = map[string]bool{
	"truncated":              true,
	"too_long":               true,
	"parse_error":            true,
	"unsupported_syntax":     true,
	"insufficient_privilege": true,
	"empty":                  true,
}

type advisorCoveredQuery struct {
	Fingerprint   string            `json:"fingerprint"`
	WeightPct     float64           `json:"weight_pct"`
	Calls         int64             `json:"calls"`
	Hosts         []string          `json:"hosts"`
	QueryIDByHost map[string]string `json:"query_id_by_host"`
	Query         string            `json:"query,omitempty"`
}

type advisorCandidate struct {
	Schema              string                          `json:"schema"`
	Table               string                          `json:"table"`
	Columns             []string                        `json:"columns"`
	Predicate           string                          `json:"predicate,omitempty"`
	DDL                 string                          `json:"ddl"`
	WeightPct           float64                         `json:"weight_pct"`
	TableRows           int64                           `json:"table_rows"`
	Writes              apiclient.IndexAdvisorWrites    `json:"writes"`
	Warnings            []apiclient.IndexAdvisorWarning `json:"warnings"`
	CoveredQueriesTotal int                             `json:"covered_queries_total"`
	CoveredQueries      []advisorCoveredQuery           `json:"covered_queries"`
}

type advisorResult struct {
	Database           string                            `json:"database"`
	PlannerChecked     bool                              `json:"planner_checked"`
	CandidatesTotal    int                               `json:"candidates_total"`
	CandidatesReturned int                               `json:"candidates_returned"`
	Gaps               []string                          `json:"gaps"`
	Candidates         []advisorCandidate                `json:"candidates"`
	NotParsed          []apiclient.IndexAdvisorNotParsed `json:"not_parsed"`
	UnreachableHosts   []string                          `json:"unreachable_hosts"`
	Summary            apiclient.IndexAdvisorSummary     `json:"summary"`
	DurationMs         int64                             `json:"duration_ms"`
}

func indexAdvisor(ctx context.Context, c *DashaClient, a indexAdvisorArgs) (any, error) {
	limit := advisorLimit(a.Limit)

	rep, err := c.IndexAdvisor(ctx, a.Cluster, a.Database, a.ExcludeUsers, limit)
	if err != nil {
		return nil, err
	}

	return advisorReport(a.Database, rep, a.IncludeQueries), nil
}

// advisorLimit clamps silently: what was cut is visible in candidates_total, and
// an error over a number the caller is free to guess helps nobody.
func advisorLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultAdvisorLimit
	case limit > maxAdvisorLimit:
		return maxAdvisorLimit
	default:
		return limit
	}
}

func advisorReport(database string, rep *apiclient.IndexAdvisorReport, includeQueries bool) advisorResult {
	out := advisorResult{
		Database:           database,
		PlannerChecked:     advisorPlannerChecked(rep.Candidates),
		CandidatesTotal:    rep.Total,
		CandidatesReturned: len(rep.Candidates),
		Gaps:               advisorGaps(rep),
		Candidates:         make([]advisorCandidate, 0, len(rep.Candidates)),
		NotParsed:          advisorNotParsed(rep.NotParsed),
		UnreachableHosts:   rep.UnreachableHosts,
		Summary:            rep.Summary,
		DurationMs:         rep.DurationMs,
	}

	for _, c := range rep.Candidates {
		out.Candidates = append(out.Candidates, advisorCandidateOf(c, includeQueries))
	}

	return out
}

// advisorPlannerChecked lifts the per-candidate flag to the report: it is false
// throughout this step, and repeating one constant on every candidate spends
// context on nothing.
func advisorPlannerChecked(candidates []apiclient.IndexAdvisorCandidate) bool {
	for _, c := range candidates {
		if !c.PlannerChecked {
			return false
		}
	}

	return len(candidates) > 0
}

func advisorCandidateOf(c apiclient.IndexAdvisorCandidate, includeQueries bool) advisorCandidate {
	return advisorCandidate{
		Schema:              c.Schema,
		Table:               c.Table,
		Columns:             c.Columns,
		Predicate:           c.Predicate,
		DDL:                 c.Ddl,
		WeightPct:           c.WeightPct,
		TableRows:           c.TableRows,
		Writes:              c.Writes,
		Warnings:            c.Warnings,
		CoveredQueriesTotal: len(c.CoveredQueries),
		CoveredQueries:      advisorCovered(c.CoveredQueries, includeQueries),
	}
}

// advisorCovered keeps the heaviest statements of a candidate. query_ids is
// dropped: a drill-down needs the queryid of a named host, which query_id_by_host
// carries and the flat list cannot.
func advisorCovered(qs []apiclient.IndexAdvisorCoveredQuery, includeQueries bool) []advisorCoveredQuery {
	ranked := slices.Clone(qs)
	slices.SortStableFunc(ranked, func(a, b apiclient.IndexAdvisorCoveredQuery) int {
		return cmp.Compare(b.WeightPct, a.WeightPct)
	})

	if len(ranked) > maxCoveredPerCandidate {
		ranked = ranked[:maxCoveredPerCandidate]
	}

	out := make([]advisorCoveredQuery, 0, len(ranked))

	for _, q := range ranked {
		row := advisorCoveredQuery{
			Fingerprint:   q.Fingerprint,
			WeightPct:     q.WeightPct,
			Calls:         q.Calls,
			Hosts:         q.Hosts,
			QueryIDByHost: q.QueryIdByHost,
			Query:         "",
		}

		if includeQueries {
			row.Query = clipTo(q.Query, maxAdvisorQueryBytes)
		}

		out = append(out, row)
	}

	return out
}

// advisorNotParsed travels whole — an empty candidate list is unreadable without
// it — worst first.
func advisorNotParsed(rows []apiclient.IndexAdvisorNotParsed) []apiclient.IndexAdvisorNotParsed {
	out := make([]apiclient.IndexAdvisorNotParsed, len(rows))
	copy(out, rows)

	slices.SortStableFunc(out, func(a, b apiclient.IndexAdvisorNotParsed) int {
		return cmp.Compare(b.Count, a.Count)
	})

	return out
}

// advisorGaps states machine-readably what the analysis did not see. Empty gaps
// is the only condition under which an empty candidate list means there is
// nothing to propose.
func advisorGaps(rep *apiclient.IndexAdvisorReport) []string {
	gaps := []string{}

	// A readable pg_stat_statements holding nothing — after a reset or a restart —
	// is a different answer from an unreadable one.
	switch {
	case !rep.Summary.PgssAvailable:
		gaps = append(gaps, gapPgssUnavailable)
	case rep.Summary.AnalyzedQueries <= 0:
		gaps = append(gaps, gapNoWorkload)
	}

	if len(rep.UnreachableHosts) > 0 {
		gaps = append(gaps, gapHostsUnreachable)
	}

	if len(rep.Summary.HostsWithoutStats) > 0 {
		gaps = append(gaps, gapHostsNoStats)
	}

	if advisorPartlyUnparsed(rep) {
		gaps = append(gaps, gapPartlyUnparsed)
	}

	if rep.Summary.CatalogTruncated {
		gaps = append(gaps, gapCatalogTruncated)
	}

	return gaps
}

func advisorPartlyUnparsed(rep *apiclient.IndexAdvisorReport) bool {
	rows, groups := 0, 0

	for _, n := range rep.NotParsed {
		if gap, known := advisorReasonGap[n.ReasonCode]; known && !gap {
			continue
		}

		if advisorRowReasons[n.ReasonCode] {
			rows += n.Count
			continue
		}

		groups += n.Count
	}

	return advisorOverGapPct(rows, rep.Summary.AnalyzedQueries) ||
		advisorOverGapPct(groups, rep.Summary.CollapsedGroups)
}

func advisorOverGapPct(unresolved, total int) bool {
	if unresolved == 0 {
		return false
	}

	if total <= 0 {
		return true
	}

	return float64(unresolved)/float64(total)*100 >= advisorUnparsedGapPct
}
