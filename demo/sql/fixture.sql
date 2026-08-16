-- Dasha Demo Lab Fixture
-- Adapted from backend/internal/testinfra/fixture.go

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;

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
-- HASH-partitioned table (hot-objects rollup demo)
-- 8 hash partitions + a partitioned secondary index. Activity spreads evenly
-- across the leaves, so the hot-objects top must roll them up and show the
-- parent `sensor_readings` (and one anchor row, not eight).
-- =============================================
CREATE TABLE sensor_readings (
    id          bigint GENERATED ALWAYS AS IDENTITY,
    sensor_id   integer NOT NULL,
    reading     numeric(10,2) NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sensor_id, id)
) PARTITION BY HASH (sensor_id);

DO $$
BEGIN
    FOR r IN 0..7 LOOP
        EXECUTE format(
            'CREATE TABLE sensor_readings_p%1$s PARTITION OF sensor_readings FOR VALUES WITH (MODULUS 8, REMAINDER %1$s)',
            r);
    END LOOP;
END $$;

CREATE INDEX idx_sensor_readings_recorded ON sensor_readings (recorded_at);

INSERT INTO sensor_readings (sensor_id, reading, recorded_at)
SELECT (random()*10000)::int, (random()*100)::numeric(10,2), now() - (random() * interval '30 days')
FROM generate_series(1, 40000);

-- =============================================
-- RANGE → HASH subpartitioned table (hot-objects recursive rollup demo)
-- Two monthly range partitions, each split into 4 hash subpartitions. Rollup
-- must collapse the hash subpartitions into their month but KEEP the months
-- distinct: the top shows metrics_2026_01 / metrics_2026_02, not the *_pN leaves.
-- =============================================
CREATE TABLE metrics (
    id          bigint GENERATED ALWAYS AS IDENTITY,
    bucket      integer NOT NULL,
    metric_date date NOT NULL,
    value       double precision,
    PRIMARY KEY (metric_date, bucket, id)
) PARTITION BY RANGE (metric_date);

CREATE TABLE metrics_2026_01 PARTITION OF metrics
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01') PARTITION BY HASH (bucket);
CREATE TABLE metrics_2026_02 PARTITION OF metrics
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01') PARTITION BY HASH (bucket);

DO $$
DECLARE
    m text;
BEGIN
    FOREACH m IN ARRAY ARRAY['metrics_2026_01', 'metrics_2026_02'] LOOP
        FOR h IN 0..3 LOOP
            EXECUTE format(
                'CREATE TABLE %1$s_p%2$s PARTITION OF %1$s FOR VALUES WITH (MODULUS 4, REMAINDER %2$s)',
                m, h);
        END LOOP;
    END LOOP;
END $$;

INSERT INTO metrics (bucket, metric_date, value)
SELECT (random()*1000)::int, '2026-01-01'::date + (random()*58)::int, random()*1000
FROM generate_series(1, 20000);

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

-- =============================================
-- Wide table for row estimate / TOAST demo
-- =============================================
CREATE TABLE customer_profiles (
    id serial PRIMARY KEY,
    first_name varchar(100) NOT NULL,
    last_name varchar(100) NOT NULL,
    email varchar(255) NOT NULL,
    phone varchar(20),
    bio text,                        -- TOAST candidate (extended)
    preferences jsonb DEFAULT '{}',  -- TOAST candidate (extended)
    avatar_data bytea,               -- TOAST candidate (extended)
    notes text,                      -- TOAST candidate (extended)
    address_line1 varchar(200),
    address_line2 varchar(200),
    city varchar(100),
    country varchar(100),
    postal_code varchar(20),
    metadata jsonb DEFAULT '{}',
    created_at timestamptz DEFAULT now(),
    updated_at timestamptz DEFAULT now()
) WITH (fillfactor = 70);

INSERT INTO customer_profiles (
    first_name, last_name, email, phone,
    bio, preferences, avatar_data, notes,
    address_line1, city, country, postal_code, metadata
)
SELECT
    'First_' || i,
    'Last_' || i,
    'user' || i || '@example.com',
    '+1-555-' || lpad((i % 10000)::text, 4, '0'),
    repeat('Lorem ipsum dolor sit amet. ', 20 + (i % 80)),  -- 600-2800 bytes
    jsonb_build_object(
        'theme', (ARRAY['light','dark','auto'])[1 + i % 3],
        'lang', (ARRAY['en','ru','de'])[1 + i % 3],
        'notifications', jsonb_build_object('email', i % 2 = 0, 'push', i % 3 = 0),
        'tags', jsonb_build_array('tag_' || (i % 10), 'tag_' || (i % 20))
    ),
    decode(repeat(lpad(to_hex(i % 256), 2, '0'), 500 + (i % 1500)), 'hex'),  -- 500-2000 bytes
    CASE WHEN i % 3 = 0 THEN repeat('Note entry. ', 10 + (i % 50)) ELSE NULL END,
    i || ' Main Street',
    (ARRAY['Moscow','Berlin','London','Tokyo','New York'])[1 + i % 5],
    (ARRAY['RU','DE','GB','JP','US'])[1 + i % 5],
    lpad((10000 + i % 90000)::text, 5, '0'),
    CASE WHEN i % 5 = 0 THEN '{"vip": true}'::jsonb ELSE '{}'::jsonb END
