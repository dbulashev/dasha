package opensearch

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

const narrowIndex = "pg-logs-prod"

// narrowProvider serves one stream whose mapping is already known, so Narrow
// decides without asking the store.
func narrowProvider(t *testing.T, types map[string][]string) *Provider {
	t.Helper()

	p := &Provider{ //nolint:exhaustruct
		streams: map[string]streamDef{
			source.StreamPostgreSQL: {index: "pg-logs-{{ .Cluster }}", fields: testFieldMap(t)},
		},
		logger: zap.NewNop(),
	}
	p.fieldTypeCache.Store(narrowIndex, fieldTypeEntry{types: types, at: time.Now()})

	return p
}

func narrowParams(f source.Filter) source.StreamParams {
	return source.StreamParams{ //nolint:exhaustruct
		Cluster: config.Cluster{Name: "prod"},
		Stream:  source.StreamPostgreSQL,
		Filter:  f,
	}
}

func planFilter() source.Filter {
	id := int64(-4452854032459450605)

	return source.Filter{Severities: []string{"LOG"}, Host: "db-1", QueryID: &id, Words: [][]string{{"plan"}}}
}

func TestNarrowFollowsTheMapping(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{
		"query_id": {"long"},
		"message":  {"text"},
	})

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID == nil || len(got.Words) != 1 {
		t.Errorf("filter = %+v, want the statement id and the phrase pushed down", got)
	}
}

// A phrase sent to a keyword field matches no record, and a term sent to an
// analyzed one loses the sign of a negative id: neither may be pushed down.
func TestNarrowDropsWhatTheMappingWouldSilentlyEmpty(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{
		"query_id": {"text"},
		"message":  {"keyword"},
	})

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID != nil || got.Words != nil {
		t.Errorf("filter = %+v, want neither pushed down", got)
	}

	if len(got.Severities) != 1 || got.Host != "db-1" {
		t.Errorf("filter = %+v, want severity and host kept", got)
	}
}

// One index of the pattern mapping the field as a keyword is enough: the phrase
// matches nothing there, and the records it holds go missing.
func TestNarrowDropsAPhraseOnAMixedMapping(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{
		"query_id": {"long"},
		"message":  {"text", "keyword"},
	})

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.Words != nil {
		t.Errorf("filter = %+v, want the phrase dropped: one index maps the field as a keyword", got)
	}

	if got.QueryID == nil {
		t.Errorf("filter = %+v, want the id kept: its own mapping is exact", got)
	}
}

// A field mapped with index: false fails the whole search when queried, so
// neither a phrase nor a term may go to it.
func TestNarrowDropsWhatTheIndexCannotSearch(t *testing.T) {
	t.Parallel()

	caps := fieldCapsResponse{}
	if err := json.Unmarshal([]byte(`{"fields": {
		"query_id": {"long": {"type": "long", "searchable": false}},
		"message":  {"text": {"type": "text", "searchable": false}},
		"other":    {"unmapped": {"type": "unmapped", "searchable": false}}
	}}`), &caps); err != nil {
		t.Fatal(err)
	}

	types := fieldTypesOf(caps)
	if types["other"][0] != unmappedType {
		t.Errorf("types = %v, want an unmapped index to stay unmapped", types)
	}

	p := narrowProvider(t, types)

	f := planFilter()
	f.Terms = []string{"seq"}

	got := p.Narrow(context.Background(), narrowParams(f))

	if got.QueryID != nil || got.Words != nil || got.Terms != nil || got.TextSkip != source.TextSkipMapping {
		t.Errorf("narrowed = %+v, want nothing on the unsearchable fields and a mapping skip", got)
	}
}

func TestNarrowWithoutAMappingNarrowsNothingExtra(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, nil)

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID != nil || got.Words != nil {
		t.Errorf("filter = %+v, want a wide scan over a silently empty answer", got)
	}
}

func TestNarrowUsesTheKeywordOverrideOfTheQueryIDField(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{
		"query_id":         {"text"},
		"query_id.keyword": {"keyword"},
	})
	fm := p.streams[source.StreamPostgreSQL].fields
	fm.KeywordFields = map[string]string{"query_id": "query_id.keyword"}
	p.streams[source.StreamPostgreSQL] = streamDef{index: "pg-logs-{{ .Cluster }}", fields: fm} //nolint:exhaustruct

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID == nil {
		t.Errorf("filter = %+v, want the id pushed down onto the keyword field", got)
	}
}

// A term on a field the index does not hold matches nothing at all.
func TestNarrowDropsAnUnmappedQueryIDField(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{"message": {"text"}})

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID != nil {
		t.Errorf("filter = %+v, want the id dropped: the index has no such field", got)
	}

	if len(got.Words) != 1 {
		t.Errorf("filter = %+v, want the phrase kept", got)
	}
}

// One index of the pattern not holding query_id is enough: a term sent there
// matches nothing, and every record it holds goes missing.
func TestNarrowDropsTheIDOnAPartlyUnmappedField(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{
		"query_id": {"long", unmappedType},
		"message":  {"text"},
	})

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID != nil {
		t.Errorf("filter = %+v, want the id dropped: one index of the pattern has no such field", got)
	}

	if len(got.Words) != 1 {
		t.Errorf("filter = %+v, want the phrase kept", got)
	}
}

func TestNarrowHoldsBackSubstringsDatabaseAndUser(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{"message": {"text"}})
	f := planFilter()
	f.Substrings, f.Database, f.User = []string{"select"}, "app", "bob"

	got := p.Narrow(context.Background(), narrowParams(f))
	if len(got.Words) != 1 || got.Substrings != nil || got.Database != "" || got.User != "" {
		t.Errorf("filter = %+v, want the word alone", got.Filter)
	}

	if got.TextSkip != source.TextSkipUnsupported {
		t.Errorf("text skip = %q, want unsupported", got.TextSkip)
	}
}

func TestNarrowSendsTermsToAnAnalyzedField(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{"message": {"text"}})
	f := source.Filter{Terms: []string{"deadlock", "detected"}} //nolint:exhaustruct

	got := p.Narrow(context.Background(), narrowParams(f))

	if len(got.Terms) != 2 || got.TextSkip != source.TextSkipNone {
		t.Errorf("narrowed = %+v, want both terms", got)
	}
}

// Terms are the message filter itself: dropping some would widen the answer
// past what the user asked for, so they go all together or not at all.
func TestNarrowDropsEveryTermOnAMixedMapping(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{"message": {"text", "keyword"}})
	f := source.Filter{Terms: []string{"deadlock"}} //nolint:exhaustruct

	got := p.Narrow(context.Background(), narrowParams(f))

	if got.Terms != nil || got.TextSkip != source.TextSkipMapping {
		t.Errorf("narrowed = %+v, want no terms and a mapping skip", got)
	}
}

func TestNarrowDropsTermsWithoutAMapping(t *testing.T) {
	t.Parallel()

	f := source.Filter{Terms: []string{"deadlock"}} //nolint:exhaustruct

	got := narrowProvider(t, nil).Narrow(context.Background(), narrowParams(f))

	if got.Terms != nil || got.TextSkip != source.TextSkipMapping {
		t.Errorf("narrowed = %+v, want no terms and a mapping skip", got)
	}
}
