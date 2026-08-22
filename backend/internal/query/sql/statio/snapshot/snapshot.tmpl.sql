SELECT
    s.backend_type,
    s.object,
    s.context,
    s.stats_reset,
    current_setting('track_io_timing')::boolean AS track_io_timing,
    NULL::int                                   AS op_bytes,
    jsonb_strip_nulls(jsonb_build_object(
        'reads',          s.reads,
        'read_bytes',     s.read_bytes,
        'read_time',      round(s.read_time)::bigint,
        'writes',         s.writes,
        'write_bytes',    s.write_bytes,
        'write_time',     round(s.write_time)::bigint,
        'writebacks',     s.writebacks,
        'writeback_time', round(s.writeback_time)::bigint,
        'extends',        s.extends,
        'extend_bytes',   s.extend_bytes,
        'extend_time',    round(s.extend_time)::bigint,
        'hits',           s.hits,
        'evictions',      s.evictions,
        'reuses',         s.reuses,
        'fsyncs',         s.fsyncs,
        'fsync_time',     round(s.fsync_time)::bigint
    )) AS counters
FROM pg_stat_io s
