package piicatalog

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
)

type fakeRepo struct {
	mu       sync.Mutex
	replicas map[string]bool
	infoErr  map[string]error
	// keyed by host + "/" + database
	catalogErr map[string]error
	panicOn    map[string]bool
	attempts   []string
}

func (f *fakeRepo) GetInstanceInfo(_ context.Context, _, instance string) (dto.InstanceInfo, error) {
	if err := f.infoErr[instance]; err != nil {
		return dto.InstanceInfo{}, err
	}

	return dto.InstanceInfo{InRecovery: f.replicas[instance], VersionNum: 170000}, nil
}

func (f *fakeRepo) GetColumnCatalog(
	_ context.Context, _, instance, database string, exclude []string, _ time.Duration,
) ([]Pair, error) {
	key := instance + "/" + database

	f.mu.Lock()
	f.attempts = append(f.attempts, key)
	f.mu.Unlock()

	if exclude == nil {
		return nil, errors.New("nil exclude list")
	}

	if f.panicOn[key] {
		panic("boom")
	}

	if err := f.catalogErr[key]; err != nil {
		return nil, err
	}

	return []Pair{{Name: "id"}, {Name: database}}, nil
}

type fakeStore struct {
	mu         sync.Mutex
	status     map[Key]Status
	applied    map[Key]Snapshot
	failures   map[Key]string
	retentions int
}

func newFakeStore() *fakeStore {
	return &fakeStore{status: map[Key]Status{}, applied: map[Key]Snapshot{}, failures: map[Key]string{}}
}

func (f *fakeStore) ListPIIStatus(context.Context) (map[Key]Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make(map[Key]Status, len(f.status))
	for k, v := range f.status {
		out[k] = v
	}

	return out, nil
}

func (f *fakeStore) ApplyPIISnapshot(_ context.Context, k Key, snap Snapshot) (ApplyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	at := snap.AttemptAt
	f.status[k] = Status{LastAttemptAt: &at, LastSuccessAt: &at}
	f.applied[k] = snap
	delete(f.failures, k)

	return ApplyResult{VersionID: 1, Added: len(snap.Pairs)}, nil
}

func (f *fakeStore) RecordPIIFailure(_ context.Context, k Key, at time.Time, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	st := f.status[k]
	st.LastAttemptAt = &at
	st.LastError = &msg
	f.status[k] = st
	f.failures[k] = msg

	return nil
}

func (f *fakeStore) PIIRetention(context.Context, time.Time, time.Time) (RetentionStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.retentions++

	return RetentionStats{}, nil
}

func (f *fakeStore) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.applied = map[Key]Snapshot{}
	f.failures = map[Key]string{}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestRunner(t *testing.T, cls []config.Cluster, repo *fakeRepo, store *fakeStore, c *clock) *Runner {
	t.Helper()

	cfg := config.PIICatalogConfig{Enabled: true}.WithDefaults()

	r, err := NewRunner(cfg, config.NewClustersFromConfig(config.Config{Clusters: cls}), repo, store, zap.NewNop())
	require.NoError(t, err)

	r.clock = c.now

	return r
}

func TestRunner_ScheduleFromPersistedAttempt(t *testing.T) {
	cls := []config.Cluster{{Name: "c1", Hosts: []config.Host{"h1"}, Databases: []config.Database{"app", "app"}}}
	store := newFakeStore()
	c := &clock{t: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}
	r := newTestRunner(t, cls, &fakeRepo{}, store, c)
	k := Key{Cluster: "c1", Database: "app"}

	r.runOnce(t.Context())
	require.Contains(t, store.applied, k, "a database without status is collected on the first tick")
	assert.Equal(t, "h1", store.applied[k].SourceInstance)
	assert.Equal(t, 170000, store.applied[k].ServerVersion)

	store.reset()
	c.t = c.t.Add(time.Minute)
	r.runOnce(t.Context())
	assert.Empty(t, store.applied)

	// A fresh runner (restart) reads the same persisted attempt.
	store.reset()
	r = newTestRunner(t, cls, &fakeRepo{}, store, c)
	r.runOnce(t.Context())
	assert.Empty(t, store.applied)

	c.t = time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	r.runOnce(t.Context())
	assert.Contains(t, store.applied, k)
}

func TestRunner_ReplicaFirstWithFallback(t *testing.T) {
	cls := []config.Cluster{{Name: "c1", Hosts: []config.Host{"unknown", "primary", "replica"}, Databases: []config.Database{"app"}}}
	repo := &fakeRepo{
		replicas:   map[string]bool{"replica": true},
		infoErr:    map[string]error{"unknown": errors.New("down")},
		catalogErr: map[string]error{"replica/app": errors.New("canceling statement due to conflict with recovery")},
	}
	store := newFakeStore()
	r := newTestRunner(t, cls, repo, store, &clock{t: time.Now()})

	r.runOnce(t.Context())

	assert.Equal(t, []string{"replica/app", "primary/app"}, repo.attempts)
	assert.Equal(t, "primary", store.applied[Key{Cluster: "c1", Database: "app"}].SourceInstance)
}

func TestRunner_AllHostsFailRecordedOthersCollected(t *testing.T) {
	cls := []config.Cluster{
		{Name: "c1", Hosts: []config.Host{"a", "b"}, Databases: []config.Database{"app"}},
		{Name: "c2", Hosts: []config.Host{"x"}, Databases: []config.Database{"app"}},
	}
	repo := &fakeRepo{catalogErr: map[string]error{
		"a/app": errors.New("connection refused"),
		"b/app": errors.New("timeout"),
	}}
	store := newFakeStore()
	r := newTestRunner(t, cls, repo, store, &clock{t: time.Now()})

	r.runOnce(t.Context())

	assert.Equal(t, "a: connection refused; b: timeout", store.failures[Key{Cluster: "c1", Database: "app"}])
	assert.Contains(t, store.applied, Key{Cluster: "c2", Database: "app"})
}

func TestRunner_PanicInOneDatabase(t *testing.T) {
	cls := []config.Cluster{{Name: "c1", Hosts: []config.Host{"h"}, Databases: []config.Database{"bad", "good"}}}
	repo := &fakeRepo{panicOn: map[string]bool{"h/bad": true}}
	store := newFakeStore()
	r := newTestRunner(t, cls, repo, store, &clock{t: time.Now()})

	r.runOnce(t.Context())

	assert.Equal(t, "internal error", store.failures[Key{Cluster: "c1", Database: "bad"}])
	assert.Contains(t, store.applied, Key{Cluster: "c1", Database: "good"})
}

func TestRunner_RetentionOncePerDay(t *testing.T) {
	store := newFakeStore()
	c := &clock{t: time.Now()}
	r := newTestRunner(t, nil, &fakeRepo{}, store, c)

	r.runOnce(t.Context())
	c.t = c.t.Add(time.Hour)
	r.runOnce(t.Context())
	assert.Equal(t, 1, store.retentions)

	c.t = c.t.Add(retentionInterval)
	r.runOnce(t.Context())
	assert.Equal(t, 2, store.retentions)
}

func TestRunner_CancelledContextRecordsNothing(t *testing.T) {
	cls := []config.Cluster{{Name: "c1", Hosts: []config.Host{"h"}, Databases: []config.Database{"app"}}}
	store := newFakeStore()
	r := newTestRunner(t, cls, &fakeRepo{}, store, &clock{t: time.Now()})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.runOnce(ctx)

	assert.Empty(t, store.applied)
	assert.Empty(t, store.failures)
	assert.Zero(t, store.retentions)
}
