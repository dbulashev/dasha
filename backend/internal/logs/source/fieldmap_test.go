package source

import (
	"errors"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
)

func TestFieldMapFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.LogFieldMapConfig
		wantErr bool
		check   func(t *testing.T, fm FieldMap)
	}{
		{
			name: "preset filled in, timestamp and host from config",
			cfg: config.LogFieldMapConfig{
				Preset:    PresetJSONLog,
				Timestamp: "@timestamp",
				Host:      "host.name",
			},
			check: func(t *testing.T, fm FieldMap) {
				t.Helper()

				if fm.Text != "message" || fm.Database != "dbname" || fm.User != "user" {
					t.Errorf("preset roles not applied: %+v", fm)
				}

				if fm.HostMatch != HostMatchExact {
					t.Errorf("HostMatch = %q, want %q", fm.HostMatch, HostMatchExact)
				}
			},
		},
		{
			name: "explicit field overrides the preset",
			cfg: config.LogFieldMapConfig{
				Preset:    PresetJSONLog,
				Timestamp: "@timestamp",
				Host:      "host.name",
				Database:  "db",
				Mask:      []string{"msg"},
			},
			check: func(t *testing.T, fm FieldMap) {
				t.Helper()

				if fm.Database != "db" {
					t.Errorf("Database = %q, want db", fm.Database)
				}

				if len(fm.Mask) != 1 || fm.Mask[0] != "msg" {
					t.Errorf("Mask = %v, want [msg]", fm.Mask)
				}
			},
		},
		{
			name: "no preset, every role explicit",
			cfg: config.LogFieldMapConfig{
				Preset:     PresetNone,
				Timestamp:  "@timestamp",
				Severity:   "level",
				Text:       "msg",
				Host:       "host",
				Severities: []string{"error"},
			},
			check: func(t *testing.T, fm FieldMap) {
				t.Helper()

				if got, ok := fm.CanonicalSeverity("ERROR"); !ok || got != "error" {
					t.Errorf("CanonicalSeverity(ERROR) = %q, %v; want error, true", got, ok)
				}
			},
		},
		{
			name:    "missing timestamp and host",
			cfg:     config.LogFieldMapConfig{Preset: PresetJSONLog},
			wantErr: true,
		},
		{
			name: "no severity values",
			cfg: config.LogFieldMapConfig{
				Timestamp: "@timestamp",
				Severity:  "level",
				Text:      "msg",
				Host:      "host",
			},
			wantErr: true,
		},
		{
			name: "unknown preset",
			cfg: config.LogFieldMapConfig{
				Preset:    "syslog",
				Timestamp: "@timestamp",
				Host:      "host",
			},
			wantErr: true,
		},
		{
			name: "unknown host match",
			cfg: config.LogFieldMapConfig{
				Preset:    PresetJSONLog,
				Timestamp: "@timestamp",
				Host:      "host",
				HostMatch: "regex",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fm, err := FieldMapFromConfig(tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", fm)
				}

				if !errors.Is(err, ErrConfig) {
					t.Fatalf("error %v does not wrap ErrConfig", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tt.check(t, fm)
		})
	}
}

func TestFieldMapFromConfigDoesNotMutatePreset(t *testing.T) {
	t.Parallel()

	cfg := config.LogFieldMapConfig{
		Preset:    PresetCSVLog,
		Timestamp: "ts",
		Host:      "host",
		Mask:      []string{"only"},
	}

	if _, err := FieldMapFromConfig(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fm, _ := Preset(PresetCSVLog)
	if len(fm.Mask) != 6 {
		t.Errorf("preset mask was modified: %v", fm.Mask)
	}
}

func TestCanonicalSeverityIgnoresCase(t *testing.T) {
	t.Parallel()

	fm, _ := Preset(PresetCSVLog)

	got, ok := fm.CanonicalSeverity("error")
	if !ok || got != "ERROR" {
		t.Errorf("CanonicalSeverity(error) = %q, %v; want ERROR, true", got, ok)
	}

	if _, ok := fm.CanonicalSeverity("verbose"); ok {
		t.Error("CanonicalSeverity accepted an unknown value")
	}
}
