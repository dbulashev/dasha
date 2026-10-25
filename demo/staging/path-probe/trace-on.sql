-- Switch this session to full pathprobe coverage.
--
-- Run it first in every session that is probed: the make targets do, and
-- from an interactive psql use \i /path-probe/trace-on.sql.  After that
--
--   SELECT pathprobe('SELECT ...');               text report, for people
--   SELECT pathprobe_json('SELECT ...')::jsonb;   events, summary, coverage
--   EXPLAIN SELECT ...;                           plan + PathProbe section
--
-- The demo user is a superuser (POSTGRES_USER), which the SUSET settings
-- below need.

-- Keep the "already exists" notice out of the written reports.
SET client_min_messages = warning;

CREATE EXTENSION IF NOT EXISTS pathprobe;

-- Load the library before the SETs, so they hit the real GUCs rather than
-- placeholders.
LOAD 'pathprobe';

-- accepted/displaced events are off by default (high volume): without them a
-- path that was accepted and later pushed out leaves no trace, which is the
-- case the demo is about.
SET pathprobe.log_accepted = on;

-- Report every rejection, however small the cost gap (0 is the default; set
-- explicitly so the coverage does not depend on someone's postgresql.conf).
SET pathprobe.min_gap = 0;

-- Plain EXPLAIN gets a PathProbe section too.
SET pathprobe.explain = on;

-- pathprobe does not observe partial paths: keep parallel plans out of the
-- search, or part of it is invisible.
SET max_parallel_workers_per_gather = 0;
