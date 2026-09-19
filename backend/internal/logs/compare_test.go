package logs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/logs/source"
)

var baseWindow = struct{ from, to time.Time }{
	from: testWindow.from.Add(-time.Hour),
	to:   testWindow.from,
}

const compareQueryText = "Query Text: SELECT count(*) FROM orders WHERE status = 'new'\n"

func indexedPlan(ms string) string {
	return "duration: " + ms + " ms  plan:\n" + compareQueryText +
		"Aggregate  (cost=8.30..8.31 rows=1 width=8) (actual time=0.100..0.101 rows=1 loops=1)\n" +
		"  ->  Index Scan using orders_status_idx on orders  (cost=0.29..8.30 rows=1 width=0) " +
		"(actual time=0.010..0.050 rows=1 loops=1)\n" +
		"        Index Cond: (status = 'new'::text)\n"
}

func seqScanPlan(ms string) string {
	return "duration: " + ms + " ms  plan:\n" + compareQueryText +
		"Aggregate  (cost=1000.00..1000.01 rows=1 width=8) (actual time=24.000..24.001 rows=1 loops=1)\n" +
		"  ->  Seq Scan on orders  (cost=0.00..900.00 rows=100 width=0) " +
		"(actual time=0.010..23.000 rows=120 loops=1)\n" +
		"        Filter: (status = 'new'::text)\n"
}

func planRecordsOf(bodies ...string) []source.Record {
	recs := make([]source.Record, 0, len(bodies))

	for i, body := range bodies {
		r := record(i, body)
		r.Fields["error_severity"] = "LOG"
		r.Fields["query_id"] = "42"
		recs = append(recs, r)
	}

	return recs
}

// windowProvider replays a different record list per window, so a comparison
// reads two distinct windows off one source.
type windowProvider struct {
	fakeProvider
	byWindow map[time.Time][]source.Record
}

func (p *windowProvider) Stream(ctx context.Context, sp source.StreamParams, fn func(source.Record) bool) error {
	p.records = p.byWindow[sp.From]

	return p.fakeProvider.Stream(ctx, sp, fn)
}

func compareProvider(t *testing.T, current, baseline []source.Record) *windowProvider {
	t.Helper()

	return &windowProvider{
		fakeProvider: fakeProvider{fields: testFieldMap(t)}, //nolint:exhaustruct
		byWindow: map[time.Time][]source.Record{
			testWindow.from: current,
			baseWindow.from: baseline,
		},
	}
}

func compareQuery() CompareQuery {
	return CompareQuery{ //nolint:exhaustruct
		PlansQuery: PlansQuery{ //nolint:exhaustruct
			Cluster: "prod",
			Stream:  testStream,
			From:    testWindow.from,
			To:      testWindow.to,
		},
		BaseFrom: baseWindow.from,
		BaseTo:   baseWindow.to,
	}
}

func newCompareService(
	t *testing.T,
	p *windowProvider,
	snapshots SnapshotStore,
	cfg config.LogInsightsConfig,
) Service {
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

	return NewService(clusters, reg, config.LogSearchConfig{}, cfg, snapshots, nil, zap.NewNop())
}

func TestCompareReadsBothWindowsAndNamesTheRegression(t *testing.T) {
	t.Parallel()

	p := compareProvider(t,
		planRecordsOf(seqScanPlan("250.000"), seqScanPlan("300.000")),
		planRecordsOf(indexedPlan("12.000"), indexedPlan("14.000")))
	svc := newCompareService(t, p, nil, config.LogInsightsConfig{})

	res, err := svc.Compare(context.Background(), compareQuery())
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	if p.calls != 2 {
		t.Errorf("source read %d times, want both windows", p.calls)
	}

	if res.Baseline == nil {
		t.Fatal("baseline = none, want the window that was read")
	}

	if !res.Baseline.From.Equal(baseWindow.from) || !res.Current.From.Equal(testWindow.from) {
		t.Errorf("windows = %v / %v, want the two asked for", res.Current.From, res.Baseline.From)
	}

	if res.Partial {
		t.Errorf("partial = true, reasons %v; want a whole comparison",
			append(res.Current.Result.PartialReasons, res.Baseline.Result.PartialReasons...))
	}

	if len(res.Regressions) != 1 {
		t.Fatalf("regressions = %+v, want the one statement", res.Regressions)
	}

	r := res.Regressions[0]
	if len(r.LostIndexes) != 1 || r.LostIndexes[0] != "orders_status_idx" {
		t.Errorf("lost indexes = %v, want the index the baseline read", r.LostIndexes)
	}

	if len(r.AddedHashes) != 1 || len(r.RemovedHashes) != 1 {
		t.Errorf("shapes = +%v -%v, want one of each", r.AddedHashes, r.RemovedHashes)
	}
}

