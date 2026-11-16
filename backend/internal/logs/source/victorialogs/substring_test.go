package victorialogs

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/internal/logs/source"
)

func TestLogsQLPushesDownSubstringsDatabaseAndUser(t *testing.T) {
	t.Parallel()

	d := testStream(t, nil)
	f := source.Filter{ //nolint:exhaustruct
		Words:      []string{"duration"},
		Substrings: []string{`a\b "c" .*(`},
		Database:   "Billing",
		User:       "adm",
	}

	got := d.logsQL(f, queryFrom, queryTo)

	wants := []string{
		`"_msg":i("duration")`,
		`"_msg":~` + strconv.Quote(`(?i)a\\b "c" \.\*\(`),
		`"dbname":~"(?i)B[iİ]ll[iİ]ng"`,
		`"user":~"(?i)adm"`,
	}

	last := -1

	for _, want := range wants {
		at := strings.Index(got, want)
		if at < 0 {
			t.Fatalf("logsQL() misses %s\n got %s", want, got)
		}

		if at < last {
			t.Errorf("%s out of order: words go before the regexps\n got %s", want, got)
		}

		last = at
	}
}

// The pushed regexp must find whatever containsFold finds, or the store drops
// records the service would keep. It may find more: (?i) matches ς for Σ.
func TestSubstringPatternMatchesLikeContainsFold(t *testing.T) {
	t.Parallel()

	corpus := []string{
		"duration:", "DURATION: 1 ms", "Duration", "plan:", "a.b", "axb", "x*y", "(", ")", `\`, `"`,
		"[", "^$", "foo_bar", "İstanbul", "istanbul", "ISTANBUL", "ı", "I", "i",
		"K", "k", "K", "ß", "ẞ", "ss", "Σ", "σ", "ς", "ДЛИТЕЛЬНОСТЬ", "длительность",
		"日本語", "本", "\xff", "�", "a\nb", "a\tb", " ", "",
	}

	for _, needle := range corpus {
		re := regexp.MustCompile(substringPattern(needle))

		for _, h := range corpus {
			hay := "pre " + h + " post"
			want := strings.Contains(strings.ToLower(hay), strings.ToLower(needle))

			if got := re.MatchString(hay); want && !got {
				t.Errorf("needle %q in %q: regexp %v, containsFold %v (pattern %q)",
					needle, hay, got, want, substringPattern(needle))
			}
		}
	}
}

func TestLowerOutsideFoldHoldsTheDottedCapitalI(t *testing.T) {
	t.Parallel()

	if !slices.Contains(lowerOutsideFold()['i'], 'İ') {
		t.Errorf("runes lowering to i outside its fold = %q, want U+0130", lowerOutsideFold()['i'])
	}
}

func TestNarrowDropsDatabaseAndUserTheStreamDoesNotCarry(t *testing.T) {
	t.Parallel()

	with := testStream(t, nil)
	without := with
	without.fields.Database, without.fields.User = "", ""

	p := &Provider{streams: map[string]streamDef{"with": with, "without": without}} //nolint:exhaustruct
	f := source.Filter{                                                             //nolint:exhaustruct
		Words: []string{"duration"}, Substrings: []string{"select"}, Database: "app", User: "bob",
	}

	got := p.Narrow(context.Background(), source.StreamParams{Stream: "with", Filter: f}) //nolint:exhaustruct
	if len(got.Words) != 1 || len(got.Substrings) != 1 || got.Database != "app" || got.User != "bob" {
		t.Errorf("filter = %+v, want everything kept", got.Filter)
	}

	if got.TextSkip != source.TextSkipNone {
		t.Errorf("text skip = %q, want none", got.TextSkip)
	}

	got = p.Narrow(context.Background(), source.StreamParams{Stream: "without", Filter: f}) //nolint:exhaustruct
	if len(got.Substrings) != 1 || got.Database != "" || got.User != "" {
		t.Errorf("filter = %+v, want database and user dropped", got.Filter)
	}
}
