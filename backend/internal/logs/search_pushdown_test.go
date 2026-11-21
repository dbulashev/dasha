package logs

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

func pushdownQuery() SearchQuery {
	q := testQuery()
	q.Preset = "slow"
	q.Include = []string{"duration:", ""}
	q.Database = "shop"
	q.User = "app"

	return q
}

func TestSearchNarrowsWithoutAQueryID(t *testing.T) {
	t.Parallel()

	recs := records(3)
	recs[1].Fields["message"] = "duration: 12 ms"
	recs[2].Fields["message"] = "duration: 15 ms"

	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newTestService(t, p, config.LogSearchConfig{})

	res, err := svc.Search(context.Background(), pushdownQuery())
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(p.narrowed) != 1 {
		t.Fatalf("narrow calls = %d, want 1", len(p.narrowed))
	}

	f := p.filter
	if !reflect.DeepEqual(f.Words, [][]string{{"duration"}}) || !slices.Equal(f.Substrings, []string{"duration:"}) ||
		f.Database != "shop" || f.User != "app" {
		t.Errorf("filter = %+v, want the preset word, the substring, database and user", f)
	}

	want := []string{"text=duration", "text~duration:", NarrowedDatabase, NarrowedUser}
	if !slices.Equal(res.NarrowedBy, want) {
		t.Errorf("narrowed_by = %v, want %v", res.NarrowedBy, want)
	}

	if res.TextSkipped != source.TextSkipNone {
		t.Errorf("text_skipped = %q, want none", res.TextSkipped)
	}

	if len(res.Items) != 1 || res.Items[0].Text != "duration: 15 ms" {
		t.Errorf("items = %+v, want the one slow record of shop and app", res.Items)
	}
}

func TestSearchSendsOneFilterToEveryRead(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(25)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Include = []string{"statement"}

	first, err := svc.Search(context.Background(), q)
	if err != nil || first.NextPageToken == "" {
		t.Fatalf("first page: token %q, err %v", first.NextPageToken, err)
	}

	q.PageToken = first.NextPageToken
	if _, err := svc.Search(context.Background(), q); err != nil {
		t.Fatalf("next page: %v", err)
	}

	q.PageToken = ""
	q.Dedup = true
	if _, err := svc.Search(context.Background(), q); err != nil {
		t.Fatalf("dedup: %v", err)
	}

	if len(p.streamed) != 3 {
		t.Fatalf("reads = %d, want 3", len(p.streamed))
	}

	for i, f := range p.streamed[1:] {
		if !reflect.DeepEqual(f, p.streamed[0]) {
			t.Errorf("read %d filter = %+v, want %+v", i+1, f, p.streamed[0])
		}
	}

	if !slices.Equal(p.streamed[0].Substrings, []string{"statement"}) {
		t.Errorf("filter = %+v, want the substring", p.streamed[0])
	}
}

func TestSearchSaysWhyTheTextStayedBehind(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields:   testFieldMap(t),
		records:  records(4),
		textSkip: source.TextSkipUnsupported,
		narrow: func(f source.Filter) source.Filter {
			f.Substrings = nil

			return f
		},
	}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Include = []string{"statement 1 "}

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if res.TextSkipped != source.TextSkipUnsupported {
		t.Errorf("text_skipped = %q, want unsupported", res.TextSkipped)
	}

	if len(res.NarrowedBy) != 0 {
		t.Errorf("narrowed_by = %v, want empty", res.NarrowedBy)
	}

	if len(res.Items) != 1 {
		t.Errorf("items = %d, want the one record filtered on the Dasha side", len(res.Items))
	}

	q.Include = nil

	res, err = svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search without text: %v", err)
	}

	if res.TextSkipped != source.TextSkipNone {
		t.Errorf("text_skipped without text = %q, want none", res.TextSkipped)
	}
}

func TestSearchPushdownOff(t *testing.T) {
	t.Parallel()

	off := false
	p := &fakeProvider{fields: testFieldMap(t), records: records(4)}
	svc := newTestService(t, p, config.LogSearchConfig{Pushdown: &off})

	q := pushdownQuery()
	q.Host = "db-1"

	if _, err := svc.Search(context.Background(), q); err != nil {
		t.Fatalf("search: %v", err)
	}

	f := p.filter
	if f.Words != nil || f.Substrings != nil || f.Database != "" || f.User != "" {
		t.Errorf("filter = %+v, want no text, database or user", f)
	}

	if f.Host != "db-1" {
		t.Errorf("host = %q, want db-1: pushdown covers text, database and user only", f.Host)
	}
}

func TestSearchRejectsAnUnknownPreset(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(1)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Preset = "nope"

	if _, err := svc.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}

	if p.calls != 0 {
		t.Errorf("reads = %d, want none", p.calls)
	}
}

func wordsQuery() SearchQuery {
	q := testQuery()
	q.TextMode = TextModeWords
	q.Include = []string{"Statement  took", "took ms"}

	return q
}

