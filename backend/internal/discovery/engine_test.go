package discovery

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

type fakeProvider struct {
	clusters []config.Cluster
	err      error
	calls    int
}

func (f *fakeProvider) Discover(context.Context) ([]config.Cluster, error) {
	f.calls++

	if f.err != nil {
		return nil, f.err
	}

	return f.clusters, nil
}

func (f *fakeProvider) Interval() time.Duration {
	return time.Minute
}

func newTestEngine(t *testing.T) (*Engine, config.Clusters) {
	t.Helper()

	clusters := config.NewClustersFromConfig(config.Config{}) //nolint:exhaustruct

	return NewEngine(nil, clusters, nil, zap.NewNop()), clusters
}

func clusterNames(t *testing.T, clusters config.Clusters) []string {
	t.Helper()

	all, err := clusters.Get(t.Context())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	names := make([]string, 0, len(all))
	for _, cl := range all {
		names = append(names, cl.Name.String())
	}

	return names
}

func TestDiscoverPublishesClusters(t *testing.T) {
	t.Parallel()

	engine, clusters := newTestEngine(t)
	provider := &fakeProvider{ //nolint:exhaustruct
		clusters: []config.Cluster{{Name: "prod"}}, //nolint:exhaustruct
	}

	engine.discover(t.Context(), "folder_a", provider)

	if got := clusterNames(t, clusters); len(got) != 1 || got[0] != "prod" {
		t.Fatalf("got %v, want [prod]", got)
	}
}

func TestDiscoverKeepsPreviousStateOnError(t *testing.T) {
	t.Parallel()

	engine, clusters := newTestEngine(t)
	provider := &fakeProvider{ //nolint:exhaustruct
		clusters: []config.Cluster{{Name: "prod"}}, //nolint:exhaustruct
	}

	engine.discover(t.Context(), "folder_a", provider)

	// A failed cycle must not wipe what the source published before.
	provider.err = errors.New("api unavailable")
	engine.discover(t.Context(), "folder_a", provider)

	if got := clusterNames(t, clusters); len(got) != 1 || got[0] != "prod" {
		t.Fatalf("got %v, want [prod]", got)
	}

	if provider.calls != 2 {
		t.Errorf("Discover called %d times, want 2", provider.calls)
	}
}

func TestNewProviderUnknownType(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	_, err := engine.newProvider("entry", config.DiscoveryEntry{Type: "redis-mdb"}, false) //nolint:exhaustruct
	if err == nil {
		t.Fatal("newProvider() error = nil, want unknown type error")
	}
}

func TestNewProviderYandexInvalidConfig(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	// No authorized_key: the entry must fail before any SDK is built.
	_, err := engine.newProvider("entry", config.DiscoveryEntry{
		Type:   discoveryTypeYandexMDB,
		Config: map[string]any{"folder_id": "b1g123"},
	}, false)
	if err == nil {
		t.Fatal("newProvider() error = nil, want a validation error")
	}
}

func TestNewProviderPostgres(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	provider, err := engine.newProvider("onprem", config.DiscoveryEntry{
		Type: discoveryTypePostgres,
		Config: map[string]any{
			"hosts": []any{"pg-01.local"},
			"user":  "dasha",
		},
	}, false)
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	if got, want := provider.Interval(), 5*time.Minute; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}
}

func TestPrefixByType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  map[string]config.DiscoveryEntry
		want map[string]bool
	}{
		{
			name: "one entry of each type",
			cfg: map[string]config.DiscoveryEntry{
				"prod":   {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
				"onprem": {Type: discoveryTypePostgres},  //nolint:exhaustruct
				"etcd":   {Type: discoveryTypePatroni},   //nolint:exhaustruct
			},
			want: map[string]bool{discoveryTypeYandexMDB: false, discoveryTypePatroni: false},
		},
		{
			name: "two folders, one patroni",
			cfg: map[string]config.DiscoveryEntry{
				"prod":    {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
				"staging": {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
				"etcd":    {Type: discoveryTypePatroni},   //nolint:exhaustruct
			},
			want: map[string]bool{discoveryTypeYandexMDB: true, discoveryTypePatroni: false},
		},
		{
			name: "two patroni, one folder",
			cfg: map[string]config.DiscoveryEntry{
				"prod":   {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
				"etcd":   {Type: discoveryTypePatroni},   //nolint:exhaustruct
				"consul": {Type: discoveryTypePatroni},   //nolint:exhaustruct
			},
			want: map[string]bool{discoveryTypeYandexMDB: false, discoveryTypePatroni: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := prefixByType(tt.cfg)

			for typ, want := range tt.want {
				if got[typ] != want {
					t.Errorf("prefixByType()[%q] = %v, want %v", typ, got[typ], want)
				}
			}

			if got[discoveryTypePostgres] {
				t.Errorf("prefixByType()[%q] = true, postgres names are never prefixed", discoveryTypePostgres)
			}
		})
	}
}

func TestNewProviderPatroni(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	// The etcd client connects lazily: an unreachable endpoint must not fail the entry.
	provider, err := engine.newProvider("etcd", config.DiscoveryEntry{
		Type: discoveryTypePatroni,
		Config: map[string]any{
			"dcs":   "etcd3",
			"etcd3": map[string]any{"endpoints": []any{"http://127.0.0.1:1"}},
			"user":  "dasha",
		},
	}, true)
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	if got, want := provider.Interval(), 5*time.Minute; got != want {
		t.Errorf("Interval() = %v, want %v", got, want)
	}

	closer, ok := provider.(io.Closer)
	if !ok {
		t.Fatal("patroni provider does not implement io.Closer")
	}

	if err := closer.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestNewProviderPatroniInvalidConfig(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	_, err := engine.newProvider("etcd", config.DiscoveryEntry{
		Type:   discoveryTypePatroni,
		Config: map[string]any{"user": "dasha"},
	}, false)
	if err == nil {
		t.Fatal("newProvider() error = nil, want a validation error")
	}
}

type closingProvider struct {
	fakeProvider

	closed chan struct{}
}

func (c *closingProvider) Close() error {
	close(c.closed)

	return nil
}

func TestRunLoopClosesProvider(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)
	provider := &closingProvider{closed: make(chan struct{})} //nolint:exhaustruct

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		engine.runLoop(ctx, "etcd", provider)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runLoop did not stop after cancel")
	}

	select {
	case <-provider.closed:
	default:
		t.Fatal("Close() was not called when the loop stopped")
	}
}

func TestStartWithoutEntries(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	if err := engine.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}