func TestCompareLeavesTheBaselineUnreadWithNothingToCompare(t *testing.T) {
	t.Parallel()

	p := compareProvider(t,
		[]source.Record{record(0, "checkpoint starting: time")},
		planRecordsOf(indexedPlan("12.000")))
	svc := newCompareService(t, p, nil, config.LogInsightsConfig{})

	res, err := svc.Compare(context.Background(), compareQuery())
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	if p.calls != 1 {
		t.Errorf("source read %d times, want the current window alone", p.calls)
	}

	if res.Baseline != nil || len(res.Regressions) != 0 {
		t.Errorf("baseline = %+v, regressions = %+v; want neither", res.Baseline, res.Regressions)
	}

	if res.Current.Result.EmptyReason != EmptyNoPlanRecords {
		t.Errorf("empty reason = %q, want %q", res.Current.Result.EmptyReason, EmptyNoPlanRecords)
	}
}

func TestCompareIsPartialWhenEitherWindowWasCut(t *testing.T) {
	t.Parallel()

	p := compareProvider(t,
		planRecordsOf(seqScanPlan("250.000"), seqScanPlan("300.000")),
		planRecordsOf(indexedPlan("12.000"), indexedPlan("14.000")))

	svc := newCompareService(t, p, nil, config.LogInsightsConfig{MaxRecords: 1})

	res, err := svc.Compare(context.Background(), compareQuery())
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	if !res.Partial {
		t.Error("partial = false, want a comparison of truncated windows marked as partial")
	}
}

func TestCompareStoresBothWindowsAsSnapshots(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{} //nolint:exhaustruct
	p := compareProvider(t,
		planRecordsOf(seqScanPlan("250.000")),
		planRecordsOf(indexedPlan("12.000")))
	svc := newCompareService(t, p, snaps, config.LogInsightsConfig{})

	res, err := svc.Compare(context.Background(), compareQuery())
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	awaitSnapshotWrite(t, svc, res.Current.Result.ScanID)
	awaitSnapshotWrite(t, svc, res.Baseline.Result.ScanID)

	if len(snaps.saved) != 2 {
		t.Fatalf("stored %d scans, want both windows", len(snaps.saved))
	}

	for _, scan := range snaps.saved {
		if scan.Kind != ScanCompare {
			t.Errorf("scan kind = %q, want %q", scan.Kind, ScanCompare)
		}
	}
}

// storedScan answers with one stored scan and its groups, and records nothing.
type storedScan struct {
	SnapshotStore
	scan Scan
	rows []insights.GroupRow
}

func (s *storedScan) SaveInsightsScan(context.Context, Scan, []insights.PlanGroup) error { return nil }

func (s *storedScan) GetInsightsScan(_ context.Context, id uuid.UUID, _ int) (Scan, error) {
	if id != s.scan.ID {
		return Scan{}, ErrNotFound
	}

	return s.scan, nil
}

func (s *storedScan) AllInsightsGroups(context.Context, uuid.UUID) ([]insights.GroupRow, error) {
	return s.rows, nil
}

func TestCompareTakesTheCurrentWindowFromAStoredScan(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := compareProvider(t, nil, planRecordsOf(indexedPlan("12.000"), indexedPlan("14.000")))
	svc := newCompareService(t, p, storedScanOf(id, ""), config.LogInsightsConfig{})

	q := compareQuery()
	q.From, q.To = time.Time{}, time.Time{}
	q.ScanID = id

	res, err := svc.Compare(context.Background(), q)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	if p.calls != 1 {
		t.Errorf("source read %d times, want the baseline alone", p.calls)
	}

	if res.Current.Result.ScanID != id || !res.Current.To.Equal(testWindow.to) {
		t.Errorf("current = %+v, want the window of the stored scan", res.Current)
	}

	if len(res.Regressions) != 1 || res.Regressions[0].QueryID != 42 {
		t.Fatalf("regressions = %+v, want the stored statement", res.Regressions)
	}
}

func TestCompareRejectsAWindowBesideAStoredScan(t *testing.T) {
	t.Parallel()

	p := compareProvider(t, nil, nil)
	svc := newCompareService(t, p, &storedScan{}, config.LogInsightsConfig{}) //nolint:exhaustruct

	q := compareQuery()
	q.ScanID = uuid.New()

	if _, err := svc.Compare(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}
}

func TestCompareNeedsStorageForAStoredScan(t *testing.T) {
	t.Parallel()

	p := compareProvider(t, nil, nil)
	svc := newCompareService(t, p, nil, config.LogInsightsConfig{})

	q := compareQuery()
	q.From, q.To = time.Time{}, time.Time{}
	q.ScanID = uuid.New()

	if _, err := svc.Compare(context.Background(), q); !errors.Is(err, ErrNoStorage) {
		t.Errorf("error = %v, want ErrNoStorage", err)
	}
}

