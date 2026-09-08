#!/bin/bash
set -e

: "${PRIMARY_HOST:?PRIMARY_HOST env required}"
: "${REPLICATION_SLOT:?REPLICATION_SLOT env required}"

echo "Waiting for primary ${PRIMARY_HOST}..."
until pg_isready -h "${PRIMARY_HOST}" -p 5432 -U demo; do
  sleep 1
done

echo "Starting pg_basebackup from ${PRIMARY_HOST}..."
rm -rf /var/lib/postgresql/data/*

PGPASSWORD=demo pg_basebackup \
  --host="${PRIMARY_HOST}" --port=5432 \
  --username=demo \
  --pgdata=/var/lib/postgresql/data \
  --wal-method=stream \
  --write-recovery-conf \
  --slot="${REPLICATION_SLOT}" \
  --checkpoint=fast -R

: "${PG_LOG_DIR:=/var/log/postgresql}"

cat >> /var/lib/postgresql/data/postgresql.auto.conf <<EOF
hot_standby = on
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
logging_collector = on
log_destination = 'jsonlog'
log_directory = '${PG_LOG_DIR}'
log_filename = 'postgresql-%H.log'
log_rotation_age = 60
log_rotation_size = 0
log_truncate_on_rotation = on
log_timezone = 'UTC'
log_connections = on
log_disconnections = on
log_lock_waits = on
EOF

mkdir -p "${PG_LOG_DIR}"
chown -R postgres:postgres "${PG_LOG_DIR}"
chmod 0750 "${PG_LOG_DIR}"

chown -R postgres:postgres /var/lib/postgresql/data
chmod 0700 /var/lib/postgresql/data

echo "Starting replica..."
exec gosu postgres postgres -D /var/lib/postgresql/data
