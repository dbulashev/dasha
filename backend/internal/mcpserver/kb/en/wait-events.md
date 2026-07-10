# Wait events glossary

How to read `wait_events` output: backends with no wait event are running on
CPU — that is fine. A single dominant event across many backends is the signal.
Interpret by class first, then by event.

## Classes

- **Lock** — heavyweight locks (rows, tables): another transaction holds a
  conflicting lock. Always drill with `blocked_queries`.
- **LWLock** — internal shared-memory locks: contention inside PostgreSQL
  (buffers, WAL, lock manager) — usually a throughput/config issue.
- **IO** — waiting on disk reads/writes.
- **Client** — waiting on the client application (slow consumer, network,
  idle connections).
- **IPC** — waiting on another backend (parallel query workers, extensions).
- **Timeout** — deliberate sleeps (vacuum cost delay, recovery settings).
- **BufferPin** — waiting for a buffer pin to be released (often a cursor or
  vacuum vs long reader).

## Frequent events and what to do

### Lock:transactionid
Row-level contention: a transaction waits for another one that updated the
same row. Next: `blocked_queries` — find the blocker; typical cause is a long
or idle-in-transaction session.

### Lock:relation
Table-level lock conflict, usually DDL (ALTER/CREATE INDEX without
CONCURRENTLY, LOCK TABLE) vs normal DML. Next: `blocked_queries`,
`running_queries` — find and finish/kill the DDL holder.

### Lock:tuple
Queue on one hot row (many writers to the same row). Next: `top_queries` —
identify the hot-row pattern (counters, queues); consider batching or
redesign.

### LWLock:WALWrite / IO:WALSync / IO:WALWrite
WAL writing is the bottleneck: commit-heavy load or slow WAL disk. Check WAL
volume latency/IOPS (dedicated volume?), synchronous_commit, commit rate
(batch small transactions), checkpoint frequency (`get_health_score` →
wal_checkpoint; `top_queries` by=wal for WAL-heavy statements).

### IO:DataFileRead
Reads go to disk: working set exceeds cache or seq scans dominate.
Next: `top_queries` (disk-heavy), `list_indexes` (kind=usage/missing);
consider shared_buffers.

### LWLock:BufferMapping / LWLock:BufferIO
Buffer cache churn — many backends evicting/loading pages concurrently.
Usually accompanies IO:DataFileRead; same drill-down.

### LWLock:LockManager
Lock manager contention: very many locks taken per transaction (thousands of
partitions/tables touched) or extreme connection counts. Next: `connections`;
check partition-heavy queries; consider raising max_locks_per_transaction.

### Client:ClientRead / Client:ClientWrite
Server waits for the application to read/send data: slow client, network, or
big result sets. Many ClientRead sessions inside a transaction = the
idle-in-transaction pattern (see health rule idle_in_transaction).

### IPC:ParallelFinish / IPC:*
Coordination of parallel query workers; only a problem when it dominates —
then inspect the parallel-heavy statements in `top_queries`.

### Timeout:VacuumDelay
Autovacuum sleeping due to cost limiting — harmless by itself, but if
maintenance category is degraded, raise autovacuum_vacuum_cost_limit.

### BufferPin:BufferPin
VACUUM (or hot pruning) waits for a long-running reader holding a buffer pin —
usually a cursor or a very long SELECT. Next: `running_queries`.
