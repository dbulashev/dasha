-- PG 19: autovacuum's own verdict per table from pg_stat_autovacuum_scores
-- (system catalogs included, unlike the formula in 190000/), and the running
-- processes split by who started them. Current database only.
WITH q AS (SELECT do_vacuum, do_analyze FROM pg_stat_autovacuum_scores),
     pv AS (SELECT started_by FROM pg_stat_progress_vacuum  WHERE datname = current_database()),
     pa AS (SELECT started_by FROM pg_stat_progress_analyze WHERE datname = current_database())
SELECT
    (SELECT count(*) FILTER (WHERE do_vacuum AND NOT do_analyze) FROM q)::int AS tables_due_vacuum_only,
    (SELECT count(*) FILTER (WHERE do_analyze AND NOT do_vacuum) FROM q)::int AS tables_due_analyze_only,
    (SELECT count(*) FILTER (WHERE do_vacuum AND do_analyze)     FROM q)::int AS tables_due_both,
    (SELECT count(*) FROM q)::int                                             AS tables_total,
    (SELECT count(*) FROM pv)::int                                            AS running_vacuums,
    (SELECT count(*) FROM pa)::int                                            AS running_analyzes,
    (SELECT count(*) FILTER (WHERE started_by = 'manual')                FROM pv)::int AS running_vacuums_manual,
    (SELECT count(*) FILTER (WHERE started_by = 'autovacuum')            FROM pv)::int AS running_vacuums_autovacuum,
    (SELECT count(*) FILTER (WHERE started_by = 'autovacuum_wraparound') FROM pv)::int AS running_vacuums_wraparound,
    (SELECT count(*) FILTER (WHERE started_by = 'manual')                FROM pa)::int AS running_analyzes_manual,
    (SELECT count(*) FILTER (WHERE started_by = 'autovacuum')            FROM pa)::int AS running_analyzes_autovacuum
