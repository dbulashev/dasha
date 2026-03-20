-- Dasha Demo Lab Fixture
-- Adapted from backend/internal/testinfra/fixture.go

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- =============================================
-- Users table (FK target)
-- =============================================
CREATE TABLE users (
    id serial PRIMARY KEY,
    name text NOT NULL
);
INSERT INTO users (name) SELECT 'user_' || i FROM generate_series(1, 1000) i;

-- =============================================
-- Orders table (main table for indexes, bloat, queries)
-- =============================================
CREATE TABLE orders (
    id serial PRIMARY KEY,
    user_id integer NOT NULL REFERENCES users(id),
    status text NOT NULL DEFAULT 'new',
    tags integer[] DEFAULT '{}',
    amount numeric(10,2),
    created_at timestamptz DEFAULT now()
);
INSERT INTO orders (user_id, status, tags, amount, created_at)
SELECT
    1 + (random()*999)::int,
    (ARRAY['new','processing','done','cancelled'])[1 + (random()*3)::int],
    ARRAY[(random()*100)::int, (random()*100)::int],
    (random()*10000)::numeric(10,2),
    now() - (random() * interval '365 days')
FROM generate_series(1, 20000);

-- Regular indexes
CREATE INDEX idx_orders_user_id ON orders (user_id);
CREATE INDEX idx_orders_status ON orders (status);
CREATE INDEX idx_orders_created_at ON orders (created_at);
CREATE INDEX idx_orders_amount ON orders (amount);

-- BTree on array column (detected by btree_on_array)
CREATE INDEX idx_orders_tags ON orders USING btree (tags);

-- Duplicate/similar indexes (detected by similar_1/2/3)
CREATE UNIQUE INDEX idx_orders_user_id_unique ON orders (user_id, id);
CREATE INDEX idx_orders_user_id_status ON orders (user_id, status);

-- similar_1: regular index duplicating PK (unique vs non-unique, same columns)
CREATE INDEX idx_orders_id_dup ON orders (id);

-- similar_3: exact duplicate of idx_orders_status (same definition after simplification)
CREATE INDEX idx_orders_status_dup ON orders (status);

-- =============================================
-- Partitioned table (detected by tables/partitions)
-- =============================================
CREATE TABLE events (
    id serial,
    event_date date NOT NULL,
    payload text
) PARTITION BY RANGE (event_date);

CREATE TABLE events_2025 PARTITION OF events
    FOR VALUES FROM ('2025-01-01') TO ('2026-01-01');
CREATE TABLE events_2026 PARTITION OF events
    FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');

INSERT INTO events (event_date, payload)
SELECT '2025-01-01'::date + (random()*700)::int, 'data_' || i
FROM generate_series(1, 5000) i;

-- =============================================
-- FK type mismatch: products.category_id (int) → categories.id (bigint)
-- =============================================
CREATE TABLE categories (
    id bigint PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    name text NOT NULL
);
INSERT INTO categories (name) VALUES ('electronics'), ('books'), ('clothing');

CREATE TABLE products (
    id serial PRIMARY KEY,
    name text NOT NULL,
    category_id integer REFERENCES categories(id),  -- int → bigint mismatch
    created_by integer REFERENCES users(id)          -- nullable FK
);
INSERT INTO products (name, category_id, created_by)
SELECT 'product_' || i, 1 + (i % 3), 1 + (i % 100)
FROM generate_series(1, 100) i;

-- =============================================
-- Duplicate FK columns (detected by fks/possible_similar)
-- =============================================
CREATE TABLE shipments (
    id serial PRIMARY KEY,
    order_id integer NOT NULL,
    alt_order_id integer NOT NULL,
    shipped_at timestamptz DEFAULT now(),
    CONSTRAINT fk_shipments_order FOREIGN KEY (order_id) REFERENCES orders(id),
    CONSTRAINT fk_shipments_alt_order FOREIGN KEY (alt_order_id) REFERENCES orders(id)
);
INSERT INTO shipments (order_id, alt_order_id)
SELECT 1 + (i % 1000), 1 + ((i+500) % 1000)
FROM generate_series(1, 100) i;

-- =============================================
-- Overlapping FK columns (detected by indexes/similar_2)
-- Two FKs from the same table to the same target sharing a column
-- =============================================
CREATE UNIQUE INDEX orders_id_user_id_uniq ON orders(id, user_id);

CREATE TABLE order_notes (
    id serial PRIMARY KEY,
    order_id integer NOT NULL,
    user_id integer NOT NULL,
    note text,
    CONSTRAINT fk_order_notes_order FOREIGN KEY (order_id) REFERENCES orders(id),
    CONSTRAINT fk_order_notes_order_user FOREIGN KEY (order_id, user_id) REFERENCES orders(id, user_id)
);
INSERT INTO order_notes (order_id, user_id, note)
SELECT id, user_id, 'note_' || id
FROM orders LIMIT 100;

-- =============================================
-- Dead rows for maintenance/info
-- =============================================
CREATE TABLE deadrows_test (
    id serial PRIMARY KEY,
    data text
);
INSERT INTO deadrows_test (data) SELECT 'row_' || i FROM generate_series(1, 500) i;
DELETE FROM deadrows_test WHERE id <= 200;

-- Invalid FK constraint (NOT VALID — detected by constraints/invalid_constraints)
ALTER TABLE deadrows_test ADD COLUMN ref_user_id integer;
ALTER TABLE deadrows_test ADD CONSTRAINT fk_deadrows_user
    FOREIGN KEY (ref_user_id) REFERENCES users(id) NOT VALID;

-- =============================================
-- Invalid index (detected by indexes/invalid_or_not_ready)
-- Note: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- (docker-entrypoint-initdb.d wraps .sql files in a transaction),
-- so we use a regular index + manual invalidation.
-- =============================================
CREATE INDEX IF NOT EXISTS idx_orders_invalid ON orders (id) WHERE id < 0;
UPDATE pg_index SET indisvalid = false WHERE indexrelid = 'idx_orders_invalid'::regclass;

-- =============================================
-- Warm up stats
-- =============================================
SELECT count(*) FROM orders;
SELECT count(*) FROM orders WHERE user_id = 1;
SELECT count(*) FROM orders WHERE user_id = 2;
SELECT count(*) FROM orders WHERE status = 'new';
SELECT count(*) FROM orders WHERE status = 'done';
SELECT * FROM orders WHERE created_at > now() - interval '30 days' LIMIT 1;
SELECT * FROM orders WHERE amount < 100 LIMIT 1;
SELECT count(*) FROM users;
SELECT count(*) FROM events;
SELECT * FROM products p JOIN categories c ON c.id = p.category_id LIMIT 1;

ANALYZE;
