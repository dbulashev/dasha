#!/bin/bash
set -e

echo "Waiting for pg18-master..."
until pg_isready -h pg18-master -p 5432 -U demo -d demo; do sleep 2; done

echo "Waiting for pg17-master..."
until pg_isready -h pg17-master -p 5432 -U demo -d demo; do sleep 2; done

echo "=== Starting continuous pgbench load ==="
pgbench -h pg18-master -U demo -d demo -c 4 -j 2 -T 0 -P 60 &
pgbench -h pg17-master -U demo -d demo -c 2 -j 1 -T 0 -P 60 &

# --- Continuous blocking loop (separate background process) ---
echo "=== Starting continuous lock generator ==="
(
  while true; do
    # Hold lock for 25 seconds, blocker waits
    psql -h pg18-master -U demo -d demo -c \
      "BEGIN; SELECT * FROM orders WHERE id = 1 FOR UPDATE; SELECT pg_sleep(25); COMMIT;" &
    LOCK_PID=$!
    sleep 2
    # Second connection tries same row — will block for ~23 seconds
    psql -h pg18-master -U demo -d demo -c \
      "SELECT * FROM orders WHERE id = 1 FOR UPDATE;" &
    wait $LOCK_PID 2>/dev/null || true
    wait 2>/dev/null || true
    sleep 3
  done
) &

# --- Continuous long-running queries (separate background process) ---
echo "=== Starting long-running query generator ==="
(
  while true; do
    psql -h pg18-master -U demo -d demo -c \
      "SELECT pg_sleep(20), count(*) FROM orders o1 CROSS JOIN generate_series(1, 100);" 2>/dev/null
    sleep 5
  done
) &

# --- Deadlock generator for Database Health demo ---
echo "=== Starting deadlock generator ==="
(
  while true; do
    # Two transactions updating rows in opposite order → guaranteed deadlock
    psql -h pg18-master -U demo -d demo -c \
      "BEGIN; UPDATE orders SET amount = amount + 0.01 WHERE id = 2; SELECT pg_sleep(2); UPDATE orders SET amount = amount - 0.01 WHERE id = 3; COMMIT;" 2>/dev/null &
    PID_A=$!
    sleep 0.5
    psql -h pg18-master -U demo -d demo -c \
      "BEGIN; UPDATE orders SET amount = amount + 0.01 WHERE id = 3; SELECT pg_sleep(2); UPDATE orders SET amount = amount - 0.01 WHERE id = 2; COMMIT;" 2>/dev/null &
    PID_B=$!
    wait $PID_A 2>/dev/null || true
    wait $PID_B 2>/dev/null || true
    sleep 30
  done
) &

echo "=== Starting workload loop ==="
while true; do
  # 1. Dead rows generation (visible in Maintenance Info)
  psql -h pg17-master -U demo -d demo -c "
    DELETE FROM deadrows_test WHERE id IN (SELECT id FROM deadrows_test ORDER BY random() LIMIT 50);
    INSERT INTO deadrows_test (data) SELECT 'new_' || i FROM generate_series(1, 50) i;
  "

  # 2. Index bloat generation (visible in Indexes Bloat)
  psql -h pg18-master -U demo -d demo -c \
    "UPDATE orders SET amount = amount + 0.01 WHERE id BETWEEN 1 AND 500;"

  # 3. Diverse queries for pg_stat_statements variety (visible in Top10/Report)
  psql -h pg17-master -U demo -d demo <<'SQL'
    SELECT count(*) FROM orders WHERE status = 'new';
    SELECT avg(amount), max(amount) FROM orders WHERE created_at > now() - interval '30 days';
    SELECT user_id, count(*) FROM orders GROUP BY user_id ORDER BY count(*) DESC LIMIT 10;
    SELECT * FROM products p JOIN categories c ON c.id = p.category_id LIMIT 5;
    SELECT * FROM events WHERE event_date > '2025-06-01' LIMIT 10;
SQL

  psql -h pg18-master -U demo -d demo <<'SQL'
    SELECT count(*) FROM orders WHERE user_id BETWEEN 1 AND 100;
    SELECT status, count(*), avg(amount) FROM orders GROUP BY status;
    SELECT * FROM orders WHERE amount > 9000 ORDER BY created_at DESC LIMIT 20;
SQL

  echo "[$(date)] Workload cycle completed"
  sleep 15
done
