package logs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

const testStream = source.StreamPostgreSQL

var testWindow = struct{ from, to time.Time }{
	from: time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC),
	to:   time.Date(2026, 9, 5, 11, 0, 0, 0, time.UTC),
}

// fakeProvider replays a fixed record list. It pushes no filter down, which the
// source contract allows: a provider may only narrow, never filter completely.
type fakeProvider struct {
	fields  source.FieldMap
	records []source.Record
	// err ends the stream after the records have been handed out.
	err error
	// hang waits for the context instead of returning, so the service's own
	// timeout is what ends the read.
	hang bool
	// check is what Check answers with.
	check source.CheckResult
	calls int
}

func (p *fakeProvider) Streams() []string { return []string{testStream} }

func (p *fakeProvider) Fields(stream string) source.FieldMap {
	if stream != testStream {
		return source.FieldMap{}
	}

	return p.fields
}

func (p *fakeProvider) Stream(ctx context.Context, sp source.StreamParams, fn func(source.Record) bool) error {
	p.calls++

	start := 0

	if sp.Token != "" {
		for i, r := range p.records {
			if r.Token == sp.Token {
				start = i + 1

				break
			}
		}
	}

	for _, r := range p.records[start:] {
		if !fn(r) {
			return nil
		}
	}

	if p.hang {
		<-ctx.Done()

		return ctx.Err()
	}

	return p.err
}

func (p *fakeProvider) Check(context.Context, config.Cluster, string) (source.CheckResult, error) {
	return p.check, nil
}

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

// record builds one log line; i drives the fields that filters select on.
func record(i int, message string) source.Record {
	ts := testWindow.from.Add(time.Duration(i) * time.Second)

	return source.Record{
		Timestamp: ts,
		Fields: map[string]string{
			"@timestamp":     ts.Format(time.RFC3339Nano),
			"error_severity": []string{"LOG", "ERROR"}[i%2],
			"message":        message,
			"host.name":      []string{"db-1", "db-2"}[i%2],
			"dbname":         []string{"shop", "billing"}[i%2],
			"user":           []string{"app", "admin"}[i%2],
		},
		Token: fmt.Sprintf("t%d", i),
	}
}

func records(n int) []source.Record {
	out := make([]source.Record, 0, n)
	for i := range n {
		out = append(out, record(i, fmt.Sprintf("statement %d took %d ms", i, i*7)))
	}

	return out
}

func newTestService(t *testing.T, p *fakeProvider, cfg config.LogSearchConfig) Service {
	t.Helper()

	reg := source.NewRegistry()
	reg.Register("main", p)

	clusters := config.NewClustersFromConfig(config.Config{
		Clusters: []config.Cluster{{
			Name:      "prod",
			Hosts:     []config.Host{"db-1", "db-2"},
			LogSource: "main",
		}},
	})

	return NewService(clusters, reg, cfg, zap.NewNop())
}

func testQuery() SearchQuery {
	return SearchQuery{
		Cluster:  "prod",
		Stream:   testStream,
		From:     testWindow.from,
		To:       testWindow.to,
		PageSize: 10,
	}
}

func TestSearchPagesWithoutGapOrRepeat(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(25)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()

	var seen []string

	for page := 0; ; page++ {
		if page > 5 {
			t.Fatal("paging does not terminate")
		}

		res, err := svc.Search(context.Background(), q)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}

		for _, e := range res.Items {
			seen = append(seen, e.Text)
		}

		if res.NextPageToken == "" {
			break
		}

		q.PageToken = res.NextPageToken
	}

	if len(seen) != 25 {
		t.Fatalf("read %d records over all pages, want 25", len(seen))
	}

	unique := map[string]bool{}
	for _, text := range seen {
		if unique[text] {
			t.Fatalf("record %q delivered twice", text)
		}

		unique[text] = true
	}
}

func TestSearchEmitsNoTokenWhenTheLastPageIsExact(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(10)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	res, err := svc.Search(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(res.Items) != 10 {
		t.Fatalf("got %d items, want 10", len(res.Items))
	}

	// A token here would hand the client a page that can only come back empty.
	if res.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want none", res.NextPageToken)
	}
}

func TestSearchEmitsNoTokenWhenTheRestOfTheRangeCannotMatch(t *testing.T) {
	t.Parallel()

	recs := records(30)
	for i := range recs[5:] {
		recs[i+5].Fields["message"] = "unrelated"
	}

	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.PageSize = 5
	q.Include = []string{"statement"}

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(res.Items) != 5 {
		t.Fatalf("got %d items, want 5", len(res.Items))
	}

	if res.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want none", res.NextPageToken)
	}
}

