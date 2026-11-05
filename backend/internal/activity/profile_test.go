package activity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func active(pid int32, db, waitType, event, key string) Session {
	return Session{
		PID: pid, BackendType: clientBackend, State: "active", Database: db, User: "app", Application: "api",
		WaitType: waitType, WaitEvent: event, Key: key, KeyKind: KeyKindQueryID, Text: "text of " + key,
	}
}

func profileSamples() []Sample {
	t0 := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	return []Sample{
		{At: t0, Sessions: []Session{
			active(1, "shop", "", "", "q:1"),
			active(2, "shop", "Lock", "transactionid", "q:2"),
			{PID: 9, BackendType: "autovacuum worker", WaitType: "IO", WaitEvent: "DataFileRead"},
		}},
		{At: t0.Add(time.Second), Truncated: true, Sessions: []Session{
			active(1, "shop", "IO", "DataFileRead", "q:1"),
			active(3, "crm", "", "", "q:1"),
			active(2, "shop", "Lock", "transactionid", "q:2"),
			{PID: 4, BackendType: clientBackend, State: "idle in transaction", WaitType: "Client", WaitEvent: "ClientRead", Database: "shop"},
		}},
	}
}

func TestAggregate(t *testing.T) {
	t.Parallel()

	p := Aggregate(profileSamples(), Filter{}, 10)

	assert.Equal(t, 2, p.Samples)
	assert.Equal(t, 1, p.Truncated)
	assert.Equal(t, time.Second, p.To.Sub(p.From))
	assert.InDelta(t, 3.0, p.AAS, 1e-9, "6 counted sessions over 2 samples, background excluded")

	byClass := map[string]Share{}
	for _, s := range p.ByClass {
		byClass[s.Value] = s
	}

	assert.InDelta(t, 1.0, byClass[ClassLock].AAS, 1e-9)
	assert.InDelta(t, 1.0, byClass[ClassCPU].AAS, 1e-9)
	assert.InDelta(t, 0.5, byClass[ClassIO].AAS, 1e-9)
	assert.InDelta(t, 0.5, byClass[ClassClientIdleTx].AAS, 1e-9)
	assert.NotContains(t, byClass, ClassOther)

	require.NotEmpty(t, p.TopQueries)
	q1 := p.TopQueries[0]
	assert.Equal(t, "q:1", q1.Key)
	assert.InDelta(t, 1.5, q1.AAS, 1e-9)
	assert.InDelta(t, 50.0, q1.SharePct, 1e-9)
	assert.Equal(t, 2, q1.DistinctPIDs)
	assert.InDelta(t, 100.0*2/3, q1.ByClass[ClassCPU], 1e-9)
	assert.Equal(t, "text of q:1", q1.Text)

	require.NotEmpty(t, p.ByDatabase)
	assert.Equal(t, "shop", p.ByDatabase[0].Value)
	assert.InDelta(t, 2.5, p.ByDatabase[0].AAS, 1e-9)

	require.Len(t, p.TopEvents, 4)
	assert.Equal(t, ClassCPU, p.TopEvents[0].Class, "ties on AAS are ordered by class")
	assert.Equal(t, ClassLock, p.TopEvents[1].Class)
	assert.Equal(t, "transactionid", p.TopEvents[1].WaitEvent)
	assert.InDelta(t, 1.0, p.TopEvents[1].AAS, 1e-9)
	assert.InDelta(t, 100.0/3, p.TopEvents[1].SharePct, 1e-9)
}

func TestAggregateBackgroundHasNoQuery(t *testing.T) {
	t.Parallel()

	p := Aggregate(profileSamples(), Filter{IncludeBackground: true}, 10)

	assert.InDelta(t, 3.5, p.AAS, 1e-9)

	var queriesAAS float64
	for _, q := range p.TopQueries {
		queriesAAS += q.AAS
	}

	assert.InDelta(t, 3.0, queriesAAS, 1e-9, "the autovacuum worker is not a statement")

	byBackend := map[string]float64{}
	for _, s := range p.ByBackend {
		byBackend[s.Value] = s.AAS
	}

	assert.InDelta(t, 3.0, byBackend[clientBackend], 1e-9)
	assert.InDelta(t, 0.5, byBackend["autovacuum worker"], 1e-9)
}

func TestAggregateFilters(t *testing.T) {
	t.Parallel()

	p := Aggregate(profileSamples(), Filter{Database: "shop", Class: ClassLock}, 10)
	assert.InDelta(t, 1.0, p.AAS, 1e-9)
	require.Len(t, p.TopQueries, 1)
	assert.Equal(t, "q:2", p.TopQueries[0].Key)

	bg := Aggregate(profileSamples(), Filter{IncludeBackground: true}, 10)
	assert.InDelta(t, 3.5, bg.AAS, 1e-9)
}

func TestAggregateTop(t *testing.T) {
	t.Parallel()

	p := Aggregate(profileSamples(), Filter{}, 1)
	assert.Len(t, p.TopQueries, 1)
	assert.Len(t, p.TopEvents, 1)
	assert.Len(t, p.ByDatabase, 1)
	assert.Greater(t, len(p.ByClass), 1, "by_class is never cut")
}

func TestAggregateEmpty(t *testing.T) {
	t.Parallel()

	p := Aggregate(nil, Filter{}, 10)
	assert.Zero(t, p.Samples)
	assert.Zero(t, p.AAS)
}

func TestAggregateParallelWorkersCreditTheLeaderQuery(t *testing.T) {
	t.Parallel()

	worker := active(11, "shop", "", "", "q:7")
	worker.BackendType = parallelWorker

	samples := []Sample{{At: time.Now(), Sessions: []Session{active(10, "shop", "IPC", "ExecuteGather", "q:7"), worker}}}

	p := Aggregate(samples, Filter{}, 10)

	assert.InDelta(t, 2.0, p.AAS, 1e-9)
	require.Len(t, p.TopQueries, 1)
	assert.Equal(t, "q:7", p.TopQueries[0].Key)
	assert.InDelta(t, 2.0, p.TopQueries[0].AAS, 1e-9)
	assert.Equal(t, 2, p.TopQueries[0].DistinctPIDs)
}
