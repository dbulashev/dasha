# SQL templates — third-party attribution

Query templates in this tree are Dasha's own unless listed below. This file is
the single place where borrowed SQL is credited; the templates themselves carry
no licence headers.

## db_verifier

- Project: https://github.com/sdblist/db_verifier
- Licence: MIT, © 2024 Nikonov — full text in the project's `LICENSE` file
- The checks are derived from db_verifier's check set. The logic is adapted to
  Dasha: severity is assigned in Go from the facts a query returns instead of
  being baked into separate per-severity queries, partition rollup happens in
  post-processing, and system schemas are filtered uniformly.

Every borrowed template and the original check it comes from. The ones outside
the `schema_lint` tree power pages of their own and reach the schema report
through the registry rather than being reimplemented there.

| Template | db_verifier checks |
|---|---|
| `schema_lint/sequences_usage` | `s1010`, `s1011`, `s1012` |
| `schema_lint/relations_without_key` | `no1001`, `no1002` |
| `schema_lint/public_create_privileges` | `pg0002` |
| `schema_lint/unlogged_objects` | `r1001`, `s1001` |
| `schema_lint/uuid_like_columns` | `sm0001` |
| `schema_lint/relations_without_fk` | `fk1007` |
| `schema_lint/relations_without_columns` | `r1002` |
| `schema_lint/unsafe_names` | subset of `n1001`–`n1026` (keyword and quoting cases only) |
| `fks/type_mismatch` | `fk1001` |
| `fks/possible_nulls` | `fk1002` |
| `fks/possible_similar1` | `fk1010` |
| `fks/possible_similar2` | `fk1011` |
| `indexes/similar_1` | `i1001` |
| `indexes/similar_2` | `i1003` |
| `indexes/similar_3` | `i1005` |
| `indexes/btree_on_array` | `i1010` |

## pghero

- Project: https://github.com/ankane/pghero
- Licence: MIT, © 2014-2026 Andrew Kane, 2008-2014 Heroku (initial queries) —
  full text in [LICENSE-pghero](LICENSE-pghero)
- The templates below are derived from pghero's SQL. Common adaptations: pgx
  placeholders and pagination, `COALESCE` for nullable columns, exclusion of
  relations held under `AccessExclusiveLock`, numeric percentages instead of
  the `'Insufficient data'` string.

| Template | pghero method |
|---|---|
| `indexes/bloat` | `index_bloat` |
| `indexes/caching` | `index_caching` |
| `indexes/hit_rate` | `index_hit_rate` |
| `indexes/missing` | `missing_indexes` |
| `indexes/usage` | `index_usage` |
| `indexes/unused` | `unused_indexes` |
| `indexes/all_scans` | `unused_indexes` (without the scan threshold) |
| `tables/hit_rate` | `table_hit_rate` |
| `maintenance/info` | `maintenance_info` |
| `maintenance/transaction_id_danger` | `transaction_id_danger` |
| `maintenance/vacuum_progress` | `vacuum_progress` |
| `database/stats_reset_time` | `last_stats_reset_time` |
| `queries/running` | `running_queries` |
| `queries/blocked` | `blocked_queries` |
| `connections/connection_states` | `connection_states` |
| `connections/connection_sources` | `connection_sources` |
| `connections/stat_activity` | `connections` |
| `replication/lag` | `replication_lag` |
| `constraints/invalid_constraints` | `invalid_constraints` |

## postgres_dba

- Project: https://github.com/NikolayS/postgres_dba
- Licence: BSD 3-Clause, © 2017 Nikolay Samokhvalov — full text in
  [LICENSE-postgres_dba](LICENSE-postgres_dba)
- Used in: `locks/tree` — derived from `sql/l2_lock_trees.sql`. The same
  `activity` / `blockers` / `tree` recursion over `pg_blocking_pids()`, with the
  psql `\if` version branches replaced by a single statement (the PG14+
  `pg_locks.waitstart` wait age is read unconditionally, since Dasha supports
  PG14+), the output narrowed to the columns the page renders, and the query text
  truncated at 500 characters. The same query is explained in
  https://postgres.ai/blog/20211018-postgresql-lock-trees.

## pgx_scripts

- Project: https://github.com/pgexperts/pgx_scripts
- Licence: BSD 3-Clause, © 2014 PostgreSQL Experts, Inc. and additional
  contributors — full text in [LICENSE-pgx_scripts](LICENSE-pgx_scripts)
- Used in: `tables/top_k_by_size` — the table bloat estimate (the `constants` …
  `bloat_data` CTE chain) is derived from `bloat/table_bloat_check.sql`. It is
  narrowed to the top-N tables, reads `{{ .PgStatsView }}` and `pg_attribute`
  instead of `information_schema.columns`, and returns bloat per table instead
  of filtering by the script's thresholds.

## ioguix pgsql-bloat-estimation

- Project: https://github.com/ioguix/pgsql-bloat-estimation
- Licence: BSD-style (the PostgreSQL licence), © 2015-2019 Jehan-Guillaume
  (ioguix) de Rorthais — full text in the project's `LICENSE` file
- Used in: `indexes/bloat` — the statistics-based btree bloat estimation
  originates there and reaches Dasha through pghero (see above), which took it
  from a gist by Josh Berkus and Michael Banck.
