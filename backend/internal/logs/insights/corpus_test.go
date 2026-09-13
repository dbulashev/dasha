package insights

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
)

// corpusRecord is one record of testdata/, taken from the auto_explain probe
// run on PostgreSQL: pg17.jsonl is jsonlog, pg14.csv is csvlog.
type corpusRecord struct {
	Timestamp time.Time
	Severity  string
	SQLState  string
	Text      string
	QueryID   string
}

func (r corpusRecord) classified() Record {
	return Record{Stream: config.LogStreamPostgreSQL, Severity: r.Severity, SQLState: r.SQLState, Text: r.Text}
}

const corpusTimeLayout = "2006-01-02 15:04:05.000 MST"

// csvlog columns, the same on PostgreSQL 14-18.
const (
	csvLogTime   = 0
	csvSeverity  = 11
	csvSQLState  = 12
	csvMessage   = 13
	csvQueryID   = 25
	csvColumnNum = 26
)

func loadCorpus(t *testing.T, name string) []corpusRecord {
	t.Helper()

	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}

	t.Cleanup(func() { _ = f.Close() })

	if strings.HasSuffix(name, ".csv") {
		return loadCSVLog(t, f)
	}

	return loadJSONLog(t, f)
}

func loadJSONLog(t *testing.T, r io.Reader) []corpusRecord {
	t.Helper()

	var out []corpusRecord

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)

	for sc.Scan() {
		dec := json.NewDecoder(strings.NewReader(sc.Text()))
		dec.UseNumber()

		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("decode jsonlog line: %v", err)
		}

		out = append(out, corpusRecord{
			Timestamp: parseCorpusTime(t, str(raw["timestamp"])),
			Severity:  str(raw["error_severity"]),
			SQLState:  str(raw["state_code"]),
			Text:      str(raw["message"]),
			QueryID:   str(raw["query_id"]),
		})
	}

	if err := sc.Err(); err != nil {
		t.Fatalf("read jsonlog: %v", err)
	}

	return out
}

func loadCSVLog(t *testing.T, r io.Reader) []corpusRecord {
	t.Helper()

	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		t.Fatalf("read csvlog: %v", err)
	}

	out := make([]corpusRecord, 0, len(rows))

	for _, row := range rows {
		if len(row) != csvColumnNum {
			t.Fatalf("csvlog row has %d columns, want %d", len(row), csvColumnNum)
		}

		out = append(out, corpusRecord{
			Timestamp: parseCorpusTime(t, row[csvLogTime]),
			Severity:  row[csvSeverity],
			SQLState:  row[csvSQLState],
			Text:      row[csvMessage],
			QueryID:   row[csvQueryID],
		})
	}

	return out
}

func parseCorpusTime(t *testing.T, s string) time.Time {
	t.Helper()

	ts, err := time.Parse(corpusTimeLayout, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}

	return ts
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

// probeCase returns the records the probe wrote after its "PROBE <name>"
// marker, up to the next marker.
func probeCase(t *testing.T, recs []corpusRecord, name string) []corpusRecord {
	t.Helper()

	var (
		out    []corpusRecord
		inside bool
	)

	for _, r := range recs {
		if marker, ok := strings.CutPrefix(r.Text, "PROBE "); ok {
			inside = marker == name

			continue
		}

		if inside {
			out = append(out, r)
		}
	}

	if len(out) == 0 {
		t.Fatalf("probe case %q has no records", name)
	}

	return out
}

var corpora = []string{"pg17.jsonl", "pg14.csv"}
