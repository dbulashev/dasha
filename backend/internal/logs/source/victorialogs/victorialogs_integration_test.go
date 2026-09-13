//go:build integration

package victorialogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

const testCluster = "prod"

var baseURL string

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image(),
			ExposedPorts: []string{"9428/tcp"},
			WaitingFor: wait.ForHTTP("/health").
				WithPort("9428/tcp").
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("start victorialogs: %v", err))
	}

	host, err := container.Host(ctx)
	if err != nil {
		panic(err)
	}

	port, err := container.MappedPort(ctx, "9428/tcp")
	if err != nil {
		panic(err)
	}

	baseURL = fmt.Sprintf("http://%s:%s", host, port.Port())

	if err := seed(ctx); err != nil {
		panic(fmt.Sprintf("seed victorialogs: %v", err))
	}

	code := m.Run()

	_ = container.Terminate(ctx)

	os.Exit(code)
}

func image() string {
	if v := os.Getenv("VICTORIALOGS_IMAGE"); v != "" {
		return v
	}

	return "victoriametrics/victoria-logs:v1.52.0"
}

// seedRecord is one jsonlog record as the delivery agent writes it into
// VictoriaLogs: the message in _msg, the time in _time.
type seedRecord struct {
	Time          string `json:"_time"`
	Msg           string `json:"_msg"`
	ErrorSeverity string `json:"error_severity"`
	Dbname        string `json:"dbname"`
	User          string `json:"user"`
	PID           string `json:"pid"`
	QueryID       string `json:"query_id"`
	StateCode     string `json:"state_code"`
	Cluster       string `json:"cluster"`
	Host          string `json:"host"`
	App           string `json:"app"`
}

// seedStart sits inside the window Check probes, so the same set covers both
// search and check.
var seedStart = time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Millisecond)

// seedRecords builds a set covering every filter dimension. Records come in
// pairs sharing a timestamp, so a cursor always has to carry a skip list.
func seedRecords() []seedRecord {
	severities := []string{"LOG", "ERROR", "FATAL", "WARNING"}
	hosts := []string{"db-1.example.net", "db-2.example.net"}
	dbs := []string{"shop", "billing"}
	users := []string{"app", "admin"}

	out := make([]seedRecord, 0, 42)

	for i := range 40 {
		ts := seedStart.Add(time.Duration(i/2) * time.Second)

		out = append(out, seedRecord{
			Time:          ts.Format(time.RFC3339Nano),
			Msg:           fmt.Sprintf("statement %d took %d ms", i, i*7),
			ErrorSeverity: severities[i%len(severities)],
			Dbname:        dbs[i%len(dbs)],
			User:          users[i%len(users)],
			PID:           fmt.Sprintf("%d", 1000+i),
			QueryID:       queryID(i),
			StateCode:     []string{"00000", "XX000", "XX000", "01000"}[i%4],
			Cluster:       testCluster,
			Host:          hosts[i%len(hosts)],
			App:           "postgres",
		})
	}

	// Two records with the same content at the same timestamp: the cursor must
	// keep both, not collapse them into one.
	dup := seedRecord{
		Time:          seedStart.Add(100 * time.Second).Format(time.RFC3339Nano),
		Msg:           "duplicate line",
		ErrorSeverity: "LOG",
		Dbname:        "shop",
		User:          "app",
		PID:           "9999",
		QueryID:       queryID(0),
		StateCode:     "00000",
		Cluster:       testCluster,
		Host:          "db-1.example.net",
		App:           "postgres",
	}

	return append(out, dup, dup)
}

// queryID mimics the statement identifier PostgreSQL writes: a signed 64-bit
// value past the float64 range of exact integers, negative as often as positive.
func queryID(i int) string {
	id := int64(4452854032459450605) + int64(i)*1000003
	if i%2 == 1 {
		id = -id
	}

	return fmt.Sprintf("%d", id)
}

func seed(ctx context.Context) error {
	var body bytes.Buffer

	for _, r := range seedRecords() {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}

		body.Write(line)
		body.WriteString("\n")
	}

	url := baseURL + "/insert/jsonline?_time_field=_time&_msg_field=_msg&_stream_fields=cluster,host"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/stream+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("insert: %s", resp.Status)
	}

	return waitForRecords(ctx, len(seedRecords()))
}

