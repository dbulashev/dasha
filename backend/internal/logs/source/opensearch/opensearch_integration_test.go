//go:build integration

package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

const (
	testIndex   = "pg-logs-prod-000001"
	testCluster = "prod"
	// testService is stored in a field the index analyzes, the way a default
	// dynamic mapping treats every string.
	testService = "PostgreSQL Server"
	// malformedIndex sits behind the same pattern as testIndex.
	malformedIndex = "pg-logs-prod-000002"
)

var baseURL string

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image(),
			ExposedPorts: []string{"9200/tcp"},
			Env: map[string]string{
				"discovery.type":              "single-node",
				"DISABLE_SECURITY_PLUGIN":     "true",
				"DISABLE_INSTALL_DEMO_CONFIG": "true",
				"OPENSEARCH_JAVA_OPTS":        "-Xms512m -Xmx512m",
			},
			WaitingFor: wait.ForHTTP("/_cluster/health").
				WithPort("9200/tcp").
				WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("start opensearch: %v", err))
	}

	host, err := container.Host(ctx)
	if err != nil {
		panic(err)
	}

	port, err := container.MappedPort(ctx, "9200/tcp")
	if err != nil {
		panic(err)
	}

	baseURL = fmt.Sprintf("http://%s:%s", host, port.Port())

	if err := seed(ctx); err != nil {
		panic(fmt.Sprintf("seed index: %v", err))
	}

	code := m.Run()

	_ = container.Terminate(ctx)

	os.Exit(code)
}

func image() string {
	if v := os.Getenv("OPENSEARCH_IMAGE"); v != "" {
		return v
	}

	return "opensearchproject/opensearch:2"
}

// seedRecord is one jsonlog document as PostgreSQL writes it, plus the fields
// the delivery agent adds.
type seedRecord struct {
	Timestamp     string `json:"@timestamp"`
	ErrorSeverity string `json:"error_severity"`
	Message       string `json:"message"`
	Dbname        string `json:"dbname"`
	User          string `json:"user"`
	PID           int    `json:"pid"`
	Cluster       string `json:"cluster"`
	Service       string `json:"service"`
	Host          struct {
		Name string `json:"name"`
	} `json:"host"`
}

var seedStart = time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

// seedRecords builds a set covering every filter dimension, including records
// that share a timestamp so the cursor's boundary handling is exercised.
func seedRecords() []seedRecord {
	severities := []string{"LOG", "ERROR", "FATAL", "WARNING"}
	hosts := []string{"db-1.example.net", "db-2.example.net"}
	dbs := []string{"shop", "billing"}
	users := []string{"app", "admin"}

	var out []seedRecord

	for i := range 40 {
		// Records come in pairs sharing a timestamp, so a cursor always has to
		// carry boundary ids.
		ts := seedStart.Add(time.Duration(i/2) * time.Second)

		r := seedRecord{
			Timestamp:     ts.Format(time.RFC3339Nano),
			ErrorSeverity: severities[i%len(severities)],
			Message:       fmt.Sprintf("statement %d took %d ms", i, i*7),
			Dbname:        dbs[i%len(dbs)],
			User:          users[i%len(users)],
			PID:           1000 + i,
			Cluster:       testCluster,
			Service:       testService,
		}
		r.Host.Name = hosts[i%len(hosts)]

		out = append(out, r)
	}

	return out
}

func seed(ctx context.Context) error {
	mapping := map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"@timestamp":     map[string]any{"type": "date"},
				"error_severity": map[string]any{"type": "keyword"},
				"message":        map[string]any{"type": "text"},
				"dbname":         map[string]any{"type": "keyword"},
				"user":           map[string]any{"type": "keyword"},
				"cluster":        map[string]any{"type": "keyword"},
				"service": map[string]any{
					"type":   "text",
					"fields": map[string]any{"keyword": map[string]any{"type": "keyword"}},
				},
				"host": map[string]any{"properties": map[string]any{"name": map[string]any{"type": "keyword"}}},
			},
		},
	}

	if err := request(ctx, http.MethodPut, "/"+testIndex, mapping); err != nil {
		return err
	}

	var bulk bytes.Buffer

	for i, r := range seedRecords() {
		meta := fmt.Sprintf(`{"index":{"_index":%q,"_id":"doc-%d"}}`, testIndex, i)
		bulk.WriteString(meta)
		bulk.WriteString("\n")

		line, err := json.Marshal(r)
		if err != nil {
			return err
		}

		bulk.Write(line)
		bulk.WriteString("\n")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/_bulk?refresh=true", &bulk)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("bulk index: %s", resp.Status)
	}

	return seedMalformed(ctx)
}

// seedMalformed adds a second index behind the same pattern holding a record
// whose delivery agent named the timestamp field something else.
func seedMalformed(ctx context.Context) error {
	return request(ctx, http.MethodPut,
		"/"+malformedIndex+"/_doc/no-timestamp?refresh=true",
		map[string]any{"time": seedStart.Format(time.RFC3339Nano), "message": "agent wrote no @timestamp"})
}

