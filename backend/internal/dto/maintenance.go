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
}

type MaintenanceTransactionIdDanger struct {
	Schema           string
	Table            string
	TransactionsLeft int64
}

type MaintenanceVacuumProgress struct {
	Pid   int32
	Phase string
}

type MaintenanceAutovacuumSummary struct {
	TablesDueVacuumOnly  int32
	TablesDueAnalyzeOnly int32
	TablesDueBoth        int32
	TablesTotal          int32
	RunningVacuums       int32
	RunningAnalyzes      int32
}