// waitForRecords flushes the in-memory buffer and waits until the whole set is
// searchable.
func waitForRecords(ctx context.Context, want int) error {
	deadline := time.Now().Add(30 * time.Second)

	for {
		flush, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/internal/force_flush", nil)
		if err != nil {
			return err
		}

		resp, err := http.DefaultClient.Do(flush)
		if err != nil {
			return err
		}

		_ = resp.Body.Close()

		got := collect(context.Background(), nil, testProvider(nil), testParams(source.Filter{}), 0)
		if len(got) >= want {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("only %d of %d records became searchable", len(got), want)
		}

		time.Sleep(time.Second)
	}
}

func testProvider(mutate func(*config.LogStreamConfig, *config.LogSourceConfig)) *Provider {
	stream := config.LogStreamConfig{
		StreamSelector: map[string]string{"cluster": "{{ .Cluster }}"},
		Selector:       map[string]string{"app": "postgres"},
		FieldMap: config.LogFieldMapConfig{
			Preset:    source.PresetJSONLog,
			Timestamp: "_time",
			Text:      "_msg",
			Host:      "host",
			HostMatch: source.HostMatchSuffix,
		},
	}

	cfg := config.LogSourceConfig{
		Type:      config.LogSourceTypeVictoriaLogs,
		Addresses: []string{baseURL},
		Auth:      config.LogSourceAuthConfig{Kind: config.LogAuthNone},
		BatchSize: 7,
	}

	if mutate != nil {
		mutate(&stream, &cfg)
	}

	cfg.Streams = map[string]config.LogStreamConfig{source.StreamPostgreSQL: stream}

	p, err := New(cfg, config.LogSearchConfig{TimeoutSeconds: 30}, zap.NewNop())
	if err != nil {
		panic(fmt.Sprintf("new provider: %v", err))
	}

	return p
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

func collect(ctx context.Context, t *testing.T, p *Provider, params source.StreamParams, limit int) []source.Record {
	if t != nil {
		t.Helper()
	}

	var out []source.Record

	err := p.Stream(ctx, params, func(r source.Record) bool {
		out = append(out, r)

		return limit <= 0 || len(out) < limit
	})
	if err != nil && t != nil {
		t.Fatalf("stream: %v", err)
	}

	return out
}

// signature keys a record for a multiset comparison: two identical records
// sharing a timestamp are two records.
func signature(r source.Record) string {
	return r.Fields["pid"] + "|" + r.Fields["_msg"]
}

// keyed is one record reduced to what a comparison may rely on: its timestamp
// and a key identifying it.
type keyed struct {
	ts  time.Time
	key string
}

// sortTies orders the keys of one timestamp among themselves: which record of a
// timestamp comes first is the store's own choice and does not survive a
// re-read.
func sortTies(items []keyed) []string {
	out := make([]string, 0, len(items))

	for i := 0; i < len(items); {
		j := i
		for j < len(items) && items[j].ts.Equal(items[i].ts) {
			j++
		}

		run := make([]string, 0, j-i)
		for _, it := range items[i:j] {
			run = append(run, it.key)
		}

		sort.Strings(run)
		out = append(out, run...)

		i = j
	}

	return out
}

func recordKeys(records []source.Record) []string {
	items := make([]keyed, 0, len(records))
	for _, r := range records {
		items = append(items, keyed{ts: r.Timestamp, key: signature(r)})
	}

	return sortTies(items)
}

func counts(records []source.Record) map[string]int {
	out := map[string]int{}
	for _, r := range records {
		out[signature(r)]++
	}

	return out
}

func TestStreamReadsEveryRecordNewestFirst(t *testing.T) {
	got := collect(context.Background(), t, testProvider(nil), testParams(source.Filter{}), 0)

	if len(got) != len(seedRecords()) {
		t.Fatalf("read %d records, want %d", len(got), len(seedRecords()))
	}

	for i, r := range got {
		if i > 0 && r.Timestamp.After(got[i-1].Timestamp) {
			t.Fatalf("record %d goes forward in time", i)
		}
	}

	if counts(got)["9999|duplicate line"] != 2 {
		t.Errorf("the two identical records at one timestamp came back as %d",
			counts(got)["9999|duplicate line"])
	}
}

// TestStreamKeepsInt64FieldsExact: a store that keeps a column of mixed-sign
// integers as float64 hands back query ids with their low digits rounded off.
func TestStreamKeepsInt64FieldsExact(t *testing.T) {
	want := map[string]bool{}
	for _, r := range seedRecords() {
		want[r.QueryID] = true
	}

	for _, r := range collect(context.Background(), t, testProvider(nil), testParams(source.Filter{}), 0) {
		if !want[r.Fields["query_id"]] {
			t.Errorf("query_id %q was never seeded", r.Fields["query_id"])
		}
	}
}

// TestStreamPagesPastATimestampWiderThanTheBatch: records sharing a timestamp
// are read past the skip list, so batch_size does not cap how many of them a
// search can reach.
func TestStreamPagesPastATimestampWiderThanTheBatch(t *testing.T) {
	p := testProvider(func(_ *config.LogStreamConfig, cfg *config.LogSourceConfig) {
		cfg.BatchSize = 1
	})

	got := collect(context.Background(), t, p, testParams(source.Filter{}), 0)

	if len(got) != len(seedRecords()) {
		t.Fatalf("read %d records with batch_size 1, want %d", len(got), len(seedRecords()))
	}
}

// TestStreamStopsAtMaxBoundaryIDs: the cap on the hashes one cursor carries is
// what ends a read of a timestamp too wide to page through.
func TestStreamStopsAtMaxBoundaryIDs(t *testing.T) {
	p := testProvider(func(_ *config.LogStreamConfig, cfg *config.LogSourceConfig) {
		cfg.BatchSize = 1
	})
	p.maxBoundaryIDs = 1

	err := p.Stream(context.Background(), testParams(source.Filter{}), func(source.Record) bool { return true })
	if !errors.Is(err, source.ErrPartial) {
		t.Fatalf("stream error = %v, want ErrPartial", err)
	}
}

func TestStreamResumesFromCursorWithoutGapOrRepeat(t *testing.T) {
	p := testProvider(nil)
	ctx := context.Background()

	all := collect(ctx, t, p, testParams(source.Filter{}), 0)
	first := collect(ctx, t, p, testParams(source.Filter{}), 13)

	params := testParams(source.Filter{})
	params.Token = first[len(first)-1].Token

	rest := collect(ctx, t, p, params, 0)

	if len(first)+len(rest) != len(all) {
		t.Fatalf("resumed read covers %d+%d records, want %d", len(first), len(rest), len(all))
	}

	want := recordKeys(all)
	got := recordKeys(append(append([]source.Record{}, first...), rest...))

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d differs after resume: %s vs %s", i, got[i], want[i])
		}
	}
}