func request(ctx context.Context, method, path string, body any) error {
	var buf bytes.Buffer

	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, &buf)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}

	return nil
}

func testProvider(t *testing.T, batchSize int) *Provider {
	t.Helper()

	return testProviderWith(t, batchSize, nil)
}

func testProviderWith(t *testing.T, batchSize int, mutate func(*config.LogStreamConfig)) *Provider {
	t.Helper()

	stream := config.LogStreamConfig{
		Index:    "pg-logs-{{ .Cluster }}-*",
		Selector: map[string]string{"cluster": "{{ .Cluster }}"},
		FieldMap: config.LogFieldMapConfig{
			Preset:    source.PresetJSONLog,
			Timestamp: "@timestamp",
			Host:      "host.name",
			HostMatch: source.HostMatchSuffix,
		},
	}

	if mutate != nil {
		mutate(&stream)
	}

	p, err := New(config.LogSourceConfig{
		Type:      config.LogSourceTypeOpenSearch,
		Addresses: []string{baseURL},
		Auth:      config.LogSourceAuthConfig{Kind: config.LogAuthNone},
		BatchSize: batchSize,
		Streams:   map[string]config.LogStreamConfig{source.StreamPostgreSQL: stream},
	}, config.LogSearchConfig{TimeoutSeconds: 30}, zap.NewNop())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	return p
}

// TestSelectorOnAnAnalyzedFieldNeedsItsKeywordField: an exact-match filter on a
// field the store analyzed matches nothing until keyword_fields names the
// field to filter on instead.
func TestSelectorOnAnAnalyzedFieldNeedsItsKeywordField(t *testing.T) {
	analyzed := testProviderWith(t, 10, func(sc *config.LogStreamConfig) {
		sc.Selector = map[string]string{"service": testService}
	})

	if got := collect(t, analyzed, testParams(source.Filter{}), 0); len(got) != 0 {
		t.Fatalf("read %d records through an analyzed field, want 0", len(got))
	}

	keyword := testProviderWith(t, 10, func(sc *config.LogStreamConfig) {
		sc.Selector = map[string]string{"service": testService}
		sc.FieldMap.KeywordFields = map[string]string{"service": "service.keyword"}
	})

	if got := collect(t, keyword, testParams(source.Filter{}), 0); len(got) != len(seedRecords()) {
		t.Fatalf("read %d records through the keyword field, want %d", len(got), len(seedRecords()))
	}
}

func testParams(filter source.Filter) source.StreamParams {
	return source.StreamParams{
		Cluster: config.Cluster{Name: testCluster, Hosts: []config.Host{"db-1", "db-2"}},
		Stream:  source.StreamPostgreSQL,
		From:    seedStart.Add(-time.Hour),
		To:      seedStart.Add(time.Hour),
		Filter:  filter,
	}
}