FROM generate_series(1, 5000) i;

-- Table with low fillfactor for HOT update demo
CREATE TABLE hot_update_demo (
    id serial PRIMARY KEY,
    counter integer NOT NULL DEFAULT 0,
    status varchar(20) NOT NULL DEFAULT 'active',
    last_ping timestamptz DEFAULT now()
) WITH (fillfactor = 50);

INSERT INTO hot_update_demo (counter, status)
SELECT i % 1000, (ARRAY['active','idle','busy'])[1 + i % 3]
FROM generate_series(1, 10000) i;

-- Table kept under an ACCESS EXCLUSIVE lock by the workload generator, so
-- "Describe table" hits lock_timeout and the API answers 423 instead of 500.
-- Nothing else touches it — the lock must not stall the rest of the workload.
CREATE TABLE locked_table_demo (
    id serial PRIMARY KEY,
    payload text NOT NULL,
    updated_at timestamptz DEFAULT now()
);

INSERT INTO locked_table_demo (payload)
SELECT repeat('locked row ', 10) || i FROM generate_series(1, 5000) i;

CREATE INDEX idx_locked_table_demo_updated ON locked_table_demo (updated_at);

-- Materialized view for row estimate on matview
CREATE MATERIALIZED VIEW mv_order_summary AS
SELECT
    u.id AS user_id,
    u.name AS user_name,
    count(o.id) AS order_count,
    coalesce(sum(o.amount), 0) AS total_amount,
    coalesce(avg(o.amount), 0) AS avg_amount,
    min(o.created_at) AS first_order,
    max(o.created_at) AS last_order
FROM users u
LEFT JOIN orders o ON o.user_id = u.id
GROUP BY u.id, u.name;

CREATE UNIQUE INDEX idx_mv_order_summary_user ON mv_order_summary (user_id);

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
-- Schema Checks fixtures
-- One instance of every check the /schema-lint page runs by default, plus
-- relation_without_fk, which is off by default and only shows up once it is
-- enabled in the config. Keeps the page from being almost empty in the demo.
-- Nothing below is touched by the workload generator.
-- =============================================

-- A second schema, so the schema filter and the grouping have something to
-- separate; PUBLIC may create in it (public_create_privilege).
CREATE SCHEMA legacy;
GRANT CREATE ON SCHEMA legacy TO PUBLIC;

-- sequence_exhaustion: a serial with ~15% of int4 left. That alone is a notice,
-- but the owning column is integer, which raises it to a warning and makes the
-- report say ALTER SEQUENCE is not the whole fix. Deliberately kept above the
-- 5%-free error threshold: an error-level sequence clamps the health score of
-- the whole instance, and the demo dashboard should not sit permanently red.
CREATE TABLE legacy.visit_counters (
    id serial PRIMARY KEY,
    label text NOT NULL
);
INSERT INTO legacy.visit_counters (label) SELECT 'counter_' || i FROM generate_series(1, 10) i;
SELECT setval('legacy.visit_counters_id_seq', 1825000000);

-- The same check on a standalone sequence with a small maxvalue: no owning
-- column, so it stays a notice.
CREATE SEQUENCE legacy.batch_no_seq MAXVALUE 1000;
SELECT setval('legacy.batch_no_seq', 880);

-- no_primary_key: a unique index exists, but over a nullable column, so it is
-- no REPLICA IDENTITY either (unique_nullable).
CREATE TABLE legacy.import_rows (
    external_ref integer UNIQUE,
    payload text
);
INSERT INTO legacy.import_rows (external_ref, payload)
SELECT i, 'row_' || i FROM generate_series(1, 200) i;

-- unlogged_relation / unlogged_sequence (unlogged sequences need PG 15+).
CREATE UNLOGGED TABLE legacy.session_scratch (
    id bigint PRIMARY KEY,
    token text NOT NULL
);
INSERT INTO legacy.session_scratch SELECT i, md5(i::text) FROM generate_series(1, 500) i;

CREATE UNLOGGED SEQUENCE legacy.scratch_seq;

