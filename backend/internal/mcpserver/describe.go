package mcpserver

import (
	"errors"
	"maps"
	"strconv"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const partitionFloor = 5

var (
	errObjectLocked = errors.New("dasha: table locked (423)")
	errQueryTimeout = errors.New("dasha: query timed out (504)")
)

const (
	bloatPartitionedParent = "partitioned_parent"
	bloatNotAHeap          = "not_a_heap"
	bloatLockTimeout       = "lock_timeout"
	bloatTimeout           = "timeout"
	bloatError             = "error"
)

// bloatHeapTypes are the TableType values pgstattuple_approx can read.
var bloatHeapTypes = map[string]bool{"table": true, "materialized_view": true, "toast_table": true}

type bloatUnavailable struct {
	Unavailable string `json:"unavailable"`
	Hint        string `json:"hint"`
	Detail      string `json:"detail,omitempty"`
}

// bloatSkipped answers for relations pgstattuple_approx cannot read, without
// calling it; nil when the table is unknown or is a heap.
func bloatSkipped(d *apiclient.TableDescribe) *bloatUnavailable {
	switch {
	case d == nil || bloatHeapTypes[d.TableType]:
		return nil
	case d.TableType == "partitioned_table":
		return &bloatUnavailable{ //nolint:exhaustruct
			Unavailable: bloatPartitionedParent,
			Hint:        "a partitioned table stores no rows itself; call describe_table on a partition",
		}
	default:
		return &bloatUnavailable{ //nolint:exhaustruct
			Unavailable: bloatNotAHeap,
			Hint:        "bloat is measured for tables and materialized views only, not a " + d.TableType,
		}
	}
}

func bloatFailure(err error) *bloatUnavailable {
	u := &bloatUnavailable{Unavailable: bloatError, Detail: err.Error()} //nolint:exhaustruct

	switch {
	case errors.Is(err, errObjectLocked):
		u.Unavailable = bloatLockTimeout
		u.Hint = "a lock held by another transaction blocked the scan; blocked_queries shows who holds it"
	case errors.Is(err, errQueryTimeout):
		u.Unavailable = bloatTimeout
		u.Hint = "pgstattuple_approx did not finish in time on this table"
	default:
		u.Hint = "pgstattuple_approx failed: the pgstattuple extension may be missing in this database or " +
			"not executable by the monitoring role; Dasha's log holds the cause"
	}

	return u
}

// tableSections is the describe_table result; partitions is the only section
// whose size grows with the table.
type tableSections map[string]any

func (s tableSections) shrink() (shapedResult, string, bool) {
	parts, _ := s["partitions"].([]apiclient.TableDescribePartition)

	n := max(len(parts)/2, partitionFloor)
	if n >= len(parts) {
		return nil, "the partitions section is at its floor and the other sections are fixed-size", false
	}

	next := maps.Clone(s)
	next["partitions"] = parts[:n]

	return next, "partitions=" + strconv.Itoa(n), true
}

func (s tableSections) note() *shapeNote {
	return nil
}
