#!/bin/bash
# Generate an on-demand activity-spike burst against the demo lab to exercise the
# auto-snapshot activity_spike / activity_drop / deferred triggers.
#
# Runs pgbench inside the workload-generator container ON TOP OF the steady
# baseline load (do NOT pause workload-generator — the spike needs a live
# baseline). Keep the burst longer than the configured spike_duration, otherwise
# the spike is never sustained long enough to fire.
#
# The demo ships low thresholds + activity-drop snapshots out of the box
# (see demo/sql/autosnapshot-seed.sql: poll 5s, window 1m, spike_duration 20s,
# recovery_duration 20s, threshold 50%, min_baseline 2). The daemon re-reads
# config every tick, so just run this script: the spike snapshot fires
# ~spike_duration into the burst, the drop snapshot ~recovery_duration after
# pgbench finishes. Tune live in the UI (Auto-snapshots -> Settings) if needed.
#
# Usage:
#   demo/scripts/spike-test.sh [clients] [duration_sec] [host]
#   CLIENTS=30 DURATION=180 TARGET=pg18-master THREADS=4 demo/scripts/spike-test.sh
set -euo pipefail

CLIENTS="${1:-${CLIENTS:-30}}"
DURATION="${2:-${DURATION:-180}}"
TARGET="${3:-${TARGET:-pg18-master}}"
THREADS="${THREADS:-4}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE="$SCRIPT_DIR/../docker-compose.yaml"

echo "Activity-spike burst: ${CLIENTS} clients, ${DURATION}s, target ${TARGET}"
echo "Watch: docker compose -f ${COMPOSE} logs -f autosnapshot"

docker compose -f "$COMPOSE" exec workload-generator \
  pgbench -h "$TARGET" -U demo -d demo -c "$CLIENTS" -j "$THREADS" -T "$DURATION"
