package victorialogs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

func testStream(t *testing.T, mutate func(*config.LogStreamConfig)) streamDef {
	t.Helper()

	sc := config.LogStreamConfig{ //nolint:exhaustruct
		StreamSelector: map[string]string{"cluster": "{{ .Cluster }}"},
		Selector:       map[string]string{"app": "postgres"},
		Query:          "*",
		FieldMap: config.LogFieldMapConfig{ //nolint:exhaustruct
			Preset:    source.PresetJSONLog,
			Timestamp: "_time",
			Text:      "_msg",
			Host:      "host",
		},
	}

	if mutate != nil {
		mutate(&sc)
	}

	streams, err := streamsFromConfig(config.LogSourceConfig{ //nolint:exhaustruct
		Type:    config.LogSourceTypeVictoriaLogs,
		Streams: map[string]config.LogStreamConfig{source.StreamPostgreSQL: sc},
	})
	if err != nil {
		t.Fatalf("streams from config: %v", err)
	}

	d, err := streams[source.StreamPostgreSQL].expand(source.TemplateData{Cluster: "prod"})
	if err != nil {
		t.Fatalf("expand: %v", err)
	}

	return d
}

var (
	queryFrom = time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	queryTo   = time.Date(2026, 9, 12, 11, 0, 0, 123456789, time.UTC)
)

func TestLogsQLPushesDownOnlyNarrowingFilters(t *testing.T) {
	t.Parallel()

	d := testStream(t, nil)

	got := d.logsQL(source.Filter{Severities: []string{"ERROR", "FATAL"}, Host: "pg-1"}, queryFrom, queryTo)

	want := `{cluster="prod"} ` +
		`_time:[2026-09-12T10:00:00Z, 2026-09-12T11:00:00.123456789Z] ` +
		`"error_severity":in("ERROR","FATAL") ` +
		`"host":="pg-1" ` +
		`"app":="postgres" ` +
		`(*)`

	if got != want {
		t.Errorf("logsQL()\n got %s\nwant %s", got, want)
	}
}

func TestLogsQLWithoutFilters(t *testing.T) {
	t.Parallel()

	d := testStream(t, nil)

	got := d.logsQL(source.Filter{Severities: nil, Host: ""}, queryFrom, queryTo)

	for _, unwanted := range []string{"error_severity", "host"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("logsQL() filters on %s without being asked: %s", unwanted, got)
		}
	}
}

func TestLogsQLHostSuffixMatchesFQDN(t *testing.T) {
	t.Parallel()

	d := testStream(t, func(sc *config.LogStreamConfig) {
		sc.FieldMap.HostMatch = source.HostMatchSuffix
	})

	got := d.logsQL(source.Filter{Severities: nil, Host: "pg-1"}, queryFrom, queryTo)

	if !strings.Contains(got, `("host":="pg-1" OR "host":="pg-1."*)`) {
		t.Errorf("suffix host filter missing from %s", got)
	}
}

