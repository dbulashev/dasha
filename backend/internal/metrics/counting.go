package metrics

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"sync"
	"time"
)

// QueryStats counts the datasource requests issued under a context carrying it.
type QueryStats struct {
	mu      sync.Mutex
	instant int
	rng     int
	byCode  map[int]int
}

// QueryCounts is a snapshot of QueryStats. Code 0 is a request that got no HTTP answer.
type QueryCounts struct {
	Instant int
	Range   int
	ByCode  map[int]int
}

type queryStatsKey struct{}

// WithQueryStats makes every datasource request under ctx count into st.
func WithQueryStats(ctx context.Context, st *QueryStats) context.Context {
	return context.WithValue(ctx, queryStatsKey{}, st)
}

func (st *QueryStats) Counts() QueryCounts {
	st.mu.Lock()
	defer st.mu.Unlock()

	return QueryCounts{Instant: st.instant, Range: st.rng, ByCode: maps.Clone(st.byCode)}
}

func (st *QueryStats) record(isRange bool, err error) {
	code := http.StatusOK

	if err != nil {
		code = 0

		var se *StatusError
		if errors.As(err, &se) {
			code = se.Code
		}
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	if isRange {
		st.rng++
	} else {
		st.instant++
	}

	if st.byCode == nil {
		st.byCode = make(map[int]int)
	}

	st.byCode[code]++
}

type countingClient struct {
	inner DatasourceClient
}

func (c countingClient) QueryInstant(ctx context.Context, expr string, at time.Time) ([]Sample, error) {
	out, err := c.inner.QueryInstant(ctx, expr, at)
	if st, ok := ctx.Value(queryStatsKey{}).(*QueryStats); ok {
		st.record(false, err)
	}

	return out, err
}

func (c countingClient) QueryRange(ctx context.Context, expr string, r Range) ([]Series, error) {
	out, err := c.inner.QueryRange(ctx, expr, r)
	if st, ok := ctx.Value(queryStatsKey{}).(*QueryStats); ok {
		st.record(true, err)
	}

	return out, err
}