func TestSearchStopsAtMaxScan(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(50)}
	svc := newTestService(t, p, config.LogSearchConfig{MaxScan: 7})

	q := testQuery()
	q.PageSize = 100
	q.Include = []string{"statement 0"}

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if res.Scanned != 7 {
		t.Errorf("Scanned = %d, want 7", res.Scanned)
	}

	if !res.Partial {
		t.Error("Partial = false, want true on a capped scan")
	}

	// The client can keep scanning even though no further match was seen.
	if res.NextPageToken == "" {
		t.Error("a capped scan handed out no resume token")
	}
}

func TestSearchCapsThePageSize(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(30)}
	svc := newTestService(t, p, config.LogSearchConfig{MaxPageSize: 4})

	q := testQuery()
	q.PageSize = 1000

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(res.Items) != 4 {
		t.Errorf("got %d items, want the configured max of 4", len(res.Items))
	}
}

func TestSearchMasksCredentialsInTheText(t *testing.T) {
	t.Parallel()

	recs := []source.Record{record(0, "CREATE ROLE app LOGIN PASSWORD 'hunter2'")}
	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newTestService(t, p, config.LogSearchConfig{})

	res, err := svc.Search(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if strings.Contains(res.Items[0].Text, "hunter2") {
		t.Errorf("password left in the text: %q", res.Items[0].Text)
	}

	if strings.Contains(res.Items[0].Fields["message"], "hunter2") {
		t.Errorf("password left in the field map: %q", res.Items[0].Fields["message"])
	}
}

func TestSearchAppliesDashaSideFilters(t *testing.T) {
	t.Parallel()

	recs := []source.Record{
		record(0, "checkpoint starting"),
		record(1, "deadlock detected"),
		record(2, "checkpoint complete"),
	}

	tests := []struct {
		name  string
		apply func(q *SearchQuery)
		want  []string
	}{
		{
			name:  "include is an AND of substrings",
			apply: func(q *SearchQuery) { q.Include = []string{"checkpoint", "complete"} },
			want:  []string{"checkpoint complete"},
		},
		{
			name:  "exclude drops matching records",
			apply: func(q *SearchQuery) { q.Exclude = []string{"checkpoint"} },
			want:  []string{"deadlock detected"},
		},
		{
			name:  "database is a case-insensitive substring",
			apply: func(q *SearchQuery) { q.Database = "BILL" },
			want:  []string{"deadlock detected"},
		},
		{
			name:  "user is a case-insensitive substring",
			apply: func(q *SearchQuery) { q.User = "admin" },
			want:  []string{"deadlock detected"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &fakeProvider{fields: testFieldMap(t), records: recs}
			svc := newTestService(t, p, config.LogSearchConfig{})

			q := testQuery()
			tt.apply(&q)

			res, err := svc.Search(context.Background(), q)
			if err != nil {
				t.Fatalf("search: %v", err)
			}

			got := make([]string, 0, len(res.Items))
			for _, e := range res.Items {
				got = append(got, e.Text)
			}

			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchDedupGroupsStructurallyEqualMessages(t *testing.T) {
	t.Parallel()

	recs := []source.Record{
		record(0, "login time: 656 microseconds"),
		record(1, "login time: 698 microseconds"),
		record(2, "login time: 701 microseconds"),
		record(3, "deadlock detected"),
	}

	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Dedup = true

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if !res.Dedup || res.NextPageToken != "" {
		t.Errorf("Dedup = %v, NextPageToken = %q", res.Dedup, res.NextPageToken)
	}

	if len(res.Items) != 2 {
		t.Fatalf("got %d groups, want 2", len(res.Items))
	}

	top := res.Items[0]
	if top.Count != 3 {
		t.Errorf("largest group Count = %d, want 3", top.Count)
	}

	if !strings.Contains(top.Text, displayPlaceholder) {
		t.Errorf("group text %q is not a template", top.Text)
	}

	if !top.FirstSeen.Equal(recs[0].Timestamp) || !top.LastSeen.Equal(recs[2].Timestamp) {
		t.Errorf("group window = %v..%v", top.FirstSeen, top.LastSeen)
	}

	// The severity shown is the worst of the group, not the last one read.
	if top.Severity != "ERROR" {
		t.Errorf("group severity = %q, want ERROR", top.Severity)
	}
}

func TestSearchRejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		apply func(q *SearchQuery)
	}{
		{"inverted range", func(q *SearchQuery) { q.From, q.To = q.To, q.From }},
		{"dedup with a page token", func(q *SearchQuery) { q.Dedup = true; q.PageToken = "t1" }},
		{"unknown severity", func(q *SearchQuery) { q.Severities = []string{"verbose"} }},
		{"host outside the cluster", func(q *SearchQuery) { q.Host = "db-9" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &fakeProvider{fields: testFieldMap(t), records: records(3)}
			svc := newTestService(t, p, config.LogSearchConfig{})

			q := testQuery()
			tt.apply(&q)

			if _, err := svc.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
				t.Errorf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestSearchPassesCanonicalSeveritiesDown(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(2)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Severities = []string{"error", ""}

	if _, err := svc.Search(context.Background(), q); err != nil {
		t.Fatalf("search: %v", err)
	}
}

func TestSearchRejectsUnknownClusterAndStream(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(3)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Cluster = "absent"

	if _, err := svc.Search(context.Background(), q); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}

	q = testQuery()
	q.Stream = source.StreamPooler

	if _, err := svc.Search(context.Background(), q); !errors.Is(err, ErrUnsupported) {
		t.Errorf("error = %v, want ErrUnsupported", err)
	}
}

func TestSearchClassifiesSourceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{"unknown stream", fmt.Errorf("%w: pg", source.ErrStream), ErrUnsupported},
		{"cluster not served", fmt.Errorf("%w: gone", source.ErrUnavailable), ErrUnsupported},
		{"foreign page token", fmt.Errorf("%w: token", source.ErrInvalidToken), ErrInvalid},
		{"anything else", errors.New("log store answered 500"), ErrUpstream},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &fakeProvider{fields: testFieldMap(t), err: tt.err}
			svc := newTestService(t, p, config.LogSearchConfig{})

			_, err := svc.Search(context.Background(), testQuery())
			if !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSearchUpstreamErrorCarriesNoCredentials(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields: testFieldMap(t),
		err:    errors.New("dial failed: host=os user=dasha password=hunter2"),
	}
	svc := newTestService(t, p, config.LogSearchConfig{})

	_, err := svc.Search(context.Background(), testQuery())
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks the password: %v", err)
	}
}

func TestSearchKeepsWhatAPartialReadCollected(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields:  testFieldMap(t),
		records: records(4),
		err:     fmt.Errorf("%w: too many records share one timestamp", source.ErrPartial),
	}
	svc := newTestService(t, p, config.LogSearchConfig{})

	res, err := svc.Search(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(res.Items) != 4 || !res.Partial {
		t.Errorf("got %d items, Partial = %v; want 4, true", len(res.Items), res.Partial)
	}

	// A source that stopped early cannot say where to resume from.
	if res.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want none after a partial read", res.NextPageToken)
	}
}

func TestSearchDedupKeepsWhatAPartialReadCollected(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields:  testFieldMap(t),
		records: records(4),
		err:     fmt.Errorf("%w: stopped", source.ErrPartial),
	}
	svc := newTestService(t, p, config.LogSearchConfig{})

	q := testQuery()
	q.Dedup = true

	res, err := svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(res.Items) == 0 || !res.Partial {
		t.Errorf("got %d groups, Partial = %v; want groups and true", len(res.Items), res.Partial)
	}
}

func TestSearchTimeoutReturnsAResumablePage(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(3), hang: true}
	svc := newTestService(t, p, config.LogSearchConfig{TimeoutSeconds: 1})

	res, err := svc.Search(context.Background(), testQuery())
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(res.Items) != 3 || !res.Partial {
		t.Fatalf("got %d items, Partial = %v; want 3, true", len(res.Items), res.Partial)
	}

	if res.NextPageToken != "t2" {
		t.Errorf("NextPageToken = %q, want the last record's cursor", res.NextPageToken)
	}
}

func TestSearchTimeoutWithNothingCollectedFails(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), hang: true}
	svc := newTestService(t, p, config.LogSearchConfig{TimeoutSeconds: 1})

	if _, err := svc.Search(context.Background(), testQuery()); !errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, want ErrTimeout", err)
	}
}

