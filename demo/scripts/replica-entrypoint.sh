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

cat >> /var/lib/postgresql/data/postgresql.auto.conf <<EOF
hot_standby = on
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
EOF

chown -R postgres:postgres /var/lib/postgresql/data
chmod 0700 /var/lib/postgresql/data

echo "Starting replica..."
exec gosu postgres postgres -D /var/lib/postgresql/data