// TestStreamKeepsTheRequestedUpperBoundOnAReusedToken: a token reaching back
// from a wider range must not lift the bound of the next read.
func TestStreamKeepsTheRequestedUpperBoundOnAReusedToken(t *testing.T) {
	p := testProvider(nil)
	ctx := context.Background()

	all := collect(ctx, t, p, testParams(source.Filter{}), 0)

	params := testParams(source.Filter{})
	params.To = all[len(all)/2].Timestamp
	params.Token = all[0].Token

	got := collect(ctx, t, p, params, 0)
	if len(got) == 0 {
		t.Fatal("the narrowed read came back empty")
	}

	for _, r := range got {
		if r.Timestamp.After(params.To) {
			t.Fatalf("record at %s lies past the requested bound %s", r.Timestamp, params.To)
		}
	}
}

// TestPushdownOnlyNarrows is the guarantee the design rests on: what the store
// filters out is exactly what a full scan would have dropped anyway.
func TestPushdownOnlyNarrows(t *testing.T) {
	p := testProvider(nil)
	fm := p.Fields(source.StreamPostgreSQL)
	ctx := context.Background()

	full := collect(ctx, t, p, testParams(source.Filter{}), 0)

	for _, f := range []source.Filter{
		{Severities: []string{"ERROR"}},
		{Severities: []string{"ERROR", "FATAL"}},
		{Host: "db-1"},
		{Severities: []string{"ERROR"}, Host: "db-2"},
	} {
		want := map[string]int{}

		for _, r := range full {
			if matchesFilter(fm, r, f) {
				want[signature(r)]++
			}
		}

		got := counts(collect(ctx, t, p, testParams(f), 0))

		if len(got) != len(want) {
			t.Fatalf("filter %+v: pushdown returned %d kinds of record, full scan %d", f, len(got), len(want))
		}

		for sig, n := range want {
			if got[sig] != n {
				t.Fatalf("filter %+v: pushdown returned %d of %q, full scan %d", f, got[sig], sig, n)
			}
		}
	}
}

