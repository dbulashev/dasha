package discovery

import (
	"context"
	"errors"
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

func TestCountEntriesDrivesNamePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  map[string]config.DiscoveryEntry
		want int
	}{
		{
			name: "single folder alongside a postgres entry",
			cfg: map[string]config.DiscoveryEntry{
				"prod":   {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
				"onprem": {Type: discoveryTypePostgres},  //nolint:exhaustruct
			},
			// Adding a postgres entry must not start prefixing the folder's
			// cluster names: the names are the key to snapshots and history.
			want: 1,
		},
		{
			name: "two folders",
			cfg: map[string]config.DiscoveryEntry{
				"prod":    {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
				"staging": {Type: discoveryTypeYandexMDB}, //nolint:exhaustruct
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := countEntries(tt.cfg, discoveryTypeYandexMDB); got != tt.want {
				t.Errorf("countEntries() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStartWithoutEntries(t *testing.T) {
	t.Parallel()

	engine, _ := newTestEngine(t)

	if err := engine.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}
