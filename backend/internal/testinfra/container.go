//go:build integration

// Package testinfra provides test infrastructure for integration tests.
// It manages PostgreSQL test containers and provides isolated database pools.
package testinfra

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Default PostgreSQL version if POSTGRES_VERSION env is not set.
const defaultPGVersion = "17"

// TestContainer holds the primary PostgreSQL container and admin connection pool.
type TestContainer struct {
	AdminDSN  string
	Admin     *pgxpool.Pool
	Container testcontainers.Container
	PGVersion string
	Host      string
	Port      string
}

// MustNew creates a new primary PostgreSQL test container.
// Panics on failure — intended for use in TestMain.
func MustNew(ctx context.Context) *TestContainer {
	tc, err := newTestContainer(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to create test container: %v", err))
	}
	return tc
}

func newTestContainer(ctx context.Context) (*TestContainer, error) {
	pgVersion := os.Getenv("POSTGRES_VERSION")
	if pgVersion == "" {
		pgVersion = defaultPGVersion
	}

	image := fmt.Sprintf("postgres:%s-alpine", pgVersion)

	// Primary container with replication support and pg_stat_statements
	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "postgres",
		},
		Cmd: []string{
			"postgres",
			"-c", "shared_preload_libraries=pg_stat_statements",
			"-c", "pg_stat_statements.track=all",
			"-c", "wal_level=replica",
			"-c", "max_wal_senders=4",
			"-c", "max_replication_slots=4",
			"-c", "hot_standby=on",
			// Allow replication connections from any host (for replica container)
			"-c", "hba_file=/tmp/pg_hba.conf",
		},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            pgHbaReader(),
				ContainerFilePath: "/tmp/pg_hba.conf",
				FileMode:          0644,
			},
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
			wait.ForListeningPort("5432/tcp").
				WithStartupTimeout(15*time.Second),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("get host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, fmt.Errorf("get port: %w", err)
	}

	adminDSN := fmt.Sprintf("postgres://test:test@%s:%s/postgres?sslmode=disable", host, port.Port())

	admin, err := poolConnect(ctx, adminDSN)
	if err != nil {
		return nil, fmt.Errorf("admin pool connect: %w", err)
	}

	return &TestContainer{
		AdminDSN:  adminDSN,
		Admin:     admin,
		Container: container,
		PGVersion: pgVersion,
		Host:      host,
		Port:      port.Port(),
	}, nil
}

// TearDown stops the container and closes the admin pool.
func (tc *TestContainer) TearDown(ctx context.Context) {
	if tc.Admin != nil {
		tc.Admin.Close()
	}
	if tc.Container != nil {
		_ = tc.Container.Terminate(ctx)
	}
}

// poolConnect establishes a pgxpool connection with retry logic.
func poolConnect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	var (
		pool *pgxpool.Pool
		err  error
	)

	for {
		select {
		case <-cctx.Done():
			return nil, fmt.Errorf("timeout connecting to postgres: %w (last error: %v)", cctx.Err(), err)
		case <-ticker.C:
			pool, err = pgxpool.New(cctx, dsn)
			if err == nil && pool.Ping(cctx) == nil {
				return pool, nil
			}
			if pool != nil {
				pool.Close()
			}
		}
	}
}
