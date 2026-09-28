package health

import (
	"math"

	"github.com/jackc/pgx/v5"
)

const (
	coldFreezeRatioLow    = 0.9
	coldFreezeRatioMedium = 1.0
)

type ColdMaintenanceTable struct {
	Schema          string
	Table           string
	SizeBytes       int64
	DeadRatio       float64 // percent
	NeverVacuumed   bool
	RelfrozenxidAge int64
	NoWritesDays    int
}

// ColdFacts lists cold tables autovacuum will not reach, worst first.
type ColdFacts struct {
	WindowDays int
	Tables     []ColdMaintenanceTable
	More       int
}

func (c *ColdFacts) context() map[string]any {
	objects := make([]map[string]any, len(c.Tables))
	for i, t := range c.Tables {
		objects[i] = map[string]any{
			"schema":           t.Schema,
			"table":            t.Table,
			"size_bytes":       t.SizeBytes,
			"dead_ratio":       math.Round(t.DeadRatio*10) / 10,
			"never_vacuumed":   t.NeverVacuumed,
			"relfrozenxid_age": t.RelfrozenxidAge,
			"no_writes_days":   t.NoWritesDays,
		}
	}

	return map[string]any{
		"window_days": c.WindowDays,
		"worst":       pgx.Identifier{c.Tables[0].Schema, c.Tables[0].Table}.Sanitize(),
		"objects":     objects,
		"more":        c.More,
	}
}

func coldFreezeSeverity(m RawMetrics) Severity {
	if m.ColdMaxRelfrozenxidAge >= xidFailsafeAge {
		return SeverityHigh
	}

	return severityFor(m.ColdFreezeRatio, math.Inf(1), coldFreezeRatioMedium, coldFreezeRatioLow)
}
