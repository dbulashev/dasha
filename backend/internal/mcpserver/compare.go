package mcpserver

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const (
	compareDefaultLimit = 20
	compareMaxLimit     = 100
	compareFloorLimit   = 5
	compareQueryBytes   = 200

	// verdictChangePct is the growth of calls or of the mean time per call that
	// names it as the cause.
	verdictChangePct = 20.0

	compareNext = "query_report(queryid) for the full text and both sides' metrics"
)

const (
	verdictAdded        = "added"
	verdictRemoved      = "removed"
	verdictSlowerMore   = "slower_and_more_calls"
	verdictSlower       = "slower"
	verdictMoreCalls    = "more_calls"
	verdictLessLoad     = "less_load"
	verdictStable       = "stable"
	compareSortExecTime = "exec_time"
)

var compareSorts = map[string]func(compareDelta) float64{
	compareSortExecTime: func(d compareDelta) float64 { return d.ExecTimeMs },
	"calls":             func(d compareDelta) float64 { return float64(d.Calls) },
	"rows":              func(d compareDelta) float64 { return float64(d.Rows) },
	"io_time":           func(d compareDelta) float64 { return d.IoTimeMs },
}

// compareDelta is B minus A; a side missing from one snapshot counts as zero.
type compareDelta struct {
	Calls          int64   `json:"calls"`
	ExecTimeMs     float64 `json:"exec_time_ms"`
	MeanExecTimeMs float64 `json:"mean_exec_time_ms"`
	Rows           int64   `json:"rows"`
	IoTimeMs       float64 `json:"io_time_ms"`
}

// compareChange is (B − A) / A in percent, absent where A is zero.
type compareChange struct {
	Calls          *float64 `json:"calls,omitempty"`
	ExecTimeMs     *float64 `json:"exec_time_ms,omitempty"`
	MeanExecTimeMs *float64 `json:"mean_exec_time_ms,omitempty"`
}

type compareEntry struct {
	QueryID    string         `json:"queryid"`
	Datname    string         `json:"datname,omitempty"`
	QueryTrunc string         `json:"query_trunc"`
	QueryLen   int            `json:"query_len"`
	Delta      compareDelta   `json:"delta"`
	ChangePct  *compareChange `json:"change_pct,omitempty"`
	Verdict    string         `json:"verdict"`
}

type compareSummary struct {
	Compared int    `json:"compared"`
	Returned int    `json:"returned"`
	Added    int    `json:"added"`
	Removed  int    `json:"removed"`
	Sort     string `json:"sort"`
}

type compareResult struct {
	Summary compareSummary `json:"summary"`
	Queries []compareEntry `json:"queries"`
	Next    string         `json:"next"`

	ranked []compareEntry
}

func queryCompare(ctx context.Context, c *DashaClient, a queryCompareArgs) (any, error) {
	var b *string
	if a.SnapshotB != "" {
		b = &a.SnapshotB
	}

	items, err := c.QueryCompare(ctx, a.Cluster, a.Instance, a.Database, a.Scope, a.SnapshotA, b, a.ExcludeUsers)
	if err != nil {
		return nil, err
	}

	return buildCompare(items, cmp.Or(a.Sort, compareSortExecTime), cmp.Or(a.Limit, compareDefaultLimit)), nil
}

func buildCompare(items []apiclient.QueryCompareItem, sort string, limit int) *compareResult {
	key := compareSorts[sort]
	ranked := make([]compareEntry, 0, len(items))
	sum := compareSummary{Compared: len(items), Sort: sort} //nolint:exhaustruct

	for _, it := range items {
		e := compareItem(it)

		switch e.Verdict {
		case verdictAdded:
			sum.Added++
		case verdictRemoved:
			sum.Removed++
		}

		ranked = append(ranked, e)
	}

	slices.SortStableFunc(ranked, func(x, y compareEntry) int {
		return cmp.Or(cmp.Compare(key(y.Delta), key(x.Delta)), cmp.Compare(x.QueryID, y.QueryID))
	})

	r := &compareResult{Summary: sum, Next: compareNext, ranked: ranked} //nolint:exhaustruct
	r.take(limit)

	return r
}

