package opensearch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

func testFieldMap(t *testing.T) source.FieldMap {
	t.Helper()

	fm, err := source.FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:    source.PresetJSONLog,
		Timestamp: "@timestamp",
		Host:      "host.name",
	})
	if err != nil {
		t.Fatalf("field map: %v", err)
	}

	return fm
}

func TestExpandIndex(t *testing.T) {
	t.Parallel()

	got, err := expandIndex("pg-logs-{{ .Cluster }}-*", templateData{Cluster: "prod"})
	if err != nil {
		t.Fatalf("expandIndex: %v", err)
	}

	if got != "pg-logs-prod-*" {
		t.Errorf("expandIndex() = %q", got)
	}

	for _, name := range []string{"prod/../_all", "prod?x=1", "../etc"} {
		if _, err := expandIndex("{{ .Cluster }}", templateData{Cluster: name}); err == nil {
			t.Errorf("expandIndex accepted cluster name %q", name)
		}
	}
}

func TestBuildSearchPushesDownOnlyNarrowingFilters(t *testing.T) {
	t.Parallel()

	fm := testFieldMap(t)
	from := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	req := buildSearch(fm, map[string]string{"cluster": "prod"},
		source.Filter{Severities: []string{"ERROR"}, Host: "db-1"}, from, to, 500, false)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body := string(raw)

	for _, want := range []string{
		`"size":500`,
		`"@timestamp":{"order":"asc"}`,
		`"gte":"2026-09-05T10:00:00Z"`,
		`"terms":{"error_severity":["ERROR"]}`,
		`"term":{"host.name":"db-1"}`,
		`"term":{"cluster":"prod"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("query body misses %s\ngot: %s", want, body)
		}
	}

	// The message, database and user filters stay on the Dasha side: pushing
	// them down would drop records the substring match would keep.
	for _, unwanted := range []string{"message", "dbname", `"user"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("query body pushes down %s\ngot: %s", unwanted, body)
		}
	}
}

func TestBuildSearchEncodesValuesAsLiterals(t *testing.T) {
	t.Parallel()

	fm := testFieldMap(t)
	now := time.Now()

	req := buildSearch(fm, map[string]string{"cluster": `p" OR "1`},
		source.Filter{Host: `db" OR host:*`}, now, now.Add(time.Hour), 10, false)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body := string(raw)

	for _, want := range []string{`"db\" OR host:*"`, `"p\" OR \"1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("value not encoded as a JSON literal: %s\ngot: %s", want, body)
		}
	}
}

func TestHostFilterSuffixMatchesFQDN(t *testing.T) {
	t.Parallel()

	fm := testFieldMap(t)
	fm.HostMatch = source.HostMatchSuffix

	raw, err := json.Marshal(hostFilter(fm, "db-1"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body := string(raw)
	if !strings.Contains(body, `"prefix":{"host.name":"db-1."}`) ||
		!strings.Contains(body, `"term":{"host.name":"db-1"}`) {
		t.Errorf("suffix host filter = %s", body)
	}
}

func TestFlattenNestedSource(t *testing.T) {
	t.Parallel()

	src := map[string]any{
		"message": "boom",
		"host":    map[string]any{"name": "db-1"},
		"pid":     float64(4242),
		"tags":    []any{"a", "b"},
		"empty":   nil,
	}

	out := map[string]string{}
	flatten("", src, out)

	want := map[string]string{
		"message":   "boom",
		"host.name": "db-1",
		"pid":       "4242",
		"tags":      `["a","b"]`,
		"empty":     "",
	}

	for k, v := range want {
		if out[k] != v {
			t.Errorf("flatten()[%q] = %q, want %q", k, out[k], v)
		}
	}
}

func TestParseTime(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

	tests := []any{
		"2026-09-05T10:00:00Z",
		"2026-09-05T10:00:00.000Z",
		"2026-09-05 10:00:00+00:00",
		float64(want.UnixMilli()),
	}

	for _, in := range tests {
		got, err := parseTime(in)
		if err != nil {
			t.Fatalf("parseTime(%v): %v", in, err)
		}

		if !got.Equal(want) {
			t.Errorf("parseTime(%v) = %v, want %v", in, got, want)
		}
	}

	if _, err := parseTime("yesterday"); err == nil {
		t.Error("parseTime accepted an unparseable value")
	}

	if _, err := parseTime(nil); err == nil {
		t.Error("parseTime accepted a missing value")
	}
}

func TestBuildSearchMatchesAnalyzedFieldsThroughTheirKeywordField(t *testing.T) {
	t.Parallel()

	fm, err := source.FieldMapFromConfig(config.LogFieldMapConfig{
		Preset:    source.PresetJSONLog,
		Timestamp: "@timestamp",
		Host:      "host.name",
		KeywordFields: map[string]string{
			"error_severity": "error_severity.keyword",
			"host.name":      "host.name.keyword",
			"cluster":        "cluster.keyword",
		},
	})
	if err != nil {
		t.Fatalf("field map: %v", err)
	}

	now := time.Now()
	req := buildSearch(fm, map[string]string{"cluster": "prod"},
		source.Filter{Severities: []string{"ERROR"}, Host: "db-1"}, now, now.Add(time.Hour), 10, false)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body := string(raw)

	for _, want := range []string{
		`"terms":{"error_severity.keyword":["ERROR"]}`,
		`"term":{"host.name.keyword":"db-1"}`,
		`"term":{"cluster.keyword":"prod"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("query body misses %s\ngot: %s", want, body)
		}
	}
}

func TestStreamsFromConfigRejectsHostInATemplate(t *testing.T) {
	t.Parallel()

	fieldMap := config.LogFieldMapConfig{
		Preset:    source.PresetJSONLog,
		Timestamp: "@timestamp",
		Host:      "host.name",
	}

	tests := map[string]config.LogStreamConfig{
		"index":    {Index: "pg-logs-{{ .Host }}-*", FieldMap: fieldMap},
		"selector": {Index: "pg-*", Selector: map[string]string{"host": "{{ .Host }}"}, FieldMap: fieldMap},
	}

	for name, sc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := streamsFromConfig(config.LogSourceConfig{
				Streams: map[string]config.LogStreamConfig{"postgresql": sc},
			})
			if err == nil {
				t.Fatal("expected an error for a template naming the host")
			}

			if !strings.Contains(err.Error(), "Host") {
				t.Errorf("error does not name the substitution: %v", err)
			}
		})
	}
}

func TestStreamsFromConfigRejectsIncompleteFieldMap(t *testing.T) {
	t.Parallel()

	_, err := streamsFromConfig(config.LogSourceConfig{
		Streams: map[string]config.LogStreamConfig{
			"postgresql": {
				Index:    "pg-*",
				FieldMap: config.LogFieldMapConfig{Preset: source.PresetJSONLog},
			},
		},
	})
	if err == nil {
		t.Fatal("expected an error for a field map without timestamp and host")
	}

	if !strings.Contains(err.Error(), "postgresql") {
		t.Errorf("error does not name the stream: %v", err)
	}
}
