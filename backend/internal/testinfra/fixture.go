//go:build integration

package testinfra

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fixtureDBName is the template database name used for test isolation.
const fixtureDBName = "dasha_fixture"

// Setup creates the fixture template database with test data.
// Call once in TestMain before running tests.
func (tc *TestContainer) Setup(ctx context.Context) error {
	// Create fixture database
	_, err := tc.Admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", fixtureDBName))
	if err != nil {
		return fmt.Errorf("create fixture db: %w", err)
	}

	// Connect to fixture database
	dsn := strings.ReplaceAll(tc.AdminDSN, "/postgres?", fmt.Sprintf("/%s?", fixtureDBName))
	pool, err := poolConnect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to fixture db: %w", err)
	}
	defer pool.Close()

	// Apply fixture schema and data
	for i, stmt := range fixtureStatements() {
		_, err = pool.Exec(ctx, stmt)
		if err != nil {
			return fmt.Errorf("fixture statement %d: %w", i, err)
		}
	}

	// Create invalid index via CONCURRENTLY (requires separate connection, outside tx)
	if err := createInvalidIndex(ctx, pool); err != nil {
		// Non-fatal: invalid index creation may fail on some PG versions
		fmt.Printf("WARNING: could not create invalid index: %v\n", err)
	}

	// Run ANALYZE to populate statistics
	for _, table := range []string{"orders", "users", "events", "events_2025", "events_2026",
		"categories", "products", "shipments", "deadrows_test"} {
		_, err = pool.Exec(ctx, fmt.Sprintf("ANALYZE %s", table))
		if err != nil {
			return fmt.Errorf("analyze %s: %w", table, err)
		}
	}

	// VACUUM deadrows_test so n_dead_tup is visible in maintenance/info
	_, _ = pool.Exec(ctx, "VACUUM deadrows_test")

	// Generate some query stats for pg_stat_statements tests
	if err := generateQueryStats(ctx, pool); err != nil {
		return fmt.Errorf("generate query stats: %w", err)
	}

	// Warm up I/O stats so pg_statio_user_tables/indexes have data
	if err := warmupIOStats(ctx, pool); err != nil {
		return fmt.Errorf("warmup IO stats: %w", err)
	}

	// Force stats update so pg_stat_user_tables has data
	_, _ = pool.Exec(ctx, "SELECT pg_stat_force_next_flush()")

	return nil
}

