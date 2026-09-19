package logs

import (
	"slices"
	"testing"

	"github.com/dbulashev/dasha/internal/logs/source"
)

func TestConfigurationReadsTheAutoExplainSetup(t *testing.T) {
	t.Parallel()

	c := newConfiguration(map[string]string{
		settingLogMinDuration: "-1",
		settingLogAnalyze:     "off",
		settingLogFormat:      "json",
		settingLogLevel:       "log",
		settingComputeQueryID: "auto",
	})

	if !c.AutoExplain {
		t.Error("auto_explain reported as not loaded although it registered its settings")
	}

	if c.LogMinDurationMs == nil || *c.LogMinDurationMs != -1 {
		t.Errorf("log_min_duration = %v, want -1", c.LogMinDurationMs)
	}

	if c.LogAnalyze == nil || *c.LogAnalyze {
		t.Errorf("log_analyze = %v, want off", c.LogAnalyze)
	}

	if c.LogFormat != "json" || c.LogLevel != "log" || c.ComputeQueryID != "auto" {
		t.Errorf("configuration = %+v", c)
	}
}

// Without the library loaded PostgreSQL knows none of its settings, and that
// absence is the whole diagnosis.
func TestConfigurationWithoutAutoExplainLoaded(t *testing.T) {
	t.Parallel()

	c := newConfiguration(map[string]string{settingComputeQueryID: "off"})

	if c.AutoExplain || c.LogMinDurationMs != nil || c.LogAnalyze != nil || c.LogFormat != "" {
		t.Errorf("configuration = %+v, want auto_explain absent", c)
	}

	if c.ComputeQueryID != "off" {
		t.Errorf("compute_query_id = %q, want off", c.ComputeQueryID)
	}
}

func TestPlanSeveritiesFollowTheLevelsRead(t *testing.T) {
	t.Parallel()

	fm := testFieldMap(t)

	for _, tt := range []struct {
		name   string
		levels []string
		want   []string
	}{
		{name: "unread cluster", levels: nil, want: nil},
		{name: "configured level", levels: []string{"warning"}, want: []string{"WARNING"}},
		{name: "hosts logging at different levels", levels: []string{"log", "notice"}, want: []string{"LOG", "NOTICE"}},
		{name: "level the source does not accept", levels: []string{"log", "trace"}, want: nil},
	} {
		if got := planSeverities(tt.levels, fm); !slices.Equal(got, tt.want) {
			t.Errorf("%s: severities = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPlanSeveritiesOnASourceWithoutPostgresLevels(t *testing.T) {
	t.Parallel()

	fm := source.FieldMap{Severities: []string{"debug", "info"}}

	if got := planSeverities([]string{"log"}, fm); got != nil {
		t.Errorf("severities = %v, want none: the source knows no LOG", got)
	}
}