// TestLogsQLQuotesEveryValue: a value that looks like LogsQL stays a value.
func TestLogsQLQuotesEveryValue(t *testing.T) {
	t.Parallel()

	d := testStream(t, func(sc *config.LogStreamConfig) {
		sc.Selector = map[string]string{`odd name`: `va"lue\x`}
		sc.StreamSelector = map[string]string{"odd stream": `a" OR b:*`}
		sc.FieldMap.Host = "host.name"
	})

	got := d.logsQL(source.Filter{Severities: nil, Host: `db" OR *`}, queryFrom, queryTo)

	for _, want := range []string{
		`{"odd stream"="a\" OR b:*"}`,
		`"host.name":="db\" OR *"`,
		`"odd name":="va\"lue\\x"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("logsQL() lost the quoting of %s: %s", want, got)
		}
	}
}

func TestTargetNamesTheStreamWithoutTheReadBounds(t *testing.T) {
	t.Parallel()

	got := testStream(t, nil).target()

	want := `{cluster="prod"} "app":="postgres" (*)`
	if got != want {
		t.Errorf("target() = %s, want %s", got, want)
	}
}

func TestStreamsFromConfigRejectsHostInATemplate(t *testing.T) {
	t.Parallel()

	for _, sc := range []config.LogStreamConfig{
		{StreamSelector: map[string]string{"host": "{{ .Host }}"}}, //nolint:exhaustruct
		{Query: `"host":={{ .Host }}`},                             //nolint:exhaustruct
		{Selector: map[string]string{"host": "{{ .Host }}"}},       //nolint:exhaustruct
	} {
		sc.FieldMap = config.LogFieldMapConfig{ //nolint:exhaustruct
			Preset:    source.PresetJSONLog,
			Timestamp: "_time",
			Host:      "host",
		}

		_, err := streamsFromConfig(config.LogSourceConfig{ //nolint:exhaustruct
			Type:    config.LogSourceTypeVictoriaLogs,
			Streams: map[string]config.LogStreamConfig{source.StreamPostgreSQL: sc},
		})
		if err == nil {
			t.Errorf("template naming an unknown substitution accepted: %+v", sc)
		}
	}
}

func TestStreamsFromConfigRejectsIncompleteFieldMap(t *testing.T) {
	t.Parallel()

	_, err := streamsFromConfig(config.LogSourceConfig{ //nolint:exhaustruct
		Type: config.LogSourceTypeVictoriaLogs,
		Streams: map[string]config.LogStreamConfig{
			source.StreamPostgreSQL: { //nolint:exhaustruct
				Query:    "*",
				FieldMap: config.LogFieldMapConfig{Preset: source.PresetJSONLog}, //nolint:exhaustruct
			},
		},
	})
	if err == nil {
		t.Fatal("a field map without timestamp and host was accepted")
	}
}

func TestReadRecords(t *testing.T) {
	t.Parallel()

	body := `{"_time":"2026-09-12T10:00:00Z","_msg":"a"}
{"_time":"2026-09-12T10:00:01Z","_msg":"b"}
`

	got, err := readRecords(strings.NewReader(body), 10)
	if err != nil {
		t.Fatalf("readRecords: %v", err)
	}

	if len(got) != 2 || got[1]["_msg"] != "b" {
		t.Fatalf("readRecords() = %v", got)
	}

	empty, err := readRecords(strings.NewReader(""), 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty answer: %v, %v", empty, err)
	}
}

// TestReadRecordsStopsAtTheLimit: the limit bounds the answer a source may
// hold in memory, not only the request.
func TestReadRecordsStopsAtTheLimit(t *testing.T) {
	t.Parallel()

	body := `{"_time":"2026-09-12T10:00:00Z","_msg":"a"}
{"_time":"2026-09-12T10:00:01Z","_msg":"b"}
{"_time":"2026-09-12T10:00:02Z","_msg":"c"}
`

	got, err := readRecords(strings.NewReader(body), 2)
	if err != nil {
		t.Fatalf("readRecords: %v", err)
	}

	if len(got) != 2 || got[1]["_msg"] != "b" {
		t.Fatalf("readRecords() = %v, want the first two records", got)
	}
}

// TestReadRecordsOnATruncatedStream: the answer is streamed, so a failure
// upstream arrives as a cut-off body and must not read as the end of the data.
func TestReadRecordsOnATruncatedStream(t *testing.T) {
	t.Parallel()

	body := `{"_time":"2026-09-12T10:00:00Z","_msg":"a"}
{"_time":"2026-09-12T10:00:01Z","_ms`

	if _, err := readRecords(strings.NewReader(body), 10); err == nil {
		t.Fatal("a truncated stream read as a complete answer")
	}
}

func TestLogsQLPushesDownQueryIDAndPhrase(t *testing.T) {
	t.Parallel()

	d := testStream(t, nil)
	id := int64(-4452854032459450605)

	got := d.logsQL(source.Filter{QueryID: &id, Contains: []string{"plan"}}, queryFrom, queryTo)

	for _, want := range []string{`"query_id":="-4452854032459450605"`, `"_msg":"plan"`} {
		if !strings.Contains(got, want) {
			t.Errorf("logsQL() misses %s\n got %s", want, got)
		}
	}
}

func TestNarrowDropsAQueryIDTheStreamDoesNotCarry(t *testing.T) {
	t.Parallel()

	withID := testStream(t, nil)

	withoutID := withID
	withoutID.fields.QueryID = ""

	p := &Provider{streams: map[string]streamDef{"with": withID, "without": withoutID}} //nolint:exhaustruct

	id := int64(7)
	f := source.Filter{QueryID: &id, Contains: []string{"plan"}}

	if got := p.Narrow(context.Background(), source.StreamParams{Stream: "with", Filter: f}); got.QueryID == nil {
		t.Errorf("filter = %+v, want the id pushed down", got)
	}

	got := p.Narrow(context.Background(), source.StreamParams{Stream: "without", Filter: f})
	if got.QueryID != nil || len(got.Contains) != 1 {
		t.Errorf("filter = %+v, want the id dropped and the phrase kept", got)
	}
}
