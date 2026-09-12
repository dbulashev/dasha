package source

import (
	"errors"
	"slices"
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

				if !slices.Equal(fm.Mask, []string{"message", "msg"}) {
					t.Errorf("Mask = %v, want [message msg]", fm.Mask)
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

func TestFieldMapMasksTheTextFieldWithoutAMaskList(t *testing.T) {
	t.Parallel()

	fm, err := FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:     PresetNone,
		Timestamp:  "@timestamp",
		Severity:   "level",
		Text:       "msg",
		Host:       "host",
		Severities: []string{"error"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(fm.Mask, "msg") {
		t.Errorf("Mask = %v, want the text field", fm.Mask)
	}
}

func TestFieldMapMaskCoversTheTextFieldsNesting(t *testing.T) {
	t.Parallel()

	fm, err := FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:    PresetJSONLog,
		Timestamp: "@timestamp",
		Text:      "pg.message",
		Host:      "host.name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"pg.message", "pg.detail", "detail"} {
		if !slices.Contains(fm.Mask, want) {
			t.Errorf("Mask = %v, want it to cover %q", fm.Mask, want)
		}
	}
}

func TestKeywordFallsBackToTheFieldItself(t *testing.T) {
	t.Parallel()

	fm, err := FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:        PresetJSONLog,
		Timestamp:     "@timestamp",
		Host:          "host.name",
		KeywordFields: map[string]string{"error_severity": "error_severity.keyword"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := fm.Keyword("error_severity"); got != "error_severity.keyword" {
		t.Errorf("Keyword(error_severity) = %q", got)
	}

	if got := fm.Keyword("host.name"); got != "host.name" {
		t.Errorf("Keyword(host.name) = %q, want the field itself", got)
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

func TestPgBouncerPresetKeepsTheNativeLevels(t *testing.T) {
	t.Parallel()

	fm, ok := Preset(PresetPgBouncer)
	if !ok {
		t.Fatal("pgbouncer preset is missing")
	}

	if got, canon := fm.CanonicalSeverity("noise"); !canon || got != "NOISE" {
		t.Errorf("CanonicalSeverity(noise) = %q, %v; want NOISE, true", got, canon)
	}

	if _, canon := fm.CanonicalSeverity("INFO"); canon {
		t.Error("CanonicalSeverity(INFO) accepted; pgbouncer has no INFO level")
	}
}

func TestPostgreSQLPresetsBindTheQueryID(t *testing.T) {
	t.Parallel()

	for _, name := range []string{PresetCSVLog, PresetJSONLog} {
		fm, ok := Preset(name)
		if !ok {
			t.Fatalf("preset %q is missing", name)
		}

		if fm.QueryID != "query_id" {
			t.Errorf("%s: QueryID = %q, want query_id", name, fm.QueryID)
		}

		if fm.Roles()[RoleQueryID] != "query_id" {
			t.Errorf("%s: roles = %v, want the query id among them", name, fm.Roles())
		}
	}

	fm, ok := Preset(PresetOdyssey)
	if !ok {
		t.Fatal("odyssey preset is missing")
	}

	if _, reported := fm.Roles()[RoleQueryID]; reported {
		t.Error("odyssey reports a query id role; a pooler writes no plans")
	}
}

// TestFieldMapWithoutAQueryIDIsUsable: the role is optional, so a stream whose
// records carry no statement identifier still serves search and check.
func TestFieldMapWithoutAQueryIDIsUsable(t *testing.T) {
	t.Parallel()

	fm, err := FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:     PresetNone,
		Timestamp:  "@timestamp",
		Severity:   "level",
		Text:       "msg",
		Host:       "host",
		Severities: []string{"error"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, reported := fm.Roles()[RoleQueryID]; reported {
		t.Errorf("roles = %v, want no query id", fm.Roles())
	}
}

func TestQueryIDOverridesThePreset(t *testing.T) {
	t.Parallel()

	fm, err := FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:    PresetJSONLog,
		Timestamp: "@timestamp",
		Host:      "host.name",
		QueryID:   "pg.query_id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fm.QueryID != "pg.query_id" {
		t.Errorf("QueryID = %q, want pg.query_id", fm.QueryID)
	}
}
