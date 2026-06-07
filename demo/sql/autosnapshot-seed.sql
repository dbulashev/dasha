-- Demo-friendly auto-snapshot configuration.
-- Runs after backend has executed migrations and seeded default config row.
-- Low thresholds + activity-drop snapshots so the full spike -> drop cycle
-- visibly triggers within ~1 minute. recovery_duration > 0 enables the
-- activity_drop snapshot; deferred_interval 0s leaves the deferred follow-up off
-- (enable it in the UI to demo the persisted-queue path).
UPDATE autosnapshot_config_global
SET enabled                = true,
    poll_interval          = '5 seconds',
    max_snapshot_frequency = '60 seconds',
    min_baseline_active    = 2,
    defaults = '{
      "activity_spike": {"enabled": true, "window_size": "1m", "active_threshold_pct": 50, "spike_duration": "20s", "recovery_duration": "20s", "deferred_interval": "0s"},
      "role_change":    {"enabled": true, "direction": "both"}
    }'::jsonb,
    updated_by = 'demo-seed'
WHERE id = 1;
