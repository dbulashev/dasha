package indexadvisor

import (
	"time"

	"github.com/dbulashev/dasha/internal/explain"
	"github.com/dbulashev/dasha/internal/logs/insights"
)

const seqScanNode = "Seq Scan"

// EvidenceState keeps three answers apart. Found and NotFound are both
// statements about the database; NotSearched is a statement about Dasha, and
// merging it into NotFound would turn a missing log source into an argument
// against the index.
type EvidenceState string

const (
	EvidenceNotSearched EvidenceState = "not_searched"
	EvidenceNotFound    EvidenceState = "not_found"
	EvidenceFound       EvidenceState = "found"
)

// Evidence is what the plans logged over a window say about a candidate: how
// many of them scan its table sequentially, and what those nodes cost.
//
// Times and discarded rows are measured on one sampled plan per shape — the
// slowest of it — and never multiplied by how often the shape ran, so they are
// the weight of one bad execution rather than an extrapolated total.
type Evidence struct {
	State        EvidenceState
	Plans        int
	SeqScanNodes int
	ActualTimeMs float64
	RowsRemoved  float64
	From, To     time.Time
	// Partial means the window was read only in part, which makes every count
	// here a lower bound.
	Partial bool
}

// AttachEvidence fills in the evidence of every candidate from the plans of one
// window and raises the stale-statistics warning where the plans earned it.
//
// The match is by statement identifier: the candidate carries the queryid of
// every statement it covers, and the log record carries the one PostgreSQL
// computed, so nothing here has to guess at aliases or column names in the text
// of a node condition.
func AttachEvidence(rep *Report, w insights.PlanWindow, from, to time.Time) {
	// A window holding no plan at all says nothing about any candidate: the
	// source may be there and auto_explain not writing. The candidates keep the
	// not_searched Build gave them rather than turning silence into not_found.
	if len(w.Groups) == 0 && w.PlanRecords == 0 {
		return
	}

	byID := groupsByQueryID(w.Groups)
	ambiguous := ambiguousTables(rep.Candidates)

	for i := range rep.Candidates {
		c := &rep.Candidates[i]
		groups := matchGroups(byID, c)

		c.Evidence = evidenceOf(c, groups, ambiguous, w.Partial, from, to)

		if warn, ok := staleStatistics(c, groups, ambiguous); ok {
			c.Warnings = append(c.Warnings, warn)
		}
	}
}

// ambiguousTables are the table names the report holds in more than one schema.
// A plan logged without VERBOSE prints no schema, and crediting such a node to
// every candidate of that name would invent evidence for all but one of them.
func ambiguousTables(candidates []Candidate) map[string]bool {
	schema := make(map[string]string, len(candidates))
	out := map[string]bool{}

	for i := range candidates {
		c := &candidates[i]

		if s, ok := schema[c.Table]; ok {
			if s != c.Schema {
				out[c.Table] = true
			}

			continue
		}

		schema[c.Table] = c.Schema
	}

	return out
}

func groupsByQueryID(groups []insights.PlanGroup) map[int64][]insights.PlanGroup {
	out := make(map[int64][]insights.PlanGroup, len(groups))

	for _, g := range groups {
		if !g.HasQueryID {
			continue
		}

		out[g.QueryID] = append(out[g.QueryID], g)
	}

	return out
}

// matchGroups collects the plan shapes of every statement the candidate covers.
// Ord is unique within a scan, so a shape reached through two covered statements
// is counted once.
func matchGroups(byID map[int64][]insights.PlanGroup, c *Candidate) []insights.PlanGroup {
	var out []insights.PlanGroup

	seen := map[int]bool{}

	for _, q := range c.Covered {
		for _, id := range q.QueryIDs {
			for _, g := range byID[id] {
				if seen[g.Ord] {
					continue
				}

				seen[g.Ord] = true

				out = append(out, g)
			}
		}
	}

	return out
}

