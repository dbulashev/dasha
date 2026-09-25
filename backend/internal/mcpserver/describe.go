package mcpserver

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"sync"

	"github.com/dbulashev/dasha/gen/apiclient"
)

const partitionFloor = 5

var (
	errObjectLocked = errors.New("dasha: table locked (423)")
	errQueryTimeout = errors.New("dasha: query timed out (504)")
	errBloatFailed  = errors.New("dasha: bloat scan failed")
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

// bloatFailure classifies a failed bloat scan; nil for errors that belong in
// bloat_error (auth, rate limit, transport).
func describeTable(ctx context.Context, c *DashaClient, a describeTableArgs) tableSections {
	schema := a.Schema
	if schema == "" {
		schema = "public"
	}

	partitionLimit := a.Limit
	if partitionLimit <= 0 {
		partitionLimit = defaultPartitionLimit
	}

	var (
		wg               sync.WaitGroup
		d                *apiclient.TableDescribe
		bl               *apiclient.TableDescribeBloat
		pt               []apiclient.TableDescribePartition
		re, vs           any
		dErr, bErr, pErr error
		reErr, vsErr     error
	)

	wg.Go(func() {
		d, dErr = c.TableDescribe(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		if bloatSkipped(d) == nil {
			bl, bErr = c.TableDescribeBloat(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
		}
	})
	wg.Go(func() {
		pt, pErr = c.TableDescribePartitions(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table, partitionLimit)
	})
	wg.Go(func() {
		re, reErr = c.TableDescribeRowEstimate(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
	})
	wg.Go(func() {
		vs, vsErr = c.TableDescribeVacuumStats(ctx, a.Cluster, a.Instance, a.Database, schema, a.Table)
	})
	wg.Wait()

	out := tableSections{}
	section(out, "table", d, dErr)

	switch skip, u := bloatSkipped(d), bloatFailure(bErr); {
	case skip != nil:
		out["bloat"] = skip
	case u != nil && d != nil:
		out["bloat"] = u
	default:
		section(out, "bloat", bl, bErr)
	}

	section(out, "partitions", pt, pErr)
	section(out, "row_estimate", re, reErr)
	section(out, "vacuum_stats", vs, vsErr)

	return out
}

func bloatFailure(err error) *bloatUnavailable {
	u := &bloatUnavailable{Unavailable: bloatError} //nolint:exhaustruct

	switch {
	case err == nil:
		return nil
	case errors.Is(err, errObjectLocked):
		u.Unavailable = bloatLockTimeout
		u.Hint = "a lock held by another transaction blocked the scan; blocked_queries shows who holds it"
	case errors.Is(err, errQueryTimeout):
		u.Unavailable = bloatTimeout
		u.Hint = "pgstattuple_approx did not finish in time on this table"
	case errors.Is(err, errBloatFailed):
		u.Hint = "pgstattuple_approx failed: the pgstattuple extension may be missing in this database or " +
			"not executable by the monitoring role; Dasha's log holds the cause"
	default:
		return nil
	}

	u.Detail = err.Error()

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
