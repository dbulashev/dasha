# Schema checks

What `schema_lint` returns, code by code. The API ships codes and numbers, never
prose — the wording is yours to build. Every check reads the system catalog
only, so nothing here depends on user data.

Levels: `error` (fix it), `warning` (fix it soon), `notice` (worth knowing).
The level is assigned from the facts, and thresholds are configurable, so trust
the `level` field over the numbers you remember.

Two fields decide whether a report can be trusted at all:

- `skipped` — checks that did NOT run, with a reason (`insufficient_privileges`,
  `unsupported_version`, `disabled`, `error`). A check listed here proves
  nothing about the schema. Never report "clean" while it is non-empty without
  naming what was skipped.
- `truncated` — a check hit its row cap; counts are a lower bound.

In `schema_lint_summary` the same rule applies per database: `skipped` counts the
checks that did not run there, and `failed` marks a database that could not be
read at all. Zeros mean clean only when both are empty.

`params.partitions` means the finding is already rolled up to the parent table
and covers that many partitions. Act on the parent — PostgreSQL refuses most
per-partition fixes anyway.

## sequence_exhaustion

`params`: `used_pct`, `last_value`, `max_value`, `owned_by`, `owned_column_type`.

Levels by values still free: < 5% error, < 10% warning, < 20% notice. An
`integer` owner column raises the level one step. Operators can move the
thresholds with `schema_lint.sequence_thresholds`; the `sequence_exhaustion`
health rule reads the same setting and sits on the same points, but does not
apply the int4 step — its level can be one below this one. Never state a
threshold you did not read here.

`max_value` is the ceiling that actually applies, not always the sequence's own:
a bigint sequence owned by an `integer` column breaks at 2147483647, when the
column overflows.

First action: widen the sequence (`ALTER SEQUENCE … AS bigint`). If
`owned_column_type` is `integer`, that alone is NOT the fix — the column type
has to change too, which rewrites the table and needs a maintenance window. Say
that instead of quoting only the ALTER SEQUENCE.

## no_primary_key / no_unique_key

`no_primary_key` — no PK, but a usable unique index exists.
`no_unique_key` — neither. Both `error`.

`params.unique_nullable = true` means the unique index covers a nullable column:
it cannot serve as REPLICA IDENTITY, so "you already have a unique index" is
wrong advice for logical replication.

Consequences to quote: logical replication cannot replicate UPDATE/DELETE
without a replica identity, pg_repack refuses the table, and one row of a
duplicated pair cannot be deleted on its own.

First action: ask what the table is for before prescribing. A staging or
append-only table may legitimately have no key.

## public_create_privilege

`params.owner`. `error` on PG 15+, `warning` on PG 14 where the open `public`
schema is the factory default — the exposure is the same, it just was not
chosen.

Any user can create an object that intercepts calls resolved through
`search_path` (CVE-2018-1058).

First action: `REVOKE CREATE ON SCHEMA <schema> FROM PUBLIC;`

## unlogged_relation / unlogged_sequence

`warning`. Contents do not survive a crash and never reach the replicas — on a
standby the table is always empty. Intentional for scratch data; a problem when
someone expects the rows to be there.

First action: `ALTER TABLE … SET LOGGED` (rewrites the table) if the data must
survive.

## uuid_in_non_uuid_type

`params`: `column`, `column_type`. `notice`.

A heuristic on column name and declared length (32/36 chars), NOT a fact — say
it needs verifying. When it holds, `uuid` costs 16 bytes against 36, on the
table and on every index over the column.

## relation_without_fk

`notice`, off unless enabled in the configuration. The table is on neither side
of any foreign key: forgotten, or its relationships live only in application
code. Noise on schemas that avoid foreign keys by design.

## relation_without_columns

`notice`. Every column dropped, or none ever added. Stores nothing, still lands
in dumps.

## reserved_word_in_name / unsafe_chars_in_name

`reserved_word_in_name` (`warning`) — the object name is a reserved SQL keyword:
unquoted statements fail, or parse as something else.
`unsafe_chars_in_name` (`notice`) — the name holds characters outside
`[A-Za-z0-9_]` (dot, space, hyphen, quote, non-ASCII) and must be quoted
everywhere. Upper case is deliberately not flagged: that is naming style.

Only tooling breakage, not style. The fix is a rename, which is a migration —
say so rather than presenting it as free.

## invalid_constraint

`params`: `constraint`, `referenced_by`. `warning`. The constraint was created
NOT VALID: it applies to new rows, but existing data was never checked, and the
planner will not rely on it. What looks like enforced integrity is not.

First action: `ALTER TABLE … VALIDATE CONSTRAINT …` — it reads the whole table
but takes a weak lock and blocks neither reads nor writes.

Same rows as the FK Analysis page: the report is a second view, not a second
source.

## fk_type_mismatch / fk_nullable / fk_similar

Also from the FK Analysis page. `params`: `constraint`, `other_object` (the
referenced table, the covered columns, or the second key).

`fk_type_mismatch` (`warning`) — the two sides of the key have different column
types. Every referential check casts, which can keep the parent-side index out
of the plan. The fix is an `ALTER TABLE` with a rewrite: say that it needs a
window.

`fk_nullable` (`notice`) — the key's columns are nullable, so under MATCH SIMPLE
a row with a NULL passes the check entirely. Only a defect when the relationship
was meant to be mandatory — ask before prescribing NOT NULL.

`fk_similar` (`notice`, off by default) — two keys over overlapping columns to
the same target. A candidate for review, not a verdict.

## index_similar / btree_on_array

From the index analysis. `params`: `index`, `other_object`.

`index_similar` (`notice`, off by default) — two indexes identical once their
definitions are normalized. An index backing a constraint cannot be dropped
directly; the constraint goes first.

`btree_on_array` (`notice`) — a btree over an array column serves only
whole-array comparison. If elements are searched, GIN is the answer; if whole
arrays are compared, the index is correct and the finding is noise.