func TestCompareRejectsAnInvalidBaselineWindow(t *testing.T) {
	t.Parallel()

	p := compareProvider(t, nil, nil)
	svc := newCompareService(t, p, nil, config.LogInsightsConfig{})

	q := compareQuery()
	q.BaseFrom, q.BaseTo = baseWindow.to, baseWindow.from

	if _, err := svc.Compare(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}

	if p.calls != 0 {
		t.Errorf("source read %d times, want none", p.calls)
	}
}

func TestCompareDisabledReadsNothing(t *testing.T) {
	t.Parallel()

	off := false
	p := compareProvider(t, nil, nil)
	svc := newCompareService(t, p, nil, config.LogInsightsConfig{Enabled: &off})

	if _, err := svc.Compare(context.Background(), compareQuery()); !errors.Is(err, ErrDisabled) {
		t.Errorf("error = %v, want ErrDisabled", err)
	}

	if p.calls != 0 {
		t.Errorf("source read %d times, want none", p.calls)
	}
}

func storedScanOf(id uuid.UUID, host string) *storedScan {
	return &storedScan{ //nolint:exhaustruct
		scan: Scan{ //nolint:exhaustruct
			ID:      id,
			Kind:    ScanPlans,
			Cluster: "prod",
			Stream:  testStream,
			Host:    host,
			From:    testWindow.from,
			To:      testWindow.to,
			Result:  ScanResult{ScanID: id, Scanned: 12}, //nolint:exhaustruct
		},
		rows: []insights.GroupRow{{ //nolint:exhaustruct
			QueryID:    42,
			HasQueryID: true,
			Hash:       "seqscan",
			Count:      2,
			Durations:  insights.DurationStats{Min: 250, P50: 250, P95: 300, Max: 300, Sum: 550},
			QueryText:  "SELECT count(*) FROM orders WHERE status = $1",
		}},
	}
}

func TestCompareReadsTheBaselineForTheHostTheStoredScanCovers(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := compareProvider(t, nil, planRecordsOf(indexedPlan("12.000"), indexedPlan("14.000")))
	svc := newCompareService(t, p, storedScanOf(id, "db-1"), config.LogInsightsConfig{})

	q := compareQuery()
	q.From, q.To = time.Time{}, time.Time{}
	q.ScanID = id

	if _, err := svc.Compare(context.Background(), q); err != nil {
		t.Fatalf("compare: %v", err)
	}

	if p.filter.Host != "db-1" {
		t.Errorf("baseline host = %q, want the host of the stored scan", p.filter.Host)
	}
}

func TestCompareRejectsAStoredScanOfAnotherHost(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := compareProvider(t, nil, nil)
	svc := newCompareService(t, p, storedScanOf(id, "db-1"), config.LogInsightsConfig{})

	q := compareQuery()
	q.From, q.To = time.Time{}, time.Time{}
	q.ScanID, q.Host = id, "db-2"

	if _, err := svc.Compare(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}

	if p.calls != 0 {
		t.Errorf("source read %d times, want none", p.calls)
	}
}

// A scan stored before the groups carried the indexes of their plan holds none,
// and every index of the baseline would read as one the window lost.
func TestCompareCallsNoIndexLostWhenTheStoredScanRecordedNone(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := compareProvider(t, nil, planRecordsOf(indexedPlan("240.000"), indexedPlan("280.000")))
	svc := newCompareService(t, p, storedScanOf(id, ""), config.LogInsightsConfig{})

	q := compareQuery()
	q.From, q.To = time.Time{}, time.Time{}
	q.ScanID = id

	res, err := svc.Compare(context.Background(), q)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	for _, r := range res.Regressions {
		if len(r.LostIndexes) != 0 {
			t.Errorf("lost indexes = %v, want none: the stored scan recorded no index", r.LostIndexes)
		}
	}
}

// A stored group whose plan read no index holds an empty list, not an absent
// one, and the index the baseline read is one the window lost.
func TestCompareCallsAnIndexLostWhenTheStoredScanRecordedAnEmptyList(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	p := compareProvider(t, nil, planRecordsOf(indexedPlan("12.000"), indexedPlan("14.000")))
	stored := storedScanOf(id, "")
	stored.rows[0].Indexes = []string{}
	svc := newCompareService(t, p, stored, config.LogInsightsConfig{})

	q := compareQuery()
	q.From, q.To = time.Time{}, time.Time{}
	q.ScanID = id

	res, err := svc.Compare(context.Background(), q)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	if len(res.Regressions) != 1 {
		t.Fatalf("regressions = %+v, want the stored statement", res.Regressions)
	}

	if lost := res.Regressions[0].LostIndexes; len(lost) != 1 || lost[0] != "orders_status_idx" {
		t.Errorf("lost indexes = %v, want the index the baseline read", lost)
	}
}
