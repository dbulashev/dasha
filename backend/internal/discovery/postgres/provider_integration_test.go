//go:build integration

package postgres

import (
	"context"
	"fmt"
	"maps"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/testinfra"
)

var tc *testinfra.TestContainer

func TestMain(m *testing.M) {
	ctx := context.Background()
	tc = testinfra.MustNew(ctx)

	code := m.Run()

	tc.TearDown(ctx)
	os.Exit(code)
}

// newIntegrationProvider builds a provider pointed at the test container.
// Every test filters on its own database prefix, so tests stay independent of
// the databases other tests leave behind while they run.
func newIntegrationProvider(t *testing.T, extra map[string]any) *Provider {
	t.Helper()

	raw := map[string]any{
		"hosts":    []any{tc.Host},
		"port":     tc.Port,
		"user":     "test",
		"password": "test",
	}
	maps.Copy(raw, extra)

	p, err := NewProvider("integration", raw, zap.NewNop())
	require.NoError(t, err)

	return p
}

func createDatabase(t *testing.T, name string, opts ...string) {
	t.Helper()

	quoted := pgx.Identifier{name}.Sanitize()

	stmt := "CREATE DATABASE " + quoted
	for _, opt := range opts {
		stmt += " " + opt
	}

	_, err := tc.Admin.Exec(t.Context(), stmt)
	require.NoError(t, err, "create database %s", name)

	t.Cleanup(func() {
		_, _ = tc.Admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	})
}

func discoveredNames(t *testing.T, p *Provider) []string {
	t.Helper()

	clusters, err := p.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, clusters, 1)

	names := make([]string, 0, len(clusters[0].Databases))
	for _, db := range clusters[0].Databases {
		names = append(names, db.String())
	}

	return names
}

func TestDiscoverListsDatabases(t *testing.T) {
	createDatabase(t, "disco_list_app")
	createDatabase(t, "disco_list_archive")

	p := newIntegrationProvider(t, map[string]any{"db": "^disco_list_"})

	assert.Equal(t, []string{"disco_list_app", "disco_list_archive"}, discoveredNames(t, p))

	cluster, err := p.Discover(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "integration", cluster[0].Name.String())
	assert.Equal(t, "postgres", cluster[0].Source)
	assert.Equal(t, tc.Port, cluster[0].Port)
}

func TestDiscoverAppliesExcludeFilter(t *testing.T) {
	createDatabase(t, "disco_excl_app")
	createDatabase(t, "disco_excl_archive")

	p := newIntegrationProvider(t, map[string]any{
		"db":         "^disco_excl_",
		"exclude_db": "archive$",
	})

	assert.Equal(t, []string{"disco_excl_app"}, discoveredNames(t, p))
}

func TestDiscoverSkipsTemplatesAndClosedDatabases(t *testing.T) {
	createDatabase(t, "disco_closed_db")

	_, err := tc.Admin.Exec(t.Context(),
		"ALTER DATABASE "+pgx.Identifier{"disco_closed_db"}.Sanitize()+" WITH ALLOW_CONNECTIONS false")
	require.NoError(t, err)

	p := newIntegrationProvider(t, map[string]any{"db": "^(disco_closed_db|template)"})

	assert.Empty(t, discoveredNames(t, p))
}

func TestDiscoverRespectsConnectPrivilege(t *testing.T) {
	createDatabase(t, "disco_priv_open")
	createDatabase(t, "disco_priv_closed")

	_, err := tc.Admin.Exec(t.Context(), "CREATE ROLE disco_probe LOGIN")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = tc.Admin.Exec(context.Background(), "DROP ROLE IF EXISTS disco_probe")
	})

	_, err = tc.Admin.Exec(t.Context(),
		"REVOKE CONNECT ON DATABASE "+pgx.Identifier{"disco_priv_closed"}.Sanitize()+" FROM PUBLIC")
	require.NoError(t, err)

	// The probe role is not a superuser, so has_database_privilege() actually
	// filters here — as the monitoring role would in a real cluster.
	p := newIntegrationProvider(t, map[string]any{
		"db":   "^disco_priv_",
		"user": "disco_probe",
	})

	assert.Equal(t, []string{"disco_priv_open"}, discoveredNames(t, p))
}

func TestDiscoverPicksUpCreateAndDrop(t *testing.T) {
	p := newIntegrationProvider(t, map[string]any{"db": "^disco_cycle_"})

	require.Empty(t, discoveredNames(t, p))

	quoted := pgx.Identifier{"disco_cycle_app"}.Sanitize()

	_, err := tc.Admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = tc.Admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)")
	})

	assert.Equal(t, []string{"disco_cycle_app"}, discoveredNames(t, p))

	_, err = tc.Admin.Exec(t.Context(), "DROP DATABASE "+quoted+" WITH (FORCE)")
	require.NoError(t, err)

	assert.Empty(t, discoveredNames(t, p))
}

func TestDiscoverFallsBackToNextHost(t *testing.T) {
	createDatabase(t, "disco_host_app")

	p := newIntegrationProvider(t, map[string]any{
		"hosts": []any{"dasha-discovery-nowhere.invalid", tc.Host},
		"db":    "^disco_host_",
	})

	assert.Equal(t, []string{"disco_host_app"}, discoveredNames(t, p))

	// The host that answered is remembered, so the dead one is not tried first
	// on every cycle.
	assert.Equal(t, 1, p.hostIdx)
	assert.Equal(t, []string{"disco_host_app"}, discoveredNames(t, p))
}

func TestDiscoverFailsWhenNoHostAnswers(t *testing.T) {
	p := newIntegrationProvider(t, map[string]any{
		"hosts": []any{"dasha-discovery-nowhere.invalid"},
	})

	_, err := p.Discover(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("no host answered for entry %q", "integration"))
}
