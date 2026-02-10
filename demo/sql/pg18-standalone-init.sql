-- PG18 Standalone: logical replication subscriber for orders table

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Create orders table with same schema (no data — will come via logical replication)
CREATE TABLE orders (
    id serial PRIMARY KEY,
    user_id integer NOT NULL,
    status text NOT NULL DEFAULT 'new',
    tags integer[] DEFAULT '{}',
    amount numeric(10,2),
    created_at timestamptz DEFAULT now()
);

-- Subscribe to orders publication from pg18-master
CREATE SUBSCRIPTION orders_sub
    CONNECTION 'host=pg18-master port=5432 dbname=demo user=demo password=demo'
    PUBLICATION orders_pub;
