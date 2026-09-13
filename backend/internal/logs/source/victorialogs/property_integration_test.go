//go:build integration

package victorialogs

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// replayProvider hands the seeded set back without pushing anything down,
// which the source contract allows: a provider may only narrow. It is the
// reference the store-backed provider is compared against.
type replayProvider struct {
	fields  source.FieldMap
	records []source.Record
}

func newReplayProvider(t *testing.T, fields source.FieldMap) *replayProvider {
	t.Helper()

	records := make([]source.Record, 0, len(seedRecords()))

	for i, r := range seedRecords() {
		ts, err := time.Parse(time.RFC3339Nano, r.Time)
		if err != nil {
			t.Fatalf("seed timestamp %q: %v", r.Time, err)
		}

		records = append(records, source.Record{
			Timestamp: ts,
			Token:     fmt.Sprintf("%d", i),
			Fields: map[string]string{
				"_time":          r.Time,
				"_msg":           r.Msg,
				"error_severity": r.ErrorSeverity,
				"dbname":         r.Dbname,
				"user":           r.User,
				"pid":            r.PID,
				"cluster":        r.Cluster,
				"host":           r.Host,
				"app":            r.App,
			},
		})
	}

	// The store-backed provider reads from new to old; the reference does too.
	slices.SortStableFunc(records, func(a, b source.Record) int {
		return b.Timestamp.Compare(a.Timestamp)
	})

	return &replayProvider{fields: fields, records: records}
}

func (p *replayProvider) Streams() []string { return []string{source.StreamPostgreSQL} }

func (p *replayProvider) Fields(stream string) source.FieldMap {
	if stream != source.StreamPostgreSQL {
		return source.FieldMap{}
	}

	return p.fields
}

// Stream applies severity and host itself: those two the logs service leaves
// to the source, so the reference has to answer them the same way the store
// does.
func (p *replayProvider) Stream(_ context.Context, sp source.StreamParams, fn func(source.Record) bool) error {
	matching := make([]source.Record, 0, len(p.records))

	for _, r := range p.records {
		if r.Timestamp.Before(sp.From) || r.Timestamp.After(sp.To) {
			continue
		}

		if matchesFilter(p.fields, r, sp.Filter) {
			matching = append(matching, r)
		}
	}

	start := 0

	if sp.Token != "" {
		for i, r := range matching {
			if r.Token == sp.Token {
				start = i + 1

				break
			}
		}
	}

	for _, r := range matching[start:] {
		if !fn(r) {
			return nil
		}
	}

	return nil
}

func (p *replayProvider) Check(context.Context, config.Cluster, string) (source.CheckResult, error) {
	return source.CheckResult{}, nil
}

func newTestService(t *testing.T, p source.Provider) logs.Service {
	t.Helper()

	reg := source.NewRegistry()
	reg.Register("main", p)

	clusters := config.NewClustersFromConfig(config.Config{
		Clusters: []config.Cluster{{
			Name:      testCluster,
			Hosts:     []config.Host{"db-1", "db-2"},
			LogSource: "main",
		}},
	})

	return logs.NewService(clusters, reg, config.LogSearchConfig{
		MaxScan:        1000,
		MaxPageSize:    1000,
		TimeoutSeconds: 30,
	}, config.LogInsightsConfig{}, zap.NewNop())
}

func testSearch(pageSize int) logs.SearchQuery {
	return logs.SearchQuery{
		Cluster:  testCluster,
		Stream:   source.StreamPostgreSQL,
		From:     seedStart.Add(-time.Hour),
		To:       seedStart.Add(time.Hour),
		PageSize: pageSize,
	}
}

// searchAll follows the page tokens to the end, so the cursor takes part in
// the comparison.
func searchAll(t *testing.T, s logs.Service, q logs.SearchQuery) []logs.Entry {
	t.Helper()

	var out []logs.Entry

	for page := 0; page < 100; page++ {
		res, err := s.Search(context.Background(), q)
		if err != nil {
			t.Fatalf("search: %v", err)
		}

		if res.Partial {
			t.Fatalf("search returned a partial result: %+v", q)
		}

		out = append(out, res.Items...)

		if res.NextPageToken == "" {
			return out
		}

		q.PageToken = res.NextPageToken
	}

	t.Fatal("search did not reach the end of the range")

	return nil
}

// TestSearchSemanticsDoNotDependOnTheSource: the same set answered by the
// store and by a provider that pushes nothing down gives the same results.
func TestSearchSemanticsDoNotDependOnTheSource(t *testing.T) {
	store := testProvider(nil)
	reference := newReplayProvider(t, store.Fields(source.StreamPostgreSQL))

	fromStore := newTestService(t, store)
	fromReference := newTestService(t, reference)

	queries := map[string]logs.SearchQuery{
		"everything":        testSearch(1000),
		"paged":             testSearch(5),
		"severity":          withQuery(testSearch(7), func(q *logs.SearchQuery) { q.Severities = []string{"ERROR", "FATAL"} }),
		"host":              withQuery(testSearch(7), func(q *logs.SearchQuery) { q.Host = "db-1" }),
		"severity and host": withQuery(testSearch(1000), func(q *logs.SearchQuery) { q.Severities = []string{"ERROR"}; q.Host = "db-2" }),
		"include":           withQuery(testSearch(7), func(q *logs.SearchQuery) { q.Include = []string{"took 7 ms"} }),
		"exclude":           withQuery(testSearch(7), func(q *logs.SearchQuery) { q.Exclude = []string{"statement 1"} }),
		"database":          withQuery(testSearch(7), func(q *logs.SearchQuery) { q.Database = "billing" }),
		"user":              withQuery(testSearch(7), func(q *logs.SearchQuery) { q.User = "admin" }),
	}

	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			want := entryKeys(searchAll(t, fromReference, q))
			got := entryKeys(searchAll(t, fromStore, q))

			if len(got) != len(want) {
				t.Fatalf("store returned %d entries, reference %d", len(got), len(want))
			}

			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("entry %d: store %q, reference %q", i, got[i], want[i])
				}
			}
		})
	}

	t.Run("dedup", func(t *testing.T) {
		q := testSearch(1000)
		q.Dedup = true

		want := dedupKeys(searchAll(t, fromReference, q))
		got := dedupKeys(searchAll(t, fromStore, q))

		if len(got) != len(want) {
			t.Fatalf("store returned %d groups, reference %d", len(got), len(want))
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("group %d: store %q, reference %q", i, got[i], want[i])
			}
		}
	})
}

func withQuery(q logs.SearchQuery, mutate func(*logs.SearchQuery)) logs.SearchQuery {
	mutate(&q)

	return q
}

func entryKeys(entries []logs.Entry) []string {
	items := make([]keyed, 0, len(entries))
	for _, e := range entries {
		items = append(items, keyed{ts: e.Timestamp, key: fmt.Sprintf("%s|%s|%s|%s|%s|%s",
			e.Timestamp.UTC().Format(time.RFC3339Nano), e.Severity, e.Hostname, e.Text, e.Database, e.User)})
	}

	return sortTies(items)
}

func dedupKeys(entries []logs.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, fmt.Sprintf("%s|%d", e.Text, e.Count))
	}

	sort.Strings(out)

	return out
}
