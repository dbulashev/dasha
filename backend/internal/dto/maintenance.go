package dto

import "time"

type MaintenanceAutovacuumFreezeMaxAge struct {
	AutovacuumFreezeMaxAge int64
}

type MaintenanceInfo struct {
	Schema          string
	Table           string
	LastVacuum      *time.Time
	LastAutovacuum  *time.Time
	LastAnalyze     *time.Time
	LastAutoanalyze *time.Time
	DeadRows        int64
	LiveRows        int64
	ColdIdx         *int // 1-based position in the ColdArgs; nil unless cold
}

type MaintenanceTransactionIdDanger struct {
	Schema           string
	Table            string
	TransactionsLeft int64
}

type MaintenanceVacuumProgress struct {
	Pid       int32
	Phase     string
	StartedBy *string
	Mode      *string
}

type MaintenanceAutovacuumSummary struct {
	TablesDueVacuumOnly  int32
	TablesDueAnalyzeOnly int32
	TablesDueBoth        int32
	TablesTotal          int32
	RunningVacuums       int32
	RunningAnalyzes      int32
	// PG 19+: running processes split by pg_stat_progress_*.started_by.
	RunningVacuumsManual      *int32
	RunningVacuumsAutovacuum  *int32
	RunningVacuumsWraparound  *int32
	RunningAnalyzesManual     *int32
	RunningAnalyzesAutovacuum *int32
	Source                    AutovacuumSummarySource
}

type AutovacuumSummarySource string

const (
	// AutovacuumSummarySourceScores counts tables by pg_stat_autovacuum_scores,
	// system catalogs included.
	AutovacuumSummarySourceScores AutovacuumSummarySource = "scores"
	// AutovacuumSummarySourceFormula re-derives autovacuum's trigger formula
	// over pg_stat_user_tables.
	AutovacuumSummarySourceFormula AutovacuumSummarySource = "formula"
)

type MaintenanceAutovacuumQueue struct {
	Total int32
	Rows  []MaintenanceAutovacuumQueueRow
}

type MaintenanceAutovacuumQueueRow struct {
	Schema            string
	Table             string
	Score             float64
	XidScore          float64
	MxidScore         float64
	VacuumScore       float64
	VacuumInsertScore float64
	AnalyzeScore      float64
	DoVacuum          bool
	DoAnalyze         bool
	ForWraparound     bool
	// Dominant names the component with the largest value; nil when every
	// component is zero.
	Dominant *AutovacuumScoreComponent
}

type AutovacuumScoreComponent string

// Ordered for tie-breaks: wraparound reasons win over bloat and statistics.
const (
	AutovacuumScoreXid          AutovacuumScoreComponent = "xid"
	AutovacuumScoreMxid         AutovacuumScoreComponent = "mxid"
	AutovacuumScoreVacuum       AutovacuumScoreComponent = "vacuum"
	AutovacuumScoreVacuumInsert AutovacuumScoreComponent = "vacuum_insert"
	AutovacuumScoreAnalyze      AutovacuumScoreComponent = "analyze"
)

// DominantComponent picks the component with the largest value; ties go to the
// one listed first in the constants above.
func (r MaintenanceAutovacuumQueueRow) DominantComponent() *AutovacuumScoreComponent {
	components := [...]struct {
		name  AutovacuumScoreComponent
		value float64
	}{
		{AutovacuumScoreXid, r.XidScore},
		{AutovacuumScoreMxid, r.MxidScore},
		{AutovacuumScoreVacuum, r.VacuumScore},
		{AutovacuumScoreVacuumInsert, r.VacuumInsertScore},
		{AutovacuumScoreAnalyze, r.AnalyzeScore},
	}

	best := -1

	for i, c := range components {
		if c.value > 0 && (best < 0 || c.value > components[best].value) {
			best = i
		}
	}

	if best < 0 {
		return nil
	}

	return &components[best].name
}
