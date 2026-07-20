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

# --- Activity spike generator for auto-snapshot demo ---
# Fires ~every 2 minutes: opens ~20 short concurrent busy queries on pg18-master
# so that GetActiveConnectionCount crosses the threshold over baseline for
# spike_duration, which autosnapshot detects and snapshots.
# Set SPIKE_GENERATOR=off to disable it (keeps the steady baseline) for a clean
# controlled test via demo/scripts/spike-test.sh.
if [ "${SPIKE_GENERATOR:-on}" != "off" ]; then
  echo "=== Starting activity spike generator ==="
  (
    sleep 90
    while true; do
      echo "[$(date)] firing activity spike on pg18-master"
      for i in $(seq 1 20); do
        psql -h pg18-master -U demo -d demo -c \
          "SELECT pg_sleep(25), count(*) FROM generate_series(1, 1000000);" 2>/dev/null &
      done
      wait
      sleep 120
    done
  ) &
else
  echo "=== Activity spike generator disabled (SPIKE_GENERATOR=off) ==="
fi

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

# Disable autovacuum on customer_profiles to accumulate dead tuples for demo
psql -h pg18-master -U demo -d demo -c \
  "ALTER TABLE customer_profiles SET (autovacuum_enabled = false);"
psql -h pg17-master -U demo -d demo -c \
  "ALTER TABLE customer_profiles SET (autovacuum_enabled = false);" 2>/dev/null || true

while true; do
  # 1. Dead rows generation (visible in Maintenance Info)
  psql -h pg17-master -U demo -d demo -c "
    DELETE FROM deadrows_test WHERE id IN (SELECT id FROM deadrows_test ORDER BY random() LIMIT 50);
    INSERT INTO deadrows_test (data) SELECT 'new_' || i FROM generate_series(1, 50) i;
  "

  # 2. Index bloat generation (visible in Indexes Bloat)
  psql -h pg18-master -U demo -d demo -c \
    "UPDATE orders SET amount = amount + 0.01 WHERE id BETWEEN 1 AND 500;"

  # 3. Dead tuples in customer_profiles (visible in Vacuum Stats thresholds)
  psql -h pg18-master -U demo -d demo -c "
    UPDATE customer_profiles SET updated_at = now(), notes = 'updated_' || id
    WHERE id IN (SELECT id FROM customer_profiles ORDER BY random() LIMIT 100);
    DELETE FROM customer_profiles
    WHERE id IN (SELECT id FROM customer_profiles ORDER BY random() LIMIT 20);
    INSERT INTO customer_profiles (first_name, last_name, email, bio, preferences)
    SELECT 'New_' || i, 'User_' || i,
           'new' || i || '_' || extract(epoch from now())::int || '@example.com',
           repeat('Fresh data. ', 30),
           '{\"theme\": \"dark\"}'::jsonb
    FROM generate_series(1, 20) i;
  "

  # 4. HOT updates on hot_update_demo (visible in Row Estimate / fillfactor demo)
  psql -h pg18-master -U demo -d demo -c "
    UPDATE hot_update_demo SET counter = counter + 1, last_ping = now()
    WHERE id IN (SELECT id FROM hot_update_demo ORDER BY random() LIMIT 200);
  "

  # 5. Diverse queries for pg_stat_statements variety (visible in Top10/Report)
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
    SELECT * FROM customer_profiles WHERE bio IS NOT NULL ORDER BY random() LIMIT 5;
SQL

  # 6. Hash / range→hash partition load (hot-objects rollup demo)
  #    Reads + writes spread across every partition, so the hot-objects top must
  #    roll the leaves up into sensor_readings and metrics_2026_0N.
  psql -h pg18-master -U demo -d demo -c "
    INSERT INTO sensor_readings (sensor_id, reading)
      SELECT (random()*10000)::int, (random()*100)::numeric(10,2) FROM generate_series(1, 3000);
    SELECT count(*), avg(reading) FROM sensor_readings WHERE sensor_id BETWEEN 1 AND 4000;
    SELECT sensor_id, count(*) FROM sensor_readings GROUP BY sensor_id ORDER BY count(*) DESC LIMIT 10;
    INSERT INTO metrics (bucket, metric_date, value)
      SELECT (random()*1000)::int, '2026-01-01'::date + (random()*58)::int, random()*1000 FROM generate_series(1, 1500);
    SELECT metric_date, count(*), avg(value) FROM metrics GROUP BY metric_date;
    SELECT count(*) FROM metrics WHERE bucket BETWEEN 1 AND 300;
  "

  echo "[$(date)] Workload cycle completed"
  sleep 15
done
