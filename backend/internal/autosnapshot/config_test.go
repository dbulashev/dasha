package autosnapshot

import (
	"testing"
	"time"
)

// validDefaults returns a TriggerDefaults that passes validation, used as the
// merge baseline in tests.
func validDefaults() TriggerDefaults {
	return TriggerDefaults{
		ActivitySpike: ActivitySpikeTrigger{
			Enabled:            true,
			WindowSize:         30 * time.Second,
			ActiveThresholdPct: 80,
			SpikeDuration:      10 * time.Second,
			RecoveryDuration:   0,
			DeferredInterval:   0,
		},
		RoleChange: RoleChangeTrigger{
			Enabled:   false,
			Direction: DirectionBoth,
		},
	}
}

func TestDurationUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"valid", `"1m30s"`, 90 * time.Second, false},
		{"zero", `"0s"`, 0, false},
		{"not a string", `90`, 0, true},
		{"bad duration", `"garbage"`, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var d Duration
			err := d.UnmarshalJSON([]byte(tc.input))

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if err == nil && time.Duration(d) != tc.want {
				t.Fatalf("got %v, want %v", time.Duration(d), tc.want)
			}
		})
	}
}

func TestDurationMarshalJSON(t *testing.T) {
	t.Parallel()

	b, err := Duration(90 * time.Second).MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := string(b), `"1m30s"`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEffectiveFor(t *testing.T) {
	t.Parallel()

	cfg := Config{Defaults: validDefaults()} //nolint:exhaustruct

	tests := []struct {
		name     string
		override map[string]any
		want     func() TriggerDefaults
	}{
		{
			name:     "nil override inherits defaults",
			override: nil,
			want:     validDefaults,
		},
		{
			name:     "empty override inherits defaults",
			override: map[string]any{},
			want:     validDefaults,
		},
		{
			name: "partial override touches only set fields",
			override: map[string]any{
				"activity_spike": map[string]any{"window_size": "1m"},
			},
			want: func() TriggerDefaults {
				d := validDefaults()
				d.ActivitySpike.WindowSize = time.Minute

				return d
			},
		},
		{
			name: "numeric and bool fields",
			override: map[string]any{
				"activity_spike": map[string]any{
					"enabled":              false,
					"active_threshold_pct": 50,
					"recovery_duration":    "5m",
				},
			},
			want: func() TriggerDefaults {
				d := validDefaults()
				d.ActivitySpike.Enabled = false
				d.ActivitySpike.ActiveThresholdPct = 50
				d.ActivitySpike.RecoveryDuration = 5 * time.Minute

				return d
			},
		},
		{
			name: "role_change direction only",
			override: map[string]any{
				"role_change": map[string]any{"direction": "master_to_replica"},
			},
			want: func() TriggerDefaults {
				d := validDefaults()
				d.RoleChange.Direction = DirectionMasterToReplica

				return d
			},
		},
		{
			name: "malformed duration drops the whole override",
			override: map[string]any{
				"activity_spike": map[string]any{"window_size": "garbage"},
			},
			want: validDefaults,
		},
		{
			name: "unknown keys are ignored on the read path",
			override: map[string]any{
				"activity_spike": map[string]any{"unknown": 1, "window_size": "1m"},
			},
			want: func() TriggerDefaults {
				d := validDefaults()
				d.ActivitySpike.WindowSize = time.Minute

				return d
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := cfg.EffectiveFor(tc.override)
			if want := tc.want(); got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}
