#!/bin/bash
set -e

echo "=== PG17 Master: initializing pgbench ==="
pgbench -i -s 1 -U demo demo

echo "=== PG17 Master: creating replication slots ==="
psql -U demo -d demo -c "SELECT pg_create_physical_replication_slot('pg17_replica1_slot', true);"
psql -U demo -d demo -c "SELECT pg_create_physical_replication_slot('pg17_replica2_slot', true);"

echo "=== PG17 Master: init complete ==="
