#!/bin/bash
set -e

echo "=== PG18 Master: initializing pgbench ==="
pgbench -i -s 1 -U demo demo

echo "=== PG18 Master: creating replication slots ==="
psql -U demo -d demo -c "SELECT pg_create_physical_replication_slot('pg18_replica_slot', true);"

echo "=== PG18 Master: creating publication for logical replication ==="
psql -U demo -d demo -c "CREATE PUBLICATION orders_pub FOR TABLE orders;"

echo "=== PG18 Master: init complete ==="
