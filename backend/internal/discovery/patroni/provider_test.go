package patroni

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/pgdb"
)

var errRefused = errors.New("connection refused")

type fakeStore struct {
	kvs []kv
	err error
}

func (f *fakeStore) list(context.Context, string) ([]kv, error) {
	return f.kvs, f.err
}

func (f *fakeStore) Close() error {
	return nil
}

// fakePG answers with dbs for every host not in down and records the targets asked.
type fakePG struct {
	mu    sync.Mutex
	dbs   []string
	down  map[string]bool
	asked []pgdb.Target
}

func (f *fakePG) list(_ context.Context, t pgdb.Target) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, t)

	if f.down[t.Host] {
		return nil, errRefused
	}

	return f.dbs, nil
}

func (f *fakePG) askedHosts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, 0, len(f.asked))
	for _, t := range f.asked {
		out = append(out, t.Host)
	}

	f.asked = nil

	return out
}

func memberKV(scope, name, host string, port int) kv {
	return kv{
		Key:   fmt.Sprintf("/service/%s/members/%s", scope, name),
		Value: fmt.Appendf(nil, `{"conn_url":"postgres://%s:%d/postgres","role":"replica"}`, host, port),
	}
}

type testEnv struct {
	p     *Provider
	store *fakeStore
	pg    *fakePG
	logs  *observer.ObservedLogs
}

func newTestEnv(t *testing.T, extra map[string]any, prefixName bool, kvs ...kv) *testEnv {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)

	p, err := newProvider("dc1", etcdRaw(extra), prefixName, zap.New(core))
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	env := &testEnv{
		p:     p,
		store: &fakeStore{kvs: kvs, err: nil},
		pg:    &fakePG{dbs: []string{"app", "billing", "postgres"}, down: map[string]bool{}}, //nolint:exhaustruct
		logs:  logs,
	}

	p.store = env.store
	p.list = env.pg.list

	return env
}

func (e *testEnv) discover(t *testing.T) []config.Cluster {
	t.Helper()

	clusters, err := e.p.Discover(t.Context())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	return clusters
}

func byName(clusters []config.Cluster) map[string]config.Cluster {
	out := make(map[string]config.Cluster, len(clusters))
	for _, cl := range clusters {
		out[cl.Name.String()] = cl
	}

	return out
}

func dbNames(cl config.Cluster) []string {
	out := make([]string, 0, len(cl.Databases))
	for _, db := range cl.Databases {
		out = append(out, db.String())
	}

	return out
}

func hostNames(cl config.Cluster) []string {
	out := make([]string, 0, len(cl.Hosts))
	for _, h := range cl.Hosts {
		out = append(out, h.String())
	}

	return out
}

func TestDiscoverBuildsClusters(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, map[string]any{"exclude_db": "^postgres$"}, false,
		memberKV("alpha", "alpha-2", "10.0.0.2", 5432),
		memberKV("alpha", "alpha-1", "10.0.0.1", 5433),
		memberKV("beta", "beta-1", "10.0.1.1", 5432),
	)

	clusters := env.discover(t)

	if len(clusters) != 2 || clusters[0].Name != "alpha" || clusters[1].Name != "beta" {
		t.Fatalf("clusters = %v, want alpha and beta", clusters)
	}

	alpha := clusters[0]

	if want := []string{"10.0.0.1", "10.0.0.2"}; !slices.Equal(hostNames(alpha), want) {
		t.Errorf("alpha hosts = %v, want %v (by member name)", hostNames(alpha), want)
	}

	if m := alpha.HostMeta["10.0.0.1"]; m.Port != "5433" || m.Alias != "alpha-1" {
		t.Errorf("alpha-1 meta = %+v, want port 5433 and alias alpha-1", m)
	}

	if alpha.Port != "" || alpha.UserName != "dasha" || alpha.Password != "pw" {
		t.Errorf("alpha port/user/password = %q/%q/%q", alpha.Port, alpha.UserName, alpha.Password)
	}

	if want := []string{"app", "billing"}; !slices.Equal(dbNames(alpha), want) {
		t.Errorf("alpha databases = %v, want %v", dbNames(alpha), want)
	}

	if alpha.Source != config.SourcePatroni || alpha.ProviderID != "dc1" {
		t.Errorf("source/provider = %q/%q", alpha.Source, alpha.ProviderID)
	}

	wantLabels := map[string]string{
		"patroni.dcs":       "etcd3",
		"patroni.namespace": "/service",
		"patroni.scope":     "alpha",
		"bootstrap_db":      "postgres",
	}
	for k, v := range wantLabels {
		if alpha.Labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, alpha.Labels[k], v)
		}
	}

	if _, ok := alpha.Labels[config.LabelPatroniStandbyOf]; ok {
		t.Error("alpha is not a standby")
	}
}

