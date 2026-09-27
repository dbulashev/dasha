package hotobjects

import "time"

type ColdStatus string

const (
	ColdAvailable ColdStatus = "available"
	ColdNoStorage ColdStatus = "no_storage"
	ColdDisabled  ColdStatus = "disabled"   // hot_enabled = false
	ColdStale     ColdStatus = "stale"      // no snapshot within ColdMaxAge
	ColdWarmingUp ColdStatus = "warming_up" // observed_since too recent
	ColdStandby   ColdStatus = "standby"
	ColdError     ColdStatus = "error"
)

// ColdMaxAge bounds how old the latest snapshot may be for coldness to count.
const ColdMaxAge = 48 * time.Hour

type ColdTable struct {
	Schema     string
	Table      string
	QuietSince time.Time
	Writes     int64 // n_tup_ins + n_tup_upd + n_tup_del at the anchor
}

type ColdSet struct {
	Database      string
	Status        ColdStatus
	CapturedAt    time.Time
	ObservedSince *time.Time
	Tables        []ColdTable
}

// ColdSets is one instance's cold tables per database. A non-empty Status
// applies to every database and leaves ByDatabase empty.
type ColdSets struct {
	Status     ColdStatus
	WindowDays int
	ByDatabase map[string]ColdSet
}

// For returns the database's set; a database without a fresh snapshot is stale.
func (c ColdSets) For(database string) ColdSet {
	if c.Status != "" {
		return ColdSet{Database: database, Status: c.Status} //nolint:exhaustruct
	}

	if set, ok := c.ByDatabase[database]; ok {
		return set
	}

	return ColdSet{Database: database, Status: ColdStale} //nolint:exhaustruct
}

// ColdStatusOf classifies a fresh snapshot of one database on one host.
func ColdStatusOf(capturedAt time.Time, observedSince *time.Time, window time.Duration) ColdStatus {
	switch {
	case observedSince == nil:
		return ColdStandby
	case observedSince.After(capturedAt.Add(-window)):
		return ColdWarmingUp
	default:
		return ColdAvailable
	}
}