-- uuid_in_non_uuid_type: a UUID kept as varchar(36).
CREATE TABLE legacy.external_accounts (
    id serial PRIMARY KEY,
    account_uid varchar(36) NOT NULL,
    provider text NOT NULL
);
INSERT INTO legacy.external_accounts (account_uid, provider)
SELECT gen_random_uuid()::text, 'provider_' || (i % 5) FROM generate_series(1, 300) i;

-- relation_without_columns: every column dropped, the relation stays.
CREATE TABLE legacy.abandoned (obsolete integer);
ALTER TABLE legacy.abandoned DROP COLUMN obsolete;

-- reserved_word_in_name / unsafe_chars_in_name.
CREATE TABLE legacy."order" (
    id serial PRIMARY KEY,
    placed_at timestamptz DEFAULT now()
);

CREATE TABLE legacy."order items" (
    id serial PRIMARY KEY,
    qty integer NOT NULL
);

-- relation_without_fk (off by default): a table on neither side of any FK.
CREATE TABLE legacy.audit_log (
    id bigserial PRIMARY KEY,
    happened_at timestamptz DEFAULT now(),
    message text NOT NULL
);
INSERT INTO legacy.audit_log (message) SELECT 'event ' || i FROM generate_series(1, 200) i;

-- Partition rollup: the same defect on every partition is shown once, on the
-- parent, with a partition count. Two checks at once — no key on the table, and
-- a UUID kept as varchar on a column — so both rollup keys are exercised: the
-- one addressed by the relation alone and the one addressed by relation +
-- column. Not UNLOGGED: PostgreSQL refuses that on a partitioned table.
CREATE TABLE legacy.staging_rows (
    id bigint,
    record_uid varchar(36) NOT NULL,
    batch_date date NOT NULL
) PARTITION BY RANGE (batch_date);

CREATE TABLE legacy.staging_rows_2026_01 PARTITION OF legacy.staging_rows
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE legacy.staging_rows_2026_02 PARTITION OF legacy.staging_rows
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE legacy.staging_rows_2026_03 PARTITION OF legacy.staging_rows
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

INSERT INTO legacy.staging_rows (id, record_uid, batch_date)
SELECT i, gen_random_uuid()::text, '2026-01-01'::date + (random()*80)::int
FROM generate_series(1, 3000) i;

-- =============================================
-- Index advisor: join column + filter column on one unindexed table.
-- session_events has only its primary key, so the reports below seq-scan all
-- 60 000 rows. The advisor must propose session_events (session_id, status) —
-- session_id first, it is the more selective of the two.
-- =============================================
CREATE TABLE sessions (
    id bigserial PRIMARY KEY,
    user_id integer NOT NULL,
    channel text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE session_events (
    id bigserial PRIMARY KEY,
    session_id bigint NOT NULL,
    status text NOT NULL,
    latency_ms integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO sessions (user_id, channel, started_at)
SELECT (random() * 5000)::int,
       (ARRAY['web', 'ios', 'android', 'partner'])[1 + (random() * 3)::int],
       now() - (random() * 30) * interval '1 day'
FROM generate_series(1, 5000);

INSERT INTO session_events (session_id, status, latency_ms, created_at)
SELECT (random() * 4999)::int + 1,
       (ARRAY['ok', 'failed', 'timeout'])[1 + (random() * 2)::int],
       (random() * 5000)::int,
       now() - (random() * 30) * interval '1 day'
FROM generate_series(1, 60000);

-- Aggregate per (session, status): ~15 000 rows, above the advisor's size
-- threshold. The unique index is what lets REFRESH run CONCURRENTLY; the
-- advisor must still propose (status, event_count) for the report below and
-- warn that a plain REFRESH rebuilds both.
CREATE MATERIALIZED VIEW mv_session_stats AS
SELECT e.session_id,
       e.status,
       count(*)          AS event_count,
       avg(e.latency_ms) AS avg_latency_ms,
       max(e.created_at) AS last_seen
FROM session_events e
GROUP BY e.session_id, e.status;

CREATE UNIQUE INDEX mv_session_stats_key ON mv_session_stats (session_id, status);

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
SELECT count(*) FROM customer_profiles WHERE bio IS NOT NULL;
SELECT * FROM customer_profiles ORDER BY random() LIMIT 5;
SELECT count(*) FROM hot_update_demo;
SELECT * FROM mv_order_summary LIMIT 5;
SELECT count(*) FROM sensor_readings WHERE sensor_id < 100;
SELECT count(*) FROM metrics WHERE metric_date >= '2026-02-01';
SELECT s.channel, count(*) FROM sessions s
    JOIN session_events e ON e.session_id = s.id
    WHERE e.status = 'failed' GROUP BY s.channel;
SELECT session_id, event_count FROM mv_session_stats
    WHERE status = 'failed' AND event_count > 5
    ORDER BY event_count DESC LIMIT 20;

ANALYZE;
