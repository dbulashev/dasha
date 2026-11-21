package logs

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"
)

func TestPresetWords(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		id      string
		include []string
		want    []string
		err     bool
	}{
		{name: "no preset", id: "", include: []string{"duration:"}, want: nil},
		{name: "slow", id: "slow", include: []string{"duration:"}, want: []string{"duration"}},
		{name: "plan", id: "plan", include: []string{"plan:"}, want: []string{"plan"}},
		{name: "case does not matter", id: "slow", include: []string{"DURATION:"}, want: []string{"duration"}},
		{name: "text extended", id: "slow", include: []string{"duration: 1"}, want: []string{"duration"}},
		{name: "one of several", id: "slow", include: []string{"select", "duration:"}, want: []string{"duration"}},
		{name: "text edited", id: "slow", include: []string{"durat"}, want: nil},
		{name: "text cleared", id: "slow", include: nil, want: nil},
		{
			name: "group", id: "checkpoint", include: []string{"point"},
			want: []string{"checkpoint", "checkpoints", "restartpoint", "point", "checkpointer", "checkpointed"},
		},
		{name: "preset without words", id: "errors", include: []string{"error"}, want: nil},
		{name: "unknown preset", id: "nope", include: []string{"duration:"}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := presetWords(tt.id, tt.include)
			if tt.err {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, want ErrInvalid", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("err = %v", err)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("words = %q, want %q", got, tt.want)
			}
		})
	}
}

// presetRecords are the messages PostgreSQL writes for what each preset is
// meant to find. A preset word missing from one of them would drop that record
// in the store before Dasha ever sees it.
var presetRecords = map[string][]string{
	"slow": {
		"duration: 1500.123 ms  statement: SELECT pg_sleep(1.5)",
		"duration: 12.004 ms  execute S_1: SELECT 1",
	},
	"plan": {
		"duration: 1500.123 ms  plan:\nQuery Text: SELECT pg_sleep(1.5)",
	},
	"autovacuum": {
		`automatic vacuum of table "shop.public.orders": index scans: 1`,
		`automatic aggressive vacuum of table "shop.public.orders": index scans: 0`,
		`automatic aggressive vacuum to prevent wraparound of table "shop.public.orders": index scans: 0`,
		`automatic vacuum to prevent wraparound of table "shop.public.orders": index scans: 0`,
		`automatic analyze of table "shop.public.orders"`,
	},
	"deadlock": {
		"deadlock detected",
	},
	"checkpoint": {
		"checkpoint starting: time",
		"checkpoint starting: immediate force wait",
		"checkpoint complete: wrote 12 buffers (0.1%); 0 WAL file(s) added",
		"checkpoint starting: fast force wait",
		"checkpoint complete: time: wrote 12 buffers (0.1%), wrote 0 SLRU buffers; 0 WAL file(s) added, " +
			"0 removed, 0 recycled; write=0.101 s, sync=0.002 s, total=0.110 s",
		"restartpoint complete: time: wrote 12 buffers (0.1%), wrote 0 SLRU buffers; 0 WAL file(s) added, " +
			"0 removed, 0 recycled; write=0.101 s, sync=0.002 s, total=0.110 s",
		"restartpoint starting: time",
		"restartpoint complete: wrote 12 buffers (0.1%); 0 WAL file(s) added",
		"recovery restart point at 0/3000028",
		"skipping restartpoint, already performed at 0/3000028",
		"checkpoints are occurring too frequently (12 seconds apart)",
		"skipping restartpoint, recovery has already ended",
		"checkpoints are occurring too frequently (1 second apart)",
		"checkpointer process (PID 42) was terminated by signal 9: Killed",
		"checkpointer process (PID 42) exited with exit code 1",
		"MultiXact member truncation is disabled because oldest checkpointed MultiXact 7 does not exist on disk",
	},
	"tempFiles": {
		`temporary file: path "base/pgsql_tmp/pgsql_tmp1234.0", size 1048576`,
	},
	"canceled": {
		"canceling statement due to statement timeout",
		"canceling statement due to lock timeout",
		"canceling statement due to user request",
		"canceling statement due to conflict with recovery",
	},
}

func TestPresetWordsFindTheirRecords(t *testing.T) {
	t.Parallel()

	for id, p := range searchPresets {
		if len(p.Words) == 0 {
			continue
		}

		recs := presetRecords[id]
		if len(recs) == 0 {
			t.Errorf("preset %q has words but no records to check them against", id)
		}

		for _, rec := range recs {
			if !containsFold(rec, p.Substring) {
				t.Errorf("preset %q: record %q lacks the substring %q", id, rec, p.Substring)
			}

			if !slices.ContainsFunc(p.Words, func(w string) bool { return holdsWord(rec, w) }) {
				t.Errorf("preset %q: record %q holds none of %q", id, rec, p.Words)
			}
		}
	}
}

// holdsWord splits text the way the stores tokenize it: letters, digits and
// the underscore make a word.
func holdsWord(text, w string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})

	return slices.Contains(tokens, strings.ToLower(w))
}