func TestDiscoverPortFromConfig(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, map[string]any{"port": "6432"}, false,
		memberKV("alpha", "alpha-1", "10.0.0.1", 5433),
	)

	alpha := env.discover(t)[0]

	if alpha.Port != "6432" || alpha.HostMeta["10.0.0.1"].Port != "6432" {
		t.Errorf("port = %q, meta port = %q, want 6432 for both", alpha.Port, alpha.HostMeta["10.0.0.1"].Port)
	}

	if env.pg.asked[0].Port != "6432" {
		t.Errorf("listed on port %q, want 6432", env.pg.asked[0].Port)
	}
}

func TestDiscoverOverrideAndFilters(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, map[string]any{
		"exclude_scope": "^test-",
		"overrides": map[string]any{
			"beta": map[string]any{"user": "dasha_beta", "password": "b", "db": "^app$"},
		},
	}, true,
		memberKV("alpha", "alpha-1", "10.0.0.1", 5432),
		memberKV("beta", "beta-1", "10.0.1.1", 5432),
		memberKV("test-scratch", "t-1", "10.0.2.1", 5432),
	)

	got := byName(env.discover(t))

	if len(got) != 2 {
		t.Fatalf("clusters = %v, want dc1_alpha and dc1_beta", got)
	}

	beta, ok := got["dc1_beta"]
	if !ok {
		t.Fatalf("no dc1_beta in %v", got)
	}

	if beta.UserName != "dasha_beta" || beta.Password != "b" || !slices.Equal(dbNames(beta), []string{"app"}) {
		t.Errorf("beta = %s/%s %v, want dasha_beta/b [app]", beta.UserName, beta.Password, dbNames(beta))
	}

	if beta.Labels["patroni.scope"] != "beta" {
		t.Errorf("scope label = %q, want beta without the entry prefix", beta.Labels["patroni.scope"])
	}

	for _, target := range env.pg.asked {
		if target.Host == "10.0.1.1" && target.User != "dasha_beta" {
			t.Errorf("beta listed as %q, want dasha_beta", target.User)
		}
	}
}

func TestDiscoverKeepsDatabasesWhenPostgresIsDown(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false,
		memberKV("alpha", "alpha-1", "10.0.0.1", 5432),
		memberKV("beta", "beta-1", "10.0.1.1", 5432),
	)

	env.pg.down["10.0.1.1"] = true

	first := byName(env.discover(t))

	if len(first["beta"].Databases) != 0 || len(first["beta"].Hosts) != 1 {
		t.Errorf("beta never answered: databases = %v, want none, host kept", first["beta"].Databases)
	}

	env.pg.down = map[string]bool{"10.0.0.1": true}

	second := byName(env.discover(t))

	if want := []string{"app", "billing", "postgres"}; !slices.Equal(dbNames(second["alpha"]), want) {
		t.Errorf("alpha databases = %v, want the cached %v", dbNames(second["alpha"]), want)
	}

	if env.logs.FilterMessage("no patroni member answered, keeping the last database list").Len() != 1 {
		t.Error("cache fallback was not logged")
	}
}

func TestDiscoverStartsAtStickyMember(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false,
		memberKV("alpha", "alpha-1", "10.0.0.1", 5432),
		memberKV("alpha", "alpha-2", "10.0.0.2", 5432),
	)

	env.pg.down["10.0.0.1"] = true
	env.discover(t)

	if got := env.pg.askedHosts(); !slices.Equal(got, []string{"10.0.0.1", "10.0.0.2"}) {
		t.Fatalf("first cycle asked %v", got)
	}

	env.discover(t)

	if got := env.pg.askedHosts(); !slices.Equal(got, []string{"10.0.0.2"}) {
		t.Errorf("second cycle asked %v, want the member that answered last", got)
	}
}

func TestDiscoverDCSErrorKeepsState(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false, memberKV("alpha", "alpha-1", "10.0.0.1", 5432))
	env.discover(t)

	env.store.err = errRefused

	if _, err := env.p.Discover(t.Context()); !errors.Is(err, errRefused) {
		t.Fatalf("Discover() error = %v, want %v", err, errRefused)
	}

	if _, ok := env.p.state["alpha"]; !ok {
		t.Error("state lost after a DCS error")
	}
}

func TestDiscoverEmptyNamespace(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false, memberKV("alpha", "alpha-1", "10.0.0.1", 5432))
	env.discover(t)

	env.store.kvs = nil

	if clusters := env.discover(t); len(clusters) != 0 {
		t.Fatalf("clusters = %v, want none", clusters)
	}

	if env.logs.FilterMessage("patroni scopes changed").Len() != 1 {
		t.Error("scope removal was not logged")
	}
}

