package http

import (
	"strconv"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
)

// reportKey identifies a report row across sources. A queryid is derived from
// the parse tree, object OIDs included, so it is unique only within a database:
// pairing two snapshots by queryid alone would merge unrelated statements.
type reportKey struct {
	QueryID int64
	Datname string
}

func reportKeyOf(r dto.QueryReport) reportKey {
	return reportKey{QueryID: r.QueryID, Datname: r.Datname}
}

// resolveScope decides which slice of the instance an answer covers. Without a
// named database only the instance-wide reading is possible, which is also what
// callers written before the parameter existed expect.
func resolveScope(scope *string, database string) string {
	if database == "" || (scope != nil && *scope == dto.ScopeInstance) {
		return dto.ScopeInstance
	}

	return dto.ScopeDatabase
}

// compareScope decides how two sides are read together, or reports false when
// they cannot be. Mixing generations would not merely lose rows: with the
// database part of the key empty on one side only, nothing matches and every
// statement is reported as both removed and added — worse than a refusal.
func compareScope(attributedA, attributedB bool, scope *string, database string) (string, bool) {
	if attributedA != attributedB {
		return "", false
	}

	if !attributedA {
		return dto.ScopeInstance, true
	}

	return resolveScope(scope, database), true
}

// scopeReport narrows an instance-wide report to the requested scope and moves
// the matching share set into the *Pct fields the contract exposes. The
// repository always reads every database — a stored snapshot has to stay usable
// after the user switches database — so this is the only place the narrowing
// happens.
func scopeReport(rows []dto.QueryReport, scope, database string) []dto.QueryReport {
	out := make([]dto.QueryReport, 0, len(rows))

	for _, r := range rows {
		if scope == dto.ScopeInstance {
			r.RowsPct = r.RowsPctInstance
			r.CallsPct = r.CallsPctInstance
			r.TotalTimePct = r.TotalTimePctInstance
			r.IoTimePct = r.IoTimePctInstance
			r.CpuTimePct = r.CpuTimePctInstance
			r.SharedBlksDirtiedPct = r.SharedBlksDirtiedPctInstance
			r.SharedBlksWrittenPct = r.SharedBlksWrittenPctInstance
			r.WalBytesPct = r.WalBytesPctInstance
			r.TempBlksPct = r.TempBlksPctInstance
		} else if r.Datname != database {
			continue
		}

		out = append(out, r)
	}

	return out
}

func mapQueryReport(t dto.QueryReport) serverhttp.QueryReport {
	return serverhttp.QueryReport{
		QueryID:              strconv.FormatInt(t.QueryID, 10),
		Query:                t.Query,
		Datname:              shortcut.Ptr(t.Datname),
		Usernames:            usernamesPtr(t.Usernames),
		StddevExecTimeMs:     t.StddevExecTimeMs,
		StddevPlanTimeMs:     t.StddevPlanTimeMs,
		Rows:                 t.Rows,
		RowsPct:              t.RowsPct,
		Calls:                t.Calls,
		CallsPct:             t.CallsPct,
		TotalTimeMs:          t.TotalTimeMs,
		TotalTimePct:         t.TotalTimePct,
		ExecTimeMs:           t.ExecTimeMs,
		MinExecTimeMs:        t.MinExecTimeMs,
		MaxExecTimeMs:        t.MaxExecTimeMs,
		MeanExecTimeMs:       t.MeanExecTimeMs,
		PlanTimeMs:           t.PlanTimeMs,
		MinPlanTimeMs:        t.MinPlanTimeMs,
		MaxPlanTimeMs:        t.MaxPlanTimeMs,
		MeanPlanTimeMs:       t.MeanPlanTimeMs,
		IoTimeMs:             t.IoTimeMs,
		IoTimePct:            t.IoTimePct,
		CpuTimeMs:            t.CpuTimeMs,
		CpuTimePct:           t.CpuTimePct,
		CacheHitRatio:        t.CacheHitRatio,
		SharedBlksDirtiedPct: t.SharedBlksDirtiedPct,
		SharedBlksWrittenPct: t.SharedBlksWrittenPct,
		WalBytes:             t.WalBytes,
		WalBytesPct:          t.WalBytesPct,
		WalRecords:           t.WalRecords,
		WalFpi:               t.WalFpi,
		TempBlks:             t.TempBlks,
		TempBlksPct:          t.TempBlksPct,
	}
}

// usernamesPtr returns nil for an empty slice so the JSON field is rendered as null
// (consistent with other nullable arrays in the API).
func usernamesPtr(u []string) *[]string {
	if len(u) == 0 {
		return nil
	}

	return &u
}

func mapQueryReportMetrics(t dto.QueryReport) serverhttp.QueryReportMetrics {
	return serverhttp.QueryReportMetrics{
		Usernames:            usernamesPtr(t.Usernames),
		StddevExecTimeMs:     t.StddevExecTimeMs,
		StddevPlanTimeMs:     t.StddevPlanTimeMs,
		Rows:                 t.Rows,
		RowsPct:              t.RowsPct,
		Calls:                t.Calls,
		CallsPct:             t.CallsPct,
		TotalTimeMs:          t.TotalTimeMs,
		TotalTimePct:         t.TotalTimePct,
		ExecTimeMs:           t.ExecTimeMs,
		MinExecTimeMs:        t.MinExecTimeMs,
		MaxExecTimeMs:        t.MaxExecTimeMs,
		MeanExecTimeMs:       t.MeanExecTimeMs,
		PlanTimeMs:           t.PlanTimeMs,
		MinPlanTimeMs:        t.MinPlanTimeMs,
		MaxPlanTimeMs:        t.MaxPlanTimeMs,
		MeanPlanTimeMs:       t.MeanPlanTimeMs,
		IoTimeMs:             t.IoTimeMs,
		IoTimePct:            t.IoTimePct,
		CpuTimeMs:            t.CpuTimeMs,
		CpuTimePct:           t.CpuTimePct,
		CacheHitRatio:        t.CacheHitRatio,
		SharedBlksDirtiedPct: t.SharedBlksDirtiedPct,
		SharedBlksWrittenPct: t.SharedBlksWrittenPct,
		WalBytes:             t.WalBytes,
		WalBytesPct:          t.WalBytesPct,
		WalRecords:           t.WalRecords,
		WalFpi:               t.WalFpi,
		TempBlks:             t.TempBlks,
		TempBlksPct:          t.TempBlksPct,
	}
}
