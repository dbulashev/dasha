SELECT
    sum(idx_blks_hit) / nullif(sum(idx_blks_hit + idx_blks_read), 0) AS rate
FROM
    pg_statio_user_indexes