func TestDiscoverLogsChanges(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false,
		memberKV("alpha", "alpha-1", "10.0.0.1", 5432),
		memberKV("alpha", "alpha-2", "10.0.0.2", 5432),
	)
	env.discover(t)

	if env.logs.FilterMessage("patroni scopes discovered").Len() != 1 {
		t.Error("first cycle was not logged")
	}

	env.discover(t)

	env.store.kvs = []kv{
		memberKV("alpha", "alpha-1", "10.0.0.1", 5432),
		memberKV("gamma", "gamma-1", "10.0.3.1", 5432),
	}
	env.pg.dbs = []string{"app", "postgres"}

	env.discover(t)

	if env.logs.FilterMessage("patroni scopes changed").Len() != 1 {
		t.Error("added scope was not logged")
	}

	hosts := env.logs.FilterMessage("patroni scope hosts changed").All()
	if len(hosts) != 1 || hosts[0].ContextMap()["scope"] != "alpha" {
		t.Errorf("host change logs = %v, want one for alpha", hosts)
	}

	if env.logs.FilterMessage("patroni scope databases changed").Len() != 1 {
		t.Error("database change was not logged")
	}
}

func TestDiscoverSharedHost(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false,
		memberKV("lab", "lab-1", "10.0.0.1", 5432),
		memberKV("lab", "lab-2", "10.0.0.1", 5433),
		memberKV("lab", "lab-3", "10.0.0.2", 5432),
	)

	cl := env.discover(t)[0]

	want := []config.Host{"10.0.0.1:5432", "10.0.0.1:5433", "10.0.0.2"}
	if !slices.Equal(cl.Hosts, want) {
		t.Fatalf("hosts = %v, want %v", cl.Hosts, want)
	}

	if m := cl.HostMeta["10.0.0.1:5433"]; m.Address != "10.0.0.1" || m.Port != "5433" || m.Alias != "lab-2" {
		t.Errorf("meta = %+v, want 10.0.0.1:5433 lab-2", m)
	}

	if m := cl.HostMeta["10.0.0.2"]; m.Address != "10.0.0.2" || m.Port != "5432" {
		t.Errorf("meta = %+v, want 10.0.0.2:5432", m)
	}
}

func TestDiscoverAddressCollision(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, map[string]any{"port": "6432"}, false,
		memberKV("lab", "lab-1", "10.0.0.1", 5432),
		memberKV("lab", "lab-2", "10.0.0.1", 5433),
	)

	cl := env.discover(t)[0]
	env.discover(t)

	if len(cl.Hosts) != 1 || cl.Hosts[0] != "10.0.0.1" || cl.HostMeta["10.0.0.1"].Alias != "lab-1" {
		t.Errorf("hosts = %v meta = %v, want lab-1 only", cl.Hosts, cl.HostMeta)
	}

	if n := env.logs.FilterMessage("patroni member shares an address with another member, skipped").Len(); n != 1 {
		t.Errorf("collision logged %d times over two cycles, want 1", n)
	}
}

func TestDiscoverSkipsInvalidMembers(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, false,
		memberKV("alpha", "alpha-1", "10.0.0.1", 5432),
		kv{Key: "/service/alpha/members/alpha-2", Value: []byte(`{"role":"replica"}`)},
		kv{Key: "/service/broken/members/b-1", Value: []byte(`{}`)},
	)

	clusters := env.discover(t)

	if len(clusters) != 1 || len(clusters[0].Hosts) != 1 {
		t.Fatalf("clusters = %v, want alpha with one host", clusters)
	}

	if n := env.logs.FilterMessage("patroni member skipped, no usable address").Len(); n != 2 {
		t.Errorf("skipped members logged %d times, want 2", n)
	}
}

func TestDiscoverStandbyLabel(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, nil, true,
		memberKV("beta", "beta-1", "10.0.1.1", 5432),
		kv{Key: "/service/beta/initialize", Value: []byte("42")},
		memberKV("beta-standby", "bs-1", "10.0.2.1", 5432),
		kv{Key: "/service/beta-standby/initialize", Value: []byte("42")},
		kv{Key: "/service/beta-standby/config", Value: []byte(`{"standby_cluster":{"host":"beta-primary"}}`)},
	)

	got := byName(env.discover(t))

	if v := got["dc1_beta-standby"].Labels[config.LabelPatroniStandbyOf]; v != "dc1_beta" {
		t.Errorf("standby_of = %q, want dc1_beta", v)
	}
}

func TestDiscoverReportsUnmatchedOverrideOnce(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, map[string]any{
		"overrides": map[string]any{"ghost": map[string]any{"user": "x"}},
	}, false, memberKV("alpha", "alpha-1", "10.0.0.1", 5432))

	env.discover(t)
	env.discover(t)

	if n := env.logs.FilterMessage("override has no matching scope").Len(); n != 1 {
		t.Errorf("unmatched override logged %d times, want 1", n)
	}
}
