#!/bin/sh
# Wraps the postgres image entrypoint: prepares the per-host directory on the
# shared pg-logs volume and appends the jsonlog settings Fluent Bit tails.
set -e

: "${PG_LOG_DIR:=/var/log/postgresql}"

mkdir -p "$PG_LOG_DIR"
chown -R postgres:postgres "$PG_LOG_DIR"
chmod 0750 "$PG_LOG_DIR"

exec docker-entrypoint.sh "$@" \
  -c logging_collector=on \
  -c log_destination=jsonlog \
  -c log_directory="$PG_LOG_DIR" \
  -c log_filename=postgresql-%H.log \
  -c log_rotation_age=60 \
  -c log_rotation_size=0 \
  -c log_truncate_on_rotation=on \
  -c log_timezone=UTC \
  -c log_connections=on \
  -c log_disconnections=on \
  -c log_lock_waits=on \
  -c log_autovacuum_min_duration=0