// fixtureStatements returns the SQL statements to create the test fixture.
func fixtureStatements() []string {
	return []string{
		// Enable pg_stat_statements extension
		`CREATE EXTENSION IF NOT EXISTS pg_stat_statements`,
		`CREATE EXTENSION IF NOT EXISTS pgstattuple`,

		// Users table (for FK tests)
		`CREATE TABLE users (
			id serial PRIMARY KEY,
			name text NOT NULL
		)`,
		`INSERT INTO users (name)
		 SELECT 'user_' || i FROM generate_series(1, 1000) i`,

		// Orders table (main table for indexes tests)
		`CREATE TABLE orders (
			id serial PRIMARY KEY,
			user_id integer NOT NULL REFERENCES users(id),
			status text NOT NULL DEFAULT 'new',
			tags integer[] DEFAULT '{}',
			amount numeric(10,2),
			created_at timestamptz DEFAULT now()
		)`,
		`INSERT INTO orders (user_id, status, tags, amount, created_at)
		 SELECT
			1 + (random()*999)::int,
			(ARRAY['new','processing','done','cancelled'])[1 + (random()*3)::int],
			ARRAY[(random()*100)::int, (random()*100)::int],
			(random()*10000)::numeric(10,2),
			now() - (random() * interval '365 days')
		 FROM generate_series(1, 20000)`,

		// Regular indexes
		`CREATE INDEX idx_orders_user_id ON orders (user_id)`,
		`CREATE INDEX idx_orders_status ON orders (status)`,
		`CREATE INDEX idx_orders_created_at ON orders (created_at)`,
		`CREATE INDEX idx_orders_amount ON orders (amount)`,

		// BTree on array column (for btree_on_array detection)
		`CREATE INDEX idx_orders_tags ON orders USING btree (tags)`,

		// Duplicate/similar indexes (for similar_1/2/3 detection)
		`CREATE UNIQUE INDEX idx_orders_user_id_unique ON orders (user_id, id)`,
		`CREATE INDEX idx_orders_user_id_status ON orders (user_id, status)`,

		// Partitioned table (for tables/partitions tests)
		`CREATE TABLE events (
			id serial,
			event_date date NOT NULL,
			payload text
		) PARTITION BY RANGE (event_date)`,
		`CREATE TABLE events_2025 PARTITION OF events
			FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')`,
		`CREATE TABLE events_2026 PARTITION OF events
			FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')`,
		`INSERT INTO events (event_date, payload)
		 SELECT '2025-01-01'::date + (random()*700)::int, 'data_' || i
		 FROM generate_series(1, 5000) i`,

		// Trigger some sequential scans and index scans for statistics
		`SELECT count(*) FROM orders WHERE user_id = 1`,
		`SELECT count(*) FROM orders WHERE status = 'new'`,
		`SELECT count(*) FROM orders`,

		// === Problematic FK tables (isolated, for fks/ tests) ===

		// Table with bigint PK (for type mismatch FK test)
		`CREATE TABLE categories (
			id bigint PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
			name text NOT NULL
		)`,
		`INSERT INTO categories (name) VALUES ('electronics'), ('books'), ('clothing')`,

		// Table with nullable FK (for fks/possible_nulls)
		// and type-mismatched FK int → bigint (for fks/type_mismatch)
		`CREATE TABLE products (
			id serial PRIMARY KEY,
			name text NOT NULL,
			category_id integer REFERENCES categories(id),
			created_by integer REFERENCES users(id)
		)`,
		`INSERT INTO products (name, category_id, created_by)
		 SELECT 'product_' || i, 1 + (i % 3), 1 + (i % 100)
		 FROM generate_series(1, 100) i`,

		// Duplicate FK columns (for fks/possible_similar1 — identical FK columns)
		`CREATE TABLE shipments (
			id serial PRIMARY KEY,
			order_id integer NOT NULL,
			alt_order_id integer NOT NULL,
			shipped_at timestamptz DEFAULT now(),
			CONSTRAINT fk_shipments_order FOREIGN KEY (order_id) REFERENCES orders(id),
			CONSTRAINT fk_shipments_alt_order FOREIGN KEY (alt_order_id) REFERENCES orders(id)
		)`,
		`INSERT INTO shipments (order_id, alt_order_id)
		 SELECT 1 + (i % 1000), 1 + ((i+500) % 1000)
		 FROM generate_series(1, 100) i`,

		// === Dead rows for maintenance/info tests ===
		`CREATE TABLE deadrows_test (
			id serial PRIMARY KEY,
			data text
		)`,
		`INSERT INTO deadrows_test (data) SELECT 'row_' || i FROM generate_series(1, 500) i`,
		`DELETE FROM deadrows_test WHERE id <= 200`,

		// === Invalid FK constraint (NOT VALID, for constraints/invalid_constraints) ===
		// The invalid_constraints SQL checks convalidated=false on FK constraints
		`ALTER TABLE deadrows_test ADD COLUMN ref_user_id integer`,
		`ALTER TABLE deadrows_test ADD CONSTRAINT fk_deadrows_user
		 FOREIGN KEY (ref_user_id) REFERENCES users(id) NOT VALID`,
	}
}

