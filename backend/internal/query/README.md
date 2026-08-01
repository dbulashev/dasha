# SQL templates — third-party attribution

Query templates in this tree are Dasha's own unless listed below. This file is
the single place where borrowed SQL is credited; the templates themselves carry
no licence headers.

## db_verifier

- Project: https://github.com/sdblist/db_verifier
- Licence: MIT, © Nikonov, 2024
- Used in: `schema_lint/*` — the structural checks are derived from db_verifier's
  check set. The logic is adapted to Dasha: severity is assigned in Go from the
  facts a query returns instead of being baked into separate per-severity
  queries, partition rollup happens in post-processing, and system schemas are
  filtered uniformly.

Mapping of Dasha templates to the original checks:

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
| `schema_lint/partition_roots` | — (Dasha's own, feeds the partition rollup) |

Checks that live outside the `schema_lint` tree — they power pages of their own and
feed the schema report through the registry rather than being reimplemented — come
from the same project: `constraints/invalid_constraints` (`c1001`), `fks/type_mismatch`
(`fk1001`), `fks/possible_nulls` (`fk1002`), `fks/possible_similar1`,
`fks/possible_similar2` (`fk1010`, `fk1011`), `indexes/similar_1`,
`indexes/similar_2`, `indexes/similar_3`, `indexes/duplicate` (`i1001`, `i1003`,
`i1005`), `indexes/btree_on_array` (`i1010`).
