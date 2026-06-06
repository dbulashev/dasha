-- Demo-friendly auto-snapshot configuration.
-- Runs after backend has executed migrations and seeded default config row.
-- Shortens intervals so spikes visibly trigger snapshots within ~1-2 minutes.
UPDATE autosnapshot_config_global
SET enabled                = true,
    poll_interval          = '10 seconds',
    max_snapshot_frequency = '90 seconds',
    min_baseline_active    = 2,
    defaults = '{
      "activity_spike": {"enabled": true, "window_size": "1m", "active_threshold_pct": 100, "spike_duration": "20s"},
      "role_change":    {"enabled": true, "direction": "both"}
    }'::jsonb,
    updated_by = 'demo-seed'
WHERE id = 1;
