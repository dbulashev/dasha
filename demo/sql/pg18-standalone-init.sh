#!/bin/bash
set -e

echo "=== PG18 Standalone: creating extensions and orders table ==="
psql -U demo -d demo <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;

-- Create orders table with same schema (no data — will come via logical replication)
CREATE TABLE orders (
    id serial PRIMARY KEY,
    user_id integer NOT NULL,
    status text NOT NULL DEFAULT 'new',
    tags integer[] DEFAULT '{}',
    amount numeric(10,2),
    created_at timestamptz DEFAULT now()
);
SQL

echo "=== PG18 Standalone: waiting for publication on pg18-master ==="
for i in $(seq 1 60); do
    if psql "host=pg18-master port=5432 dbname=demo user=demo password=demo" \
        -tAc "SELECT 1 FROM pg_publication WHERE pubname = 'orders_pub'" 2>/dev/null | grep -q 1; then
        echo "=== Publication found, creating subscription ==="
        psql -U demo -d demo -c \
            "CREATE SUBSCRIPTION orders_sub CONNECTION 'host=pg18-master port=5432 dbname=demo user=demo password=demo' PUBLICATION orders_pub;"
        echo "=== PG18 Standalone: init complete ==="
        exit 0
    fi
    echo "Waiting for orders_pub on pg18-master... ($i/60)"
    sleep 2
done

echo "ERROR: Timed out waiting for orders_pub publication on pg18-master"
exit 1