// The store runs the words; a record it returns is not checked again, so the
// fake, which filters nothing, hands back every record.
func TestSearchInWordsLetsTheStoreMatch(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(3)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	res, err := svc.Search(context.Background(), wordsQuery())
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if want := []string{"Statement", "took", "ms"}; !slices.Equal(p.filter.Terms, want) {
		t.Errorf("terms = %q, want %q", p.filter.Terms, want)
	}

	if p.filter.Substrings != nil {
		t.Errorf("substrings = %q, want none in words mode", p.filter.Substrings)
	}

	if res.TextMode != TextModeWords || res.TextSkipped != source.TextSkipNone {
		t.Errorf("mode = %q, skip = %q, want words and none", res.TextMode, res.TextSkipped)
	}

	if len(res.Items) != 3 {
		t.Errorf("items = %d, want every record the store returned", len(res.Items))
	}

	want := []string{"text=Statement", "text=took", "text=ms"}
	if !slices.Equal(res.NarrowedBy, want) {
		t.Errorf("narrowed_by = %v, want %v", res.NarrowedBy, want)
	}
}

func TestSearchInWordsFallsBackToSubstring(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields:   testFieldMap(t),
		records:  records(3),
		textSkip: source.TextSkipMapping,
		narrow: func(f source.Filter) source.Filter {
			f.Terms = nil

			return f
		},
	}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := wordsQuery()
	q.Include = []string{"statement 1 took"}

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if res.TextMode != TextModeSubstring || res.TextSkipped != source.TextSkipMapping {
		t.Errorf("mode = %q, skip = %q, want substring and mapping", res.TextMode, res.TextSkipped)
	}

	if len(res.Items) != 1 {
		t.Errorf("items = %d, want the one record holding the substring", len(res.Items))
	}
}

func TestSearchInWordsWithPushdownOff(t *testing.T) {
	t.Parallel()

	off := false
	p := &fakeProvider{fields: testFieldMap(t), records: records(3)}
	svc := newTestService(t, p, config.LogSearchConfig{Pushdown: &off})

	q := wordsQuery()
	q.Include = []string{"statement 2"}

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if p.filter.Terms != nil {
		t.Errorf("terms = %q, want none with pushdown off", p.filter.Terms)
	}

	if res.TextMode != TextModeSubstring || res.TextSkipped != source.TextSkipUnsupported {
		t.Errorf("mode = %q, skip = %q, want substring and unsupported", res.TextMode, res.TextSkipped)
	}

	if len(res.Items) != 1 {
		t.Errorf("items = %d, want the substring checked by Dasha", len(res.Items))
	}
}

func TestSearchRejectsAnUnknownTextMode(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(1)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.TextMode = "regex"

	if _, err := svc.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestSearchInWordsWithoutTextSkipsNothing(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(2), textSkip: source.TextSkipUnsupported}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.TextMode = TextModeWords

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if res.TextMode != TextModeWords || res.TextSkipped != source.TextSkipNone {
		t.Errorf("mode = %q, skip = %q, want words and none: no text was asked for", res.TextMode, res.TextSkipped)
	}
}

func TestSearchInWordsKeepsTheSkipOfThePresetText(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(2), textSkip: source.TextSkipUnsupported}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.TextMode = TextModeWords
	q.Preset = "slow"
	q.Include = []string{"duration:"}

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if res.TextMode != TextModeWords || res.TextSkipped != source.TextSkipUnsupported {
		t.Errorf("mode = %q, skip = %q, want words and unsupported", res.TextMode, res.TextSkipped)
	}
}

// The checkpoint preset holds "point", which no checkpoint record holds as a
// whole word: in words mode it stays a substring Dasha checks, beside the
// preset words and the terms of the rest.
func TestSearchInWordsKeepsThePresetTextASubstring(t *testing.T) {
	t.Parallel()

	recs := []source.Record{
		record(0, "checkpoint starting: time"),
		record(1, "restartpoint starting: time"),
		record(2, "replication starting"),
	}

	for _, tt := range []struct {
		name    string
		include []string
		terms   []string
	}{
		{name: "preset alone", include: []string{"point"}, terms: nil},
		{name: "preset and a word", include: []string{"point", "starting"}, terms: []string{"starting"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &fakeProvider{fields: testFieldMap(t), records: recs}
			svc := newTestService(t, p, config.LogSearchConfig{})

			q := testQuery()
			q.TextMode = TextModeWords
			q.Preset = "checkpoint"
			q.Include = tt.include

			res, err := svc.Search(context.Background(), q)
			if err != nil {
				t.Fatalf("search: %v", err)
			}

			f := p.filter
			if !slices.Equal(f.Terms, tt.terms) || !slices.Equal(f.Substrings, []string{"point"}) ||
				!reflect.DeepEqual(f.Words, [][]string{searchPresets["checkpoint"].Words}) {
				t.Errorf("filter = %+v, want terms %q, the preset text as a substring and the preset words", f, tt.terms)
			}

			if res.TextMode != TextModeWords || res.TextSkipped != source.TextSkipNone {
				t.Errorf("mode = %q, skip = %q, want words and none", res.TextMode, res.TextSkipped)
			}

			if len(res.Items) != 2 {
				t.Errorf("items = %+v, want the checkpoint and the restartpoint", res.Items)
			}
		})
	}
}