func (r *compareResult) take(limit int) {
	r.Queries = r.ranked[:min(limit, len(r.ranked))]
	r.Summary.Returned = len(r.Queries)
}

func compareItem(it apiclient.QueryCompareItem) compareEntry {
	left, right := sideOf(it.Left), sideOf(it.Right)

	e := compareEntry{ //nolint:exhaustruct
		QueryID:    it.QueryID,
		QueryTrunc: truncQuery(it.Query),
		QueryLen:   len(it.Query),
		Delta: compareDelta{
			Calls:          right.calls - left.calls,
			ExecTimeMs:     right.exec - left.exec,
			MeanExecTimeMs: right.mean - left.mean,
			Rows:           right.rows - left.rows,
			IoTimeMs:       right.io - left.io,
		},
	}

	if it.Datname != nil {
		e.Datname = *it.Datname
	}

	switch {
	case it.Left == nil:
		e.Verdict = verdictAdded
	case it.Right == nil:
		e.Verdict = verdictRemoved
	default:
		e.ChangePct = &compareChange{
			Calls:          pctChange(float64(left.calls), float64(right.calls)),
			ExecTimeMs:     pctChange(left.exec, right.exec),
			MeanExecTimeMs: pctChange(left.mean, right.mean),
		}
		e.Verdict = verdictOf(e.Delta, e.ChangePct)
	}

	return e
}

func verdictOf(d compareDelta, c *compareChange) string {
	grew := func(p *float64, delta float64) bool {
		if p == nil {
			return delta > 0
		}

		return *p >= verdictChangePct
	}

	slower := grew(c.MeanExecTimeMs, d.MeanExecTimeMs)
	more := grew(c.Calls, float64(d.Calls))

	switch {
	case slower && more:
		return verdictSlowerMore
	case slower:
		return verdictSlower
	case more:
		return verdictMoreCalls
	case d.ExecTimeMs < 0:
		return verdictLessLoad
	default:
		return verdictStable
	}
}

type compareSide struct {
	calls, rows    int64
	exec, mean, io float64
}

func sideOf(m *apiclient.QueryReportMetrics) compareSide {
	if m == nil {
		return compareSide{} //nolint:exhaustruct
	}

	deref := func(p *float64) float64 {
		if p == nil {
			return 0
		}

		return *p
	}

	derefInt := func(p *int64) int64 {
		if p == nil {
			return 0
		}

		return *p
	}

	return compareSide{
		calls: derefInt(m.Calls),
		rows:  derefInt(m.Rows),
		exec:  deref(m.ExecTimeMs),
		mean:  deref(m.MeanExecTimeMs),
		io:    deref(m.IoTimeMs),
	}
}

func pctChange(a, b float64) *float64 {
	if a == 0 {
		return nil
	}

	v := round1((b - a) / a * 100)

	return &v
}

func truncQuery(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) <= compareQueryBytes {
		return q
	}

	cut := compareQueryBytes
	for cut > 0 && !utf8.RuneStart(q[cut]) {
		cut--
	}

	return q[:cut]
}

func (r *compareResult) shrink() (shapedResult, string, bool) {
	n := max(len(r.Queries)/2, compareFloorLimit)
	if n >= len(r.Queries) {
		return nil, "a smaller limit, scope='database', or exclude_users", false
	}

	next := *r
	next.take(n)

	return &next, "limit=" + strconv.Itoa(n), true
}

func (r *compareResult) note() *shapeNote {
	n := &shapeNote{ //nolint:exhaustruct
		Reason: shapeDefaultView,
		Folded: "query text clipped to " + strconv.Itoa(compareQueryBytes) + " bytes; absolute metrics of both sides",
		Total:  r.Summary.Compared,
		Full:   "query_report(queryid)",
	}

	if r.Summary.Returned < r.Summary.Compared {
		n.Folded = "ranked tail by " + r.Summary.Sort + " delta; " + n.Folded
		n.Full = "limit=" + strconv.Itoa(min(r.Summary.Compared, compareMaxLimit)) + "; " + n.Full
	}

	return n
}
