//go:build integration

package testinfra

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Global test container, set by TestMain in each test package.
var tc *TestContainer

// SetTestContainer sets the global test container for IsolatePool usage.
func SetTestContainer(c *TestContainer) {
	tc = c
}

// IsolatePool creates an isolated database copy from the fixture template.
// Each test gets its own database, enabling parallel test execution.
// The database is automatically dropped when the test completes.
func IsolatePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	require.NotNil(t, tc, "TestContainer not initialized — call SetTestContainer in TestMain")

	// Use test name as DB name (sanitized)
	dbName := sanitizeDBName(t.Name())

	ctx := t.Context()

	// Create database from fixture template
	_, err := tc.Admin.Exec(ctx, fmt.Sprintf(
		"CREATE DATABASE %s TEMPLATE %s", dbName, fixtureDBName))
	require.NoError(t, err, "create test database")

	// Connect to the new database
	dsn := strings.ReplaceAll(tc.AdminDSN, "/postgres?", fmt.Sprintf("/%s?", dbName))
	pool, err := poolConnect(ctx, dsn)
	require.NoError(t, err, "connect to test database")

	// Cleanup: close pool, terminate connections, drop database
	t.Cleanup(func() {
		pool.Close()

		cleanCtx := context.Background()

		// Terminate all connections to the test database
		_, _ = tc.Admin.Exec(cleanCtx, fmt.Sprintf(
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
			 WHERE datname = '%s' AND pid <> pg_backend_pid()`, dbName))

		// Drop the test database
		_, err := tc.Admin.Exec(cleanCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		if err != nil {
			t.Logf("WARNING: failed to drop test database %s: %v", dbName, err)
		}
	})

	return pool
}

// sanitizeDBName converts a test name to a valid PostgreSQL database name.
func sanitizeDBName(testName string) string {
	// Replace non-alphanumeric characters with underscores
	var b strings.Builder
	b.WriteString("test_")
	for _, r := range strings.ToLower(testName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}

	name := b.String()
	// PostgreSQL identifier limit is 63 bytes
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}