// createInvalidIndex attempts to create an invalid index using CREATE INDEX CONCURRENTLY.
// It starts the index build and then cancels it to leave the index in an invalid state.
func createInvalidIndex(ctx context.Context, pool *pgxpool.Pool) error {
	// Create a function that will fail during index build
	_, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION fail_after_rows() RETURNS boolean AS $$
		DECLARE
			cnt integer;
		BEGIN
			SELECT count(*) INTO cnt FROM pg_stat_activity
			WHERE query LIKE '%idx_orders_invalid%' AND query LIKE '%CREATE%';
			IF cnt > 0 THEN
				RAISE EXCEPTION 'intentional failure for invalid index test';
			END IF;
			RETURN true;
		END;
		$$ LANGUAGE plpgsql`)
	if err != nil {
		return fmt.Errorf("create fail function: %w", err)
	}

	// Try to create index CONCURRENTLY with a WHERE clause that references the failing function.
	// CREATE INDEX CONCURRENTLY cannot run inside a transaction, which pgxpool handles correctly.
	// If it fails, the index will be left in an invalid state.
	_, _ = pool.Exec(ctx,
		`CREATE INDEX CONCURRENTLY idx_orders_invalid ON orders (id) WHERE fail_after_rows()`)

	// Verify the index exists and is invalid
	var exists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_index i
			JOIN pg_class c ON c.oid = i.indexrelid
			WHERE c.relname = 'idx_orders_invalid' AND NOT i.indisvalid
		)`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check invalid index: %w", err)
	}
	if !exists {
		// Fallback: create a valid index and then manually mark it as invalid
		_, err = pool.Exec(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_invalid ON orders (id) WHERE id < 0`)
		if err != nil {
			return fmt.Errorf("create fallback index: %w", err)
		}
		_, err = pool.Exec(ctx, `UPDATE pg_index SET indisvalid = false WHERE indexrelid = 'idx_orders_invalid'::regclass`)
		if err != nil {
			return fmt.Errorf("invalidate index: %w", err)
		}
	}

	return nil
}

// warmupIOStats runs diverse queries to populate pg_statio_user_tables and pg_statio_user_indexes.
// This ensures tables/caching, indexes/hit_rate, tables/hit_rate return non-empty results.
func warmupIOStats(ctx context.Context, pool *pgxpool.Pool) error {
	// Reset stats to get clean baseline
	_, _ = pool.Exec(ctx, "SELECT pg_stat_reset()")

	// Sequential scans (populate heap_blks_read/hit)
	stmts := []string{
		`SELECT count(*) FROM orders`,
		`SELECT count(*) FROM users`,
		`SELECT * FROM orders WHERE amount > 5000 LIMIT 100`,
		`SELECT * FROM users WHERE name LIKE 'user_1%'`,

		// Index scans (populate idx_blks_read/hit)
		`SELECT * FROM orders WHERE user_id = 1`,
		`SELECT * FROM orders WHERE user_id = 2`,
		`SELECT * FROM orders WHERE user_id = 3`,
		`SELECT * FROM orders WHERE status = 'new'`,
		`SELECT * FROM orders WHERE status = 'done'`,
		`SELECT * FROM orders WHERE created_at > now() - interval '30 days'`,
		`SELECT * FROM orders WHERE amount < 100`,

		// Repeat to generate cache hits (second pass reads from shared buffers)
		`SELECT count(*) FROM orders`,
		`SELECT * FROM orders WHERE user_id = 1`,
		`SELECT * FROM orders WHERE user_id = 2`,
		`SELECT * FROM orders WHERE status = 'new'`,
		`SELECT count(*) FROM users`,
	}

	for _, stmt := range stmts {
		_, _ = pool.Exec(ctx, stmt)
	}

	// Flush stats
	_, _ = pool.Exec(ctx, "SELECT pg_stat_force_next_flush()")

	return nil
}

// generateQueryStats executes queries multiple times to populate pg_stat_statements.
func generateQueryStats(ctx context.Context, pool *pgxpool.Pool) error {
	queries := []string{
		`SELECT count(*) FROM orders WHERE user_id = $1`,
		`SELECT count(*) FROM orders WHERE status = $1`,
		`SELECT * FROM orders ORDER BY created_at DESC LIMIT $1`,
		`SELECT avg(amount) FROM orders WHERE status = $1`,
	}

	args := [][]any{
		{1}, {2}, {3}, {4}, {5},
	}

	for _, q := range queries {
		for _, a := range args {
			_, _ = pool.Exec(ctx, q, a...)
		}
	}

	return nil
}