func TestSearchPropagatesAClientDisconnect(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), hang: true}
	svc := newTestService(t, p, config.LogSearchConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.Search(ctx, testQuery()); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestCheckMasksTheSample(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields: testFieldMap(t),
		check: source.CheckResult{
			Target:    "pg-logs-prod-*",
			Documents: 42,
			Found:     map[string]string{source.RoleText: "message"},
			Missing:   []string{source.RolePID},
			Types:     map[string]string{"message": "text"},
			Sample:    map[string]string{"message": "ALTER USER admin PASSWORD 'hunter2'"},
		},
	}
	svc := newTestService(t, p, config.LogSearchConfig{})

	report, err := svc.Check(context.Background(), "prod", testStream)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if report.Source != "main" || report.Documents != 42 || report.Target != "pg-logs-prod-*" {
		t.Errorf("report = %+v", report)
	}

	if strings.Contains(report.Sample["message"], "hunter2") {
		t.Errorf("sample leaks the password: %q", report.Sample["message"])
	}
}

func TestSourceNameResolvesOnlyServedClusters(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t)}
	svc := newTestService(t, p, config.LogSearchConfig{})

	if got := svc.SourceName(context.Background(), "prod"); got != "main" {
		t.Errorf("SourceName(prod) = %q, want main", got)
	}

	if got := svc.SourceName(context.Background(), "absent"); got != "" {
		t.Errorf("SourceName(absent) = %q, want empty", got)
	}
}
