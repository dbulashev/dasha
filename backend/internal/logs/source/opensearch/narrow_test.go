package opensearch

import (
	"context"
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

	return source.Filter{Severities: []string{"LOG"}, Host: "db-1", QueryID: &id, Contains: []string{"plan"}}
}

func TestNarrowFollowsTheMapping(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, map[string][]string{
		"query_id": {"long"},
		"message":  {"text"},
	})

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID == nil || len(got.Contains) != 1 {
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

	if got.QueryID != nil || got.Contains != nil {
		t.Errorf("filter = %+v, want neither pushed down", got)
	}

	if len(got.Severities) != 1 || got.Host != "db-1" {
		t.Errorf("filter = %+v, want severity and host kept", got)
	}
}

func TestNarrowWithoutAMappingNarrowsNothingExtra(t *testing.T) {
	t.Parallel()

	p := narrowProvider(t, nil)

	got := p.Narrow(context.Background(), narrowParams(planFilter()))

	if got.QueryID != nil || got.Contains != nil {
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

	if len(got.Contains) != 1 {
		t.Errorf("filter = %+v, want the phrase kept", got)
	}
}
