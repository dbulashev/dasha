package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/discovery/pgdb"
)

func newTestProvider(t *testing.T, raw map[string]any) *Provider {
	t.Helper()

	raw["hosts"] = []any{"pg-01.local", "pg-02.local"}
	raw["user"] = "dasha"

	p, err := NewProvider("onprem_prod", raw, zap.NewNop())
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	return p
}

func TestNewProviderValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr error
	}{
		{
			name:    "no hosts",
			raw:     map[string]any{"user": "dasha"},
			wantErr: errHostsRequired,
		},
		{
			name:    "no user",
			raw:     map[string]any{"hosts": []any{"pg-01.local"}},
			wantErr: errUserRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewProvider("entry", tt.raw, zap.NewNop())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewProvider() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewProviderInvalidFilter(t *testing.T) {
	t.Parallel()

	_, err := NewProvider("entry", map[string]any{
		"hosts":      []any{"pg-01.local"},
		"user":       "dasha",
		"exclude_db": "[",
	}, zap.NewNop())

	if err == nil || !strings.Contains(err.Error(), "compile filter") {
		t.Fatalf("NewProvider() error = %v, want a filter compile error", err)
	}
}

func TestClustersFiltersDatabases(t *testing.T) {
	t.Parallel()

	p := newTestProvider(t, map[string]any{"exclude_db": "^(archive|scratch)$"})

	clusters := p.clusters([]string{"app", "archive", "billing", "scratch"})
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1", len(clusters))
	}

	cl := clusters[0]

	got := make([]string, 0, len(cl.Databases))
	for _, db := range cl.Databases {
		got = append(got, db.String())
	}

	if want := []string{"app", "billing"}; !slices.Equal(got, want) {
		t.Errorf("Databases = %v, want %v", got, want)
	}

	if cl.Name != "onprem_prod" || cl.ProviderID != "onprem_prod" {
		t.Errorf("cluster identity = %q / %q, want onprem_prod", cl.Name, cl.ProviderID)
	}

	if cl.Source != config.SourcePostgres {
		t.Errorf("Source = %q, want %q", cl.Source, config.SourcePostgres)
	}

	if cl.Port != defaultPort || len(cl.Hosts) != 2 {
		t.Errorf("got port %q and %d hosts, want %q and 2", cl.Port, len(cl.Hosts), defaultPort)
	}

	if cl.Labels["bootstrap_db"] != defaultBootstrapDB {
		t.Errorf("bootstrap_db label = %q, want %q", cl.Labels["bootstrap_db"], defaultBootstrapDB)
	}
}

func TestClustersIncludeFilter(t *testing.T) {
	t.Parallel()

	p := newTestProvider(t, map[string]any{"db": "^app"})

	cl := p.clusters([]string{"app", "app_staging", "billing"})[0]
	if len(cl.Databases) != 2 {
		t.Fatalf("Databases = %v, want app and app_staging", cl.Databases)
	}
}

func TestClustersEmptyAfterFiltering(t *testing.T) {
	t.Parallel()

	p := newTestProvider(t, map[string]any{"db": "^nothing$"})

	// An empty match is a successful cycle: the cluster is published without
	// databases and its pools go away.
	cl := p.clusters([]string{"app", "billing"})[0]
	if len(cl.Databases) != 0 {
		t.Fatalf("Databases = %v, want none", cl.Databases)
	}
}

func TestClustersLogsListChanges(t *testing.T) {
	t.Parallel()

	core, recorded := observer.New(zapcore.InfoLevel)

	p, err := NewProvider("onprem_prod", map[string]any{
		"hosts": []any{"pg-01.local"},
		"user":  "dasha",
	}, zap.New(core))
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	p.clusters([]string{"app", "billing"})
	p.clusters([]string{"analytics", "app"})
	p.clusters([]string{"analytics", "app"})

	if got := recorded.FilterMessage("databases discovered").Len(); got != 1 {
		t.Errorf("first cycle logged %d times, want 1", got)
	}

	changes := recorded.FilterMessage("database list changed").All()
	if len(changes) != 1 {
		t.Fatalf("logged %d changes, want 1 (an unchanged cycle stays quiet)", len(changes))
	}

	fields := changes[0].ContextMap()
	if got, want := loggedStrings(fields["added"]), []string{"analytics"}; !slices.Equal(got, want) {
		t.Errorf("added = %v, want %v", got, want)
	}

	if got, want := loggedStrings(fields["removed"]), []string{"billing"}; !slices.Equal(got, want) {
		t.Errorf("removed = %v, want %v", got, want)
	}
}

// loggedStrings reads back a zap.Strings field, which the observer's map encoder
// stores as []any.
func loggedStrings(field any) []string {
	values, ok := field.([]any)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, fmt.Sprint(v))
	}

	return out
}

func TestTargetsCarryConnectionSettings(t *testing.T) {
	t.Parallel()

	p := newTestProvider(t, map[string]any{
		"password":     "s3cret",
		"port":         5433, // a yaml number must reach the string field
		"bootstrap_db": "admin_db",
	})

	want := []pgdb.Target{
		{Host: "pg-01.local", Port: "5433", User: "dasha", Password: "s3cret", Database: "admin_db"},
		{Host: "pg-02.local", Port: "5433", User: "dasha", Password: "s3cret", Database: "admin_db"},
	}

	if got := p.targets(); !slices.Equal(got, want) {
		t.Errorf("targets() = %+v, want %+v", got, want)
	}
}

func TestDiscoverRemembersAnsweringHost(t *testing.T) {
	t.Parallel()

	p := newTestProvider(t, map[string]any{})

	var asked []string

	p.list = func(_ context.Context, target pgdb.Target) ([]string, error) {
		asked = append(asked, target.Host)
		if target.Host == "pg-01.local" {
			return nil, errors.New("connection refused")
		}

		return []string{"app"}, nil
	}

	for range 2 {
		if _, err := p.Discover(t.Context()); err != nil {
			t.Fatalf("Discover() error = %v", err)
		}
	}

	if want := []string{"pg-01.local", "pg-02.local", "pg-02.local"}; !slices.Equal(asked, want) {
		t.Errorf("asked %v, want %v", asked, want)
	}
}

func TestDiscoverErrorNamesEntry(t *testing.T) {
	t.Parallel()

	p := newTestProvider(t, map[string]any{})
	p.list = func(context.Context, pgdb.Target) ([]string, error) {
		return nil, errors.New("connection refused")
	}

	_, err := p.Discover(t.Context())
	if err == nil || !strings.Contains(err.Error(), `no host answered for entry "onprem_prod"`) {
		t.Fatalf("Discover() error = %v, want a no-host error", err)
	}
}

func TestNewProviderInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  any
		want time.Duration
	}{
		{name: "unset", raw: nil, want: config.DefaultDiscoveryInterval},
		{name: "bare number is minutes", raw: 7, want: 7 * time.Minute},
		{name: "duration string", raw: "90s", want: 90 * time.Second},
		{name: "below minimum", raw: "10s", want: config.MinDiscoveryInterval},
		{name: "negative", raw: -3, want: config.DefaultDiscoveryInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := map[string]any{}
			if tt.raw != nil {
				raw["refresh_interval"] = tt.raw
			}

			if got := newTestProvider(t, raw).Interval(); got != tt.want {
				t.Errorf("Interval() = %v, want %v", got, tt.want)
			}
		})
	}
}