func matchesFilter(fm source.FieldMap, r source.Record, f source.Filter) bool {
	if len(f.Severities) > 0 && !slices.Contains(f.Severities, r.Fields[fm.Severity]) {
		return false
	}

	if f.Host != "" {
		host := r.Fields[fm.Host]
		if host != f.Host && !strings.HasPrefix(host, f.Host+".") {
			return false
		}
	}

	return true
}

func TestCheckReportsTheStreamAndASample(t *testing.T) {
	res, err := testProvider(nil).Check(context.Background(),
		config.Cluster{Name: testCluster}, source.StreamPostgreSQL)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if res.Target != `{cluster="prod"} "app":="postgres"` {
		t.Errorf("target = %q", res.Target)
	}

	if len(res.Missing) != 0 {
		t.Errorf("missing roles = %v, want none", res.Missing)
	}

	for _, role := range []string{source.RoleTimestamp, source.RoleSeverity, source.RoleText, source.RoleHost} {
		if res.Found[role] == "" {
			t.Errorf("role %q not found upstream", role)
		}
	}

	if len(res.Types) != 0 {
		t.Errorf("field types = %v, want none: VictoriaLogs holds every field as a string", res.Types)
	}

	if res.Documents != len(seedRecords()) {
		t.Errorf("documents = %d, want %d", res.Documents, len(seedRecords()))
	}

	if res.Sample["_msg"] == "" {
		t.Errorf("sample = %v", res.Sample)
	}
}

// TestCheckReportsAMisspelledField: a filter on a field VictoriaLogs does not
// hold returns an empty answer rather than an error, so the check is the only
// place the typo shows.
func TestCheckReportsAMisspelledField(t *testing.T) {
	p := testProvider(func(sc *config.LogStreamConfig, _ *config.LogSourceConfig) {
		sc.FieldMap.Host = "hst"
	})

	res, err := p.Check(context.Background(), config.Cluster{Name: testCluster}, source.StreamPostgreSQL)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if !slices.Contains(res.Missing, source.RoleHost) {
		t.Errorf("missing roles = %v, want the host among them", res.Missing)
	}
}

// TestCheckOnAnEmptyWindowSaysNothingAboutTheFields: field_names answers for
// the matching records only, so an idle window must not read as a broken map.
func TestCheckOnAnEmptyWindowSaysNothingAboutTheFields(t *testing.T) {
	p := testProvider(func(_ *config.LogStreamConfig, cfg *config.LogSourceConfig) {
		cfg.Tenant = config.LogSourceTenantConfig{AccountID: 12, ProjectID: 34}
	})

	res, err := p.Check(context.Background(), config.Cluster{Name: testCluster}, source.StreamPostgreSQL)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if res.Documents != 0 || res.Sample != nil {
		t.Fatalf("documents = %d, sample = %v, want an empty window", res.Documents, res.Sample)
	}

	if len(res.Missing) != 0 || len(res.Found) != 0 {
		t.Errorf("missing = %v, found = %v, want neither", res.Missing, res.Found)
	}
}

// TestAnotherTenantHoldsNothing: the tenant headers address a namespace of
// their own, and a wrong one reads as an empty store.
func TestAnotherTenantHoldsNothing(t *testing.T) {
	p := testProvider(func(_ *config.LogStreamConfig, cfg *config.LogSourceConfig) {
		cfg.Tenant = config.LogSourceTenantConfig{AccountID: 12, ProjectID: 34}
	})

	if got := collect(context.Background(), t, p, testParams(source.Filter{}), 0); len(got) != 0 {
		t.Fatalf("read %d records of another tenant", len(got))
	}
}

func TestUnknownStream(t *testing.T) {
	params := testParams(source.Filter{})
	params.Stream = source.StreamPooler

	err := testProvider(nil).Stream(context.Background(), params, func(source.Record) bool { return true })
	if !errors.Is(err, source.ErrStream) {
		t.Fatalf("stream error = %v, want ErrStream", err)
	}
}
