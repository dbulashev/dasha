//go:build integration

package testinfra

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ReplicaContainer holds the streaming replica container and its connection pool.
type ReplicaContainer struct {
	DSN       string
	Pool      *pgxpool.Pool
	Container testcontainers.Container
}

// CreateReplica creates a streaming replica of the primary container.
// It uses pg_basebackup to clone the primary and starts in recovery mode.
func (tc *TestContainer) CreateReplica(ctx context.Context) (*ReplicaContainer, error) {
	image := fmt.Sprintf("postgres:%s-alpine", tc.PGVersion)

	// Get primary container's network IP for inter-container communication
	primaryIP, err := tc.Container.ContainerIP(ctx)
	if err != nil {
		return nil, fmt.Errorf("get primary container IP: %w", err)
	}

	// Create replication slot on primary for the replica
	_, err = tc.Admin.Exec(ctx, "SELECT pg_create_physical_replication_slot('replica_slot', true)")
	if err != nil {
		return nil, fmt.Errorf("create replication slot: %w", err)
	}

	// Entrypoint script that performs pg_basebackup and starts postgres in recovery
	entrypoint := fmt.Sprintf(`#!/bin/bash
set -e

# Wait for primary to be ready
until pg_isready -h %s -p 5432 -U test; do
  echo "Waiting for primary..."
  sleep 1
done

# Clean data directory
rm -rf /var/lib/postgresql/data/*

# Base backup from primary
PGPASSWORD=test pg_basebackup \
  --host=%s --port=5432 \
  --username=test \
  --pgdata=/var/lib/postgresql/data \
  --wal-method=stream \
  --write-recovery-conf \
  --slot=replica_slot \
  --checkpoint=fast \
  -R

# Ensure hot_standby is enabled
echo "hot_standby = on" >> /var/lib/postgresql/data/postgresql.auto.conf

# Fix ownership and permissions
chown -R postgres:postgres /var/lib/postgresql/data
chmod 0700 /var/lib/postgresql/data

# Start postgres as postgres user
exec gosu postgres postgres -D /var/lib/postgresql/data
`, primaryIP, primaryIP)

	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
		},
		Entrypoint: []string{"bash", "-c", entrypoint},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept").
				WithStartupTimeout(60*time.Second),
			wait.ForListeningPort("5432/tcp").
				WithStartupTimeout(30*time.Second),
		),
		Networks: []string{},
	}

	// Share the same Docker network as the primary
	networks, err := tc.Container.Networks(ctx)
	if err == nil && len(networks) > 0 {
		req.Networks = networks
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("start replica container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("get replica host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, fmt.Errorf("get replica port: %w", err)
	}

	replicaDSN := fmt.Sprintf("postgres://test:test@%s:%s/postgres?sslmode=disable", host, port.Port())

	pool, err := poolConnect(ctx, replicaDSN)
	if err != nil {
		return nil, fmt.Errorf("replica pool connect: %w", err)
	}

	// Verify replica is actually in recovery mode
	var inRecovery bool
	err = pool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery)
	if err != nil {
		pool.Close()
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("check recovery status: %w", err)
	}
	if !inRecovery {
		pool.Close()
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("replica is not in recovery mode")
	}

	return &ReplicaContainer{
		DSN:       replicaDSN,
		Pool:      pool,
		Container: container,
	}, nil
}

// TearDown stops the replica container and closes the pool.
func (rc *ReplicaContainer) TearDown(ctx context.Context) {
	if rc.Pool != nil {
		rc.Pool.Close()
	}
	if rc.Container != nil {
		_ = rc.Container.Terminate(ctx)
	}
}