func collect(t *testing.T, p *Provider, params source.StreamParams, limit int) []source.Record {
	t.Helper()

	var out []source.Record

	err := p.Stream(context.Background(), params, func(r source.Record) bool {
		out = append(out, r)

		return limit <= 0 || len(out) < limit
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	return out
}

func TestStreamReadsEveryRecordInOrder(t *testing.T) {
	p := testProvider(t, 7)

	got := collect(t, p, testParams(source.Filter{}), 0)

	if len(got) != len(seedRecords()) {
		t.Fatalf("read %d records, want %d", len(got), len(seedRecords()))
	}

	seen := map[string]bool{}

	for i, r := range got {
		if i > 0 && r.Timestamp.Before(got[i-1].Timestamp) {
			t.Fatalf("record %d goes back in time", i)
		}

		if seen[r.Fields["pid"]] {
			t.Fatalf("record with pid %s delivered twice", r.Fields["pid"])
		}

		seen[r.Fields["pid"]] = true
	}
}

// TestStreamSkipsRecordsWithoutAUsableTimestamp: one document the pipeline
// wrote without the mapped timestamp must not end the read of the whole
// pattern.
func TestStreamSkipsRecordsWithoutAUsableTimestamp(t *testing.T) {
	p := testProvider(t, 7)

	got := collect(t, p, testParams(source.Filter{}), 0)

	if len(got) != len(seedRecords()) {
		t.Fatalf("read %d records, want %d", len(got), len(seedRecords()))
	}

	for _, r := range got {
		if r.Fields["time"] != "" {
			t.Fatalf("record without the mapped timestamp was delivered: %v", r.Fields)
		}
	}
}

// TestStreamPagesPastATimestampWiderThanTheBatch: records sharing a timestamp
// are read past the skip list, so batch_size does not cap how many of them a
// search can reach.
func TestStreamPagesPastATimestampWiderThanTheBatch(t *testing.T) {
	p := testProvider(t, 1)

	got := collect(t, p, testParams(source.Filter{}), 0)

	if len(got) != len(seedRecords()) {
		t.Fatalf("read %d records with batch_size 1, want %d", len(got), len(seedRecords()))
	}
}

// TestStreamStopsAtMaxBoundaryIDs: the cap on the ids one cursor carries is
// what ends a read of a timestamp too wide to page through.
func TestStreamStopsAtMaxBoundaryIDs(t *testing.T) {
	p := testProviderWith(t, 1, nil)
	p.maxBoundaryIDs = 1

	err := p.Stream(context.Background(), testParams(source.Filter{}), func(source.Record) bool { return true })
	if !errors.Is(err, source.ErrPartial) {
		t.Fatalf("stream error = %v, want ErrPartial", err)
	}

	if !strings.Contains(err.Error(), "1") {
		t.Errorf("error does not report the cap: %v", err)
	}
}

func TestStreamResumesFromCursorWithoutGapOrRepeat(t *testing.T) {
	p := testProvider(t, 5)
	all := collect(t, p, testParams(source.Filter{}), 0)

	first := collect(t, p, testParams(source.Filter{}), 13)

	params := testParams(source.Filter{})
	params.Token = first[len(first)-1].Token

	rest := collect(t, p, params, 0)

	if len(first)+len(rest) != len(all) {
		t.Fatalf("resumed read covers %d+%d records, want %d", len(first), len(rest), len(all))
	}

	for i, r := range append(append([]source.Record{}, first...), rest...) {
		if r.Fields["pid"] != all[i].Fields["pid"] {
			t.Fatalf("record %d differs after resume: %s vs %s", i, r.Fields["pid"], all[i].Fields["pid"])
		}
	}
}

// TestPushdownOnlyNarrows is the guarantee the design rests on: what the store
// filters out is exactly what a full scan would have dropped anyway.
func TestPushdownOnlyNarrows(t *testing.T) {
	p := testProvider(t, 9)
	fm := p.Fields(source.StreamPostgreSQL)

	filters := []source.Filter{
		{Severities: []string{"ERROR"}},
		{Severities: []string{"ERROR", "FATAL"}},
		{Host: "db-1"},
		{Severities: []string{"ERROR"}, Host: "db-2"},
	}

	full := collect(t, p, testParams(source.Filter{}), 0)

	for _, f := range filters {
		pushed := collect(t, p, testParams(f), 0)

		want := map[string]bool{}

		for _, r := range full {
			if matchesFilter(fm, r, f) {
				want[r.Fields["pid"]] = true
			}
		}

		if len(pushed) != len(want) {
			t.Fatalf("filter %+v: pushdown returned %d records, full scan %d", f, len(pushed), len(want))
		}

		for _, r := range pushed {
			if !want[r.Fields["pid"]] {
				t.Fatalf("filter %+v: pushdown returned pid %s a full scan drops", f, r.Fields["pid"])
			}
		}
	}
}

func matchesFilter(fm source.FieldMap, r source.Record, f source.Filter) bool {
	if len(f.Severities) > 0 {
		found := false

		for _, s := range f.Severities {
			if r.Fields[fm.Severity] == s {
				found = true

				break
			}
		}

		if !found {
			return false
		}
	}

	if f.Host != "" {
		host := r.Fields[fm.Host]
		if host != f.Host && !strings.HasPrefix(host, f.Host+".") {
			return false
		}
	}

	return true
}

func TestCheckReportsMappingAndSample(t *testing.T) {
	p := testProvider(t, 10)

	res, err := p.Check(context.Background(), config.Cluster{Name: testCluster}, source.StreamPostgreSQL)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if res.Target != "pg-logs-prod-*" {
		t.Errorf("target = %q", res.Target)
	}

	if len(res.Missing) != 0 {
		t.Errorf("missing roles = %v, want none", res.Missing)
	}

	for _, role := range []string{source.RoleTimestamp, source.RoleSeverity, source.RoleText, source.RoleHost} {
		if res.Found[role] == "" {
			t.Errorf("role %q not found in the index", role)
		}
	}

	if res.Types["@timestamp"] != "date" {
		t.Errorf("timestamp indexed as %q, want date", res.Types["@timestamp"])
	}
}

// TestCheckOnEmptyIndexPatternReportsMissingFields: nothing matches the
// pattern, and the check says so instead of leaving a silently empty search
// behind.
func TestCheckOnEmptyIndexPatternReportsMissingFields(t *testing.T) {
	p := testProvider(t, 10)

	res, err := p.Check(context.Background(), config.Cluster{Name: "absent"}, source.StreamPostgreSQL)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if res.Documents != 0 {
		t.Errorf("documents = %d, want 0", res.Documents)
	}

	if len(res.Missing) == 0 {
		t.Error("no missing roles reported for an index pattern matching nothing")
	}
}

func TestUnknownStream(t *testing.T) {
	p := testProvider(t, 10)

	params := testParams(source.Filter{})
	params.Stream = source.StreamPooler

	err := p.Stream(context.Background(), params, func(source.Record) bool { return true })
	if err == nil {
		t.Fatal("expected an error for a stream the source does not serve")
	}
}
