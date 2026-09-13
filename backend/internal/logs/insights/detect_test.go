package insights

import (
	"strings"
	"testing"
)

func TestDetectCorpus(t *testing.T) {
	t.Parallel()

	for _, name := range corpora {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			recs := loadCorpus(t, name)
			plans, negative := 0, 0

			for _, r := range recs {
				pr, ok := Detect(r.Text, r.QueryID)
				if !ok {
					continue
				}

				plans++

				if pr.DurationMs <= 0 {
					t.Errorf("plan with duration %v", pr.DurationMs)
				}

				if pr.Body == "" || strings.HasPrefix(pr.Body, "\n") {
					t.Errorf("plan body %q is empty or keeps the newline", pr.Body)
				}

				if !pr.HasQueryID {
					t.Errorf("plan without a query id, field %q", r.QueryID)
				}

				if pr.QueryID < 0 {
					negative++
				}
			}

			if plans != 14 {
				t.Errorf("detected %d plans, want 14", plans)
			}

			if negative == 0 {
				t.Error("no negative query id read back; the corpus has several")
			}

			statements := 0

			for _, r := range probeCase(t, recs, "stmt_duration") {
				if !strings.HasPrefix(r.Text, "duration: ") || !strings.Contains(r.Text, "  statement: ") {
					continue
				}

				statements++

				if _, ok := Detect(r.Text, r.QueryID); ok {
					t.Errorf("statement record taken for a plan: %q", r.Text)
				}
			}

			if statements != 2 {
				t.Errorf("stmt_duration holds %d statement records, want 2", statements)
			}
		})
	}
}

func TestDetectReadsDurationAndQueryID(t *testing.T) {
	t.Parallel()

	r := probeCase(t, loadCorpus(t, "pg17.jsonl"), "text_verbose_off")[0]

	pr, ok := Detect(r.Text, r.QueryID)
	if !ok {
		t.Fatal("plan not detected")
	}

	if pr.DurationMs != 22.439 {
		t.Errorf("duration = %v, want 22.439", pr.DurationMs)
	}

	if pr.QueryID != -4452854032459450605 {
		t.Errorf("query id = %d, want -4452854032459450605", pr.QueryID)
	}

	if !strings.HasPrefix(pr.Body, "Query Text: SELECT count(*) FROM probe_t") {
		t.Errorf("body starts with %q", pr.Body[:min(len(pr.Body), 40)])
	}
}

func TestDetectRejectsWhatIsNotAPlan(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"duration: 1.500 ms  statement: SELECT 1",
		"duration: 0.120 ms  execute S_1: SELECT 1",
		"duration: 0.050 ms  parse <unnamed>: SELECT 1",
		"duration: 0.050 ms  bind S_1: SELECT 1",
		"duration: 0.050 ms",
		"LOG:  duration: 1.000 ms  plan:\nQuery Text: SELECT 1",
		"duration: fast ms  plan:\nQuery Text: SELECT 1",
		"checkpoint starting: time",
	} {
		if _, ok := Detect(text, "42"); ok {
			t.Errorf("Detect(%q) took it for a plan", text)
		}
	}
}

func TestIsStatementDuration(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		"duration: 1.500 ms  statement: SELECT 1":         true,
		"duration: 0.120 ms  execute S_1: SELECT 1":       true,
		"duration: 0.050 ms  parse <unnamed>: SELECT 1":   true,
		"duration: 0.050 ms  bind S_1: SELECT 1":          true,
		"duration: 0.050 ms":                              true,
		"duration: 1.000 ms  plan:\nQuery Text: SELECT 1": false,
		"statement: SELECT 1":                             false,
	} {
		if got := isStatementDuration(text); got != want {
			t.Errorf("isStatementDuration(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestParseQueryID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		id   int64
		want bool
	}{
		{raw: "-9088110242053087898", id: -9088110242053087898, want: true},
		{raw: " 174205896191899363 ", id: 174205896191899363, want: true},
		{raw: "0", want: false},
		{raw: "", want: false},
		{raw: "12abc", want: false},
		{raw: "18446744073709551615", want: false},
	}

	for _, tt := range tests {
		id, ok := ParseQueryID(tt.raw)
		if ok != tt.want || id != tt.id {
			t.Errorf("ParseQueryID(%q) = %d, %v; want %d, %v", tt.raw, id, ok, tt.id, tt.want)
		}
	}
}