func evidenceOf(
	c *Candidate,
	groups []insights.PlanGroup,
	ambiguous map[string]bool,
	partial bool,
	from, to time.Time,
) Evidence {
	ev := Evidence{ //nolint:exhaustruct
		State:   EvidenceNotFound,
		From:    from,
		To:      to,
		Partial: partial,
	}

	undecided := false

	for i := range groups {
		g := &groups[i]

		scans := seqScansOn(&g.Sample, c.Schema, c.Table, ambiguous)
		undecided = undecided || scans.undecided

		if scans.nodes == 0 {
			continue
		}

		ev.Plans += g.Count
		ev.SeqScanNodes += scans.nodes
		ev.ActualTimeMs += scans.timeMs
		ev.RowsRemoved += scans.rowsRemoved
	}

	switch {
	case ev.Plans > 0:
		ev.State = EvidenceFound
	case partial || undecided:
		// not_found claims every plan was read and none matched, which neither a
		// window read in part nor a scan of a name held in two schemas supports.
		ev.State = EvidenceNotSearched
	}

	return ev
}

// seqScans is what one plan says about a relation.
type seqScans struct {
	nodes       int
	timeMs      float64
	rowsRemoved float64
	// undecided marks a scan of the table name under no schema while the report
	// holds that name in two: the scan is real, whose it is unknown.
	undecided bool
}

// seqScansOn measures the sequential scans of one relation in a plan. A node
// logged without ANALYZE still counts: the scan happened, only its cost is
// unknown, and dropping it would understate the evidence on a cluster running
// auto_explain.log_analyze = off.
func seqScansOn(p *explain.Plan, schema, table string, ambiguous map[string]bool) seqScans {
	var out seqScans

	p.Walk(func(_ []int, n *explain.Node) bool {
		if n.Type != seqScanNode || n.Relation != table {
			return true
		}

		if !relationIs(n, schema, table, ambiguous) {
			out.undecided = out.undecided || n.Schema == ""

			return true
		}

		out.nodes++

		if n.Actual == nil {
			return true
		}

		out.timeMs += n.Actual.TotalTime * n.Actual.Loops

		if n.RowsRemovedByFilter != nil {
			out.rowsRemoved += *n.RowsRemovedByFilter * n.Actual.Loops
		}

		return true
	})

	return out
}

// relationIs matches a plan node against a catalog relation. A plan logged
// without VERBOSE prints no schema, and then the bare name is all there is —
// unless the report holds that name in more than one schema, where it names no
// relation at all.
func relationIs(n *explain.Node, schema, table string, ambiguous map[string]bool) bool {
	if n.Relation != table {
		return false
	}

	if n.Schema == "" {
		return !ambiguous[table]
	}

	return n.Schema == schema
}

// staleStatistics fires when a plan of a covered statement misestimated the rows
// of the candidate's own table. The key order and the partial predicate were
// chosen from pg_stats, so a planner that already reads that table wrong is
// choosing them from the same wrong numbers.
func staleStatistics(
	c *Candidate, groups []insights.PlanGroup, ambiguous map[string]bool,
) (Warning, bool) {
	var worst, planRows, actualRows float64

	for i := range groups {
		g := &groups[i]

		for _, f := range g.Findings {
			if f.Code != explain.RuleRowMisestimate {
				continue
			}

			n := g.Sample.NodeAt(f.Path)
			if n == nil || !relationIs(n, c.Schema, c.Table, ambiguous) {
				continue
			}

			if ratio := floatParam(f.Params, explain.ParamRatio); ratio > worst {
				worst = ratio
				planRows = floatParam(f.Params, explain.ParamPlanRows)
				actualRows = floatParam(f.Params, explain.ParamActualRows)
			}
		}
	}

	if worst == 0 {
		return Warning{}, false
	}

	return Warning{
		Code:  WarnStaleStatistics,
		Names: nil,
		Params: map[string]float64{
			ParamRatio:      worst,
			ParamPlanRows:   planRows,
			ParamActualRows: actualRows,
		},
	}, true
}

func floatParam(params map[string]any, key string) float64 {
	v, _ := params[key].(float64)

	return v
}
