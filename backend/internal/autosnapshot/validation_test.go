package autosnapshot

import (
	"testing"
	"time"
)

// validConfig returns a Config that passes Validate, used as a mutation base.
func validConfig() Config {
	return Config{ //nolint:exhaustruct
		PollInterval:         10 * time.Second,
		MaxSnapshotFrequency: 30 * time.Second,
		RetentionBytes:       0,
		RetentionMinDays:     0,
		MinBaselineActive:    0,
		LockProbeCount:       3,
		LockProbeInterval:    time.Second,
		HotSchedule:          "0 3 * * *",
		HotTopN:              100,
		HotRetentionDays:     180,
		Defaults:             validDefaults(),
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(*Config) {}, false},
		{"poll_interval below floor", func(c *Config) { c.PollInterval = 3 * time.Second }, true},
		{"max_freq below poll", func(c *Config) { c.MaxSnapshotFrequency = 5 * time.Second }, true},
		{"negative retention_bytes", func(c *Config) { c.RetentionBytes = -1 }, true},
		{"negative retention_min_days", func(c *Config) { c.RetentionMinDays = -1 }, true},
		{"negative min_baseline_active", func(c *Config) { c.MinBaselineActive = -1 }, true},
		{"lock_probe_count zero", func(c *Config) { c.LockProbeCount = 0 }, true},
		{"lock_probe_count too high", func(c *Config) { c.LockProbeCount = 21 }, true},
		{"lock_probe_interval too low", func(c *Config) { c.LockProbeInterval = 50 * time.Millisecond }, true},
		{"lock_probe_interval too high", func(c *Config) { c.LockProbeInterval = 6 * time.Second }, true},
		{"window_size zero", func(c *Config) { c.Defaults.ActivitySpike.WindowSize = 0 }, true},
		{"threshold zero", func(c *Config) { c.Defaults.ActivitySpike.ActiveThresholdPct = 0 }, true},
		{"threshold above max", func(c *Config) { c.Defaults.ActivitySpike.ActiveThresholdPct = 10001 }, true},
		{"spike_duration zero", func(c *Config) { c.Defaults.ActivitySpike.SpikeDuration = 0 }, true},
		{"negative recovery_duration", func(c *Config) { c.Defaults.ActivitySpike.RecoveryDuration = -1 }, true},
		{"negative deferred_interval", func(c *Config) { c.Defaults.ActivitySpike.DeferredInterval = -1 }, true},
		{"bad direction", func(c *Config) { c.Defaults.RoleChange.Direction = "sideways" }, true},
		{"hot_top_n zero", func(c *Config) { c.HotTopN = 0 }, true},
		{"hot_top_n too high", func(c *Config) { c.HotTopN = 1001 }, true},
		{"hot_retention_days zero", func(c *Config) { c.HotRetentionDays = 0 }, true},
		{
			name:    "spike_duration at 2x window is allowed",
			mutate:  func(c *Config) { c.Defaults.ActivitySpike.SpikeDuration = 60 * time.Second },
			wantErr: false,
		},
		{
			name:    "spike_duration above 2x window",
			mutate:  func(c *Config) { c.Defaults.ActivitySpike.SpikeDuration = 61 * time.Second },
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tc.mutate(&cfg)

			if err := cfg.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestConfigValidateOverride(t *testing.T) {
	t.Parallel()

	// Defaults carry window_size = 30s, so the cross-field rule caps an
	// overriding spike_duration at 60s.
	cfg := validConfig()

	tests := []struct {
		name     string
		override map[string]any
		wantErr  bool
	}{
		{"nil deletes", nil, false},
		{"empty deletes", map[string]any{}, false},
		{
			name:     "valid partial",
			override: map[string]any{"activity_spike": map[string]any{"enabled": false}},
			wantErr:  false,
		},
		{
			name:     "recovery_duration zero allowed",
			override: map[string]any{"activity_spike": map[string]any{"recovery_duration": "0s"}},
			wantErr:  false,
		},
		{
			name: "consistent window and spike",
			override: map[string]any{"activity_spike": map[string]any{
				"window_size": "5m", "spike_duration": "1m",
			}},
			wantErr: false,
		},
		{
			name:     "valid direction",
			override: map[string]any{"role_change": map[string]any{"direction": "replica_to_master"}},
			wantErr:  false,
		},
		{
			name:     "zero window rejected",
			override: map[string]any{"activity_spike": map[string]any{"window_size": "0s"}},
			wantErr:  true,
		},
		{
			name:     "spike exceeds 2x default window",
			override: map[string]any{"activity_spike": map[string]any{"spike_duration": "5m"}},
			wantErr:  true,
		},
		{
			name:     "malformed duration string",
			override: map[string]any{"activity_spike": map[string]any{"window_size": "garbage"}},
			wantErr:  true,
		},
		{
			name:     "wrong type for enabled",
			override: map[string]any{"activity_spike": map[string]any{"enabled": "yes"}},
			wantErr:  true,
		},
		{
			name:     "non-integer threshold",
			override: map[string]any{"activity_spike": map[string]any{"active_threshold_pct": 50.5}},
			wantErr:  true,
		},
		{
			name:     "bad direction rejected",
			override: map[string]any{"role_change": map[string]any{"direction": "sideways"}},
			wantErr:  true,
		},
		{
			name:     "unknown top-level key rejected",
			override: map[string]any{"bogus": 1},
			wantErr:  true,
		},
		{
			name:     "unknown nested key rejected",
			override: map[string]any{"activity_spike": map[string]any{"bogus": 1}},
			wantErr:  true,
		},
		{
			name:     "null field rejected",
			override: map[string]any{"activity_spike": map[string]any{"window_size": nil}},
			wantErr:  true,
		},
		{
			name:     "null block rejected",
			override: map[string]any{"role_change": nil},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := cfg.ValidateOverride(tc.override); (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestConfigValidateEffective(t *testing.T) {
	t.Parallel()

	// Valid under a 30s default window (50s <= 2*30s), invalid once it tightens.
	override := map[string]any{"activity_spike": map[string]any{"spike_duration": "50s"}}

	wide := validConfig()
	if err := wide.ValidateEffective(override); err != nil {
		t.Fatalf("override should hold under wide defaults: %v", err)
	}

	tight := validConfig()
	tight.Defaults.ActivitySpike.WindowSize = 20 * time.Second // 2*20s = 40s < 50s

	if err := tight.ValidateEffective(override); err == nil {
		t.Fatal("override should be rejected once the default window is tightened")
	}
}
