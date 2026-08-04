#!/bin/bash
set -e

echo "=== PG18 Standalone: creating extensions and orders table ==="
psql -U demo -d demo <<'SQL'
-- Extensions deliberately live in a dedicated schema that is NOT on the default
-- search_path — the "CREATE EXTENSION … SCHEMA ext" layout many installations
-- use. Unqualified queries against pg_stat_statements then fail with "relation
-- does not exist", so this cluster is the demo case for schema resolution:
-- query stats here must work exactly as on the other two clusters.
CREATE SCHEMA IF NOT EXISTS ext;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements SCHEMA ext;
CREATE EXTENSION IF NOT EXISTS pgstattuple SCHEMA ext;

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

# Extra databases: this cluster is the demo case for in-cluster database
# discovery (discovery.pg18-standalone in dasha-demo.yaml lists no databases at
# all), so it needs more than one database to discover.
echo "=== PG18 Standalone: creating extra databases ==="
psql -U demo -d demo <<'SQL'
CREATE DATABASE analytics OWNER demo;
CREATE DATABASE billing OWNER demo;
CREATE DATABASE archive OWNER demo;
SQL

psql -U demo -d analytics <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;

CREATE TABLE events (
    id bigserial PRIMARY KEY,
    user_id integer NOT NULL,
    event_type text NOT NULL,
    payload jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO events (user_id, event_type, payload, created_at)
SELECT (random() * 5000)::int,
       (ARRAY['page_view', 'click', 'signup', 'purchase'])[1 + (random() * 3)::int],
       jsonb_build_object('source', 'demo', 'seq', i),
       now() - (random() * 30) * interval '1 day'
FROM generate_series(1, 20000) AS i;

CREATE INDEX events_user_id_idx ON events (user_id);
CREATE INDEX events_created_at_idx ON events (created_at);

ANALYZE events;
SQL

psql -U demo -d billing <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;

CREATE TABLE invoices (
    id serial PRIMARY KEY,
    customer_id integer NOT NULL,
    amount numeric(12,2) NOT NULL,
    status text NOT NULL DEFAULT 'issued',
    issued_at date NOT NULL DEFAULT current_date
);

CREATE TABLE payments (
    id serial PRIMARY KEY,
    invoice_id integer NOT NULL REFERENCES invoices (id),
    amount numeric(12,2) NOT NULL,
    paid_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO invoices (customer_id, amount, status, issued_at)
SELECT (random() * 800)::int,
       (random() * 5000)::numeric(12,2),
       (ARRAY['issued', 'paid', 'overdue'])[1 + (random() * 2)::int],
       current_date - (random() * 180)::int
FROM generate_series(1, 5000);

INSERT INTO payments (invoice_id, amount, paid_at)
SELECT id, amount, now() - (random() * 90) * interval '1 day'
FROM invoices
WHERE status = 'paid';

CREATE INDEX invoices_status_idx ON invoices (status);

-- No index on payments.invoice_id: the FK analysis page has something to report.
ANALYZE invoices;
ANALYZE payments;
SQL

psql -U demo -d archive <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;

CREATE TABLE orders_2024 (
    id bigserial PRIMARY KEY,
    user_id integer NOT NULL,
    status text NOT NULL,
    amount numeric(10,2),
    created_at timestamptz NOT NULL
);

INSERT INTO orders_2024 (user_id, status, amount, created_at)
SELECT (random() * 3000)::int,
       (ARRAY['shipped', 'cancelled', 'returned'])[1 + (random() * 2)::int],
       (random() * 900)::numeric(10,2),
       timestamptz '2024-01-01' + (random() * 365) * interval '1 day'
FROM generate_series(1, 10000);

ANALYZE orders_2024;
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
