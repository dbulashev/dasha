package http

import (
	"strconv"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
)

func mapQueryReport(t dto.QueryReport) serverhttp.QueryReport {
	return serverhttp.QueryReport{
		QueryID:              strconv.FormatInt(t.QueryID, 10),
		Query:                t.Query,
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
