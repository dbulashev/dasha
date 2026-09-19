package logs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// fakeSnapshots records what the service handed it; err makes every write fail.
type fakeSnapshots struct {
	SnapshotStore
	mu     sync.Mutex
	calls  int
	saved  []Scan
	groups [][]insights.PlanGroup
	err    error
}

func (f *fakeSnapshots) SaveInsightsScan(_ context.Context, scan Scan, groups []insights.PlanGroup) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.err != nil {
		return f.err
	}

	f.saved = append(f.saved, scan)
	f.groups = append(f.groups, groups)

	return nil
}

// slowSnapshots delays the write so a read racing it has to wait for the rows.
type slowSnapshots struct {
	SnapshotStore
	delay time.Duration

	mu    sync.Mutex
	saved map[uuid.UUID]Scan
}

func (f *slowSnapshots) SaveInsightsScan(_ context.Context, scan Scan, _ []insights.PlanGroup) error {
	time.Sleep(f.delay)

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.saved == nil {
		f.saved = make(map[uuid.UUID]Scan)
	}

	f.saved[scan.ID] = scan

	return nil
}

func (f *slowSnapshots) GetInsightsScan(_ context.Context, id uuid.UUID, _ int) (Scan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	scan, ok := f.saved[id]
	if !ok {
		return Scan{}, ErrNotFound
	}

	return scan, nil
}

// awaitSnapshotWrite blocks until the background write of id is over.
func awaitSnapshotWrite(t *testing.T, svc Service, id uuid.UUID) {
	t.Helper()

	impl, ok := svc.(*service)
	if !ok {
		t.Fatalf("service = %T, want the package implementation", svc)
	}

	if err := impl.awaitSnapshot(context.Background(), id); err != nil {
		t.Fatalf("await snapshot: %v", err)
	}
}

// planOf builds an auto_explain record whose plan is unique per table and whose
// total time grows with the table number.
func planOf(i int) string {
	table := fmt.Sprintf("t%d", i)

	return fmt.Sprintf("duration: %d.000 ms  plan:\n", (i+1)*10) +
		"Query Text: SELECT count(*) FROM " + table + "\n" +
		"Aggregate  (cost=1000.00..1000.01 rows=1 width=8) (actual time=1.000..1.001 rows=1 loops=1)\n" +
		"  ->  Seq Scan on " + table + "  (cost=0.00..900.00 rows=100 width=0) " +
		"(actual time=0.010..0.900 rows=120 loops=1)\n"
}

func snapshotRecords(groups int) []source.Record {
	recs := make([]source.Record, 0, groups)

	for i := range groups {
		r := record(i, planOf(i))
		r.Fields["error_severity"] = "LOG"
		recs = append(recs, r)
	}

	return recs
}

func TestInsightsStoresEveryGroupAndAnswersWithTheTop(t *testing.T) {
	t.Parallel()

	const groups = insightsTopGroups + 5

	snaps := &fakeSnapshots{} //nolint:exhaustruct
	p := &fakeProvider{fields: testFieldMap(t), records: snapshotRecords(groups)}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{}, snaps)

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	awaitSnapshotWrite(t, svc, res.ScanID)

	if len(res.Plans.Groups) != insightsTopGroups {
		t.Errorf("answered with %d groups, want the top %d", len(res.Plans.Groups), insightsTopGroups)
	}

	if len(snaps.saved) != 1 {
		t.Fatalf("stored %d scans, want one", len(snaps.saved))
	}

	stored := snaps.groups[0]
	if len(stored) != groups {
		t.Errorf("stored %d groups, want all %d", len(stored), groups)
	}

	for i, g := range stored {
		if g.Ord != i {
			t.Fatalf("group %d has ord %d, want the rank by total time", i, g.Ord)
		}
	}

	scan := snaps.saved[0]
	if scan.ID != res.ScanID || scan.ID == uuid.Nil {
		t.Errorf("scan id = %s, answered %s", scan.ID, res.ScanID)
	}

	if scan.Kind != ScanInsights || scan.Cluster != "prod" || scan.From != testWindow.from {
		t.Errorf("scan = %+v, want the insights window of prod", scan)
	}

	if scan.Result.Plans.Groups != nil {
		t.Errorf("summary carries %d groups, want them in the group rows only", len(scan.Result.Plans.Groups))
	}

	if scan.Result.Plans.TotalGroups != groups {
		t.Errorf("summary counts %d groups, want %d", scan.Result.Plans.TotalGroups, groups)
	}
}

func TestInsightsSurvivesAFailedSnapshot(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{err: errors.New("storage down")} //nolint:exhaustruct
	p := &fakeProvider{fields: testFieldMap(t), records: snapshotRecords(3)}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{}, snaps)

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	awaitSnapshotWrite(t, svc, res.ScanID)

	if snaps.calls != 1 {
		t.Errorf("write attempts = %d, want one", snaps.calls)
	}

	if len(res.Plans.Groups) != 3 {
		t.Errorf("groups = %d, want the scan itself to stand", len(res.Plans.Groups))
	}
}

func TestSnapshotWaitsForTheWriteInFlight(t *testing.T) {
	t.Parallel()

	store := &slowSnapshots{delay: 50 * time.Millisecond} //nolint:exhaustruct
	p := &fakeProvider{fields: testFieldMap(t), records: snapshotRecords(2)}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{}, store)

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	if res.ScanID == uuid.Nil {
		t.Fatal("scan id = none, want one before the write lands")
	}

	scan, err := svc.Snapshot(context.Background(), res.ScanID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if scan.ID != res.ScanID {
		t.Errorf("snapshot id = %s, want %s", scan.ID, res.ScanID)
	}
}

func TestInsightsWithoutStorageAnswersWithoutAScanID(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: snapshotRecords(2)}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	if res.ScanID != uuid.Nil {
		t.Errorf("scan id = %s, want none without storage", res.ScanID)
	}

	if _, err := svc.Snapshot(context.Background(), uuid.New()); !errors.Is(err, ErrNoStorage) {
		t.Errorf("snapshot err = %v, want %v", err, ErrNoStorage)
	}
}

func TestSnapshotRefusedWhenInsightsAreDisabled(t *testing.T) {
	t.Parallel()

	off := false
	p := &fakeProvider{fields: testFieldMap(t), records: nil}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{Enabled: &off}, &fakeSnapshots{}) //nolint:exhaustruct

	if _, err := svc.Snapshot(context.Background(), uuid.New()); !errors.Is(err, ErrDisabled) {
		t.Errorf("snapshot err = %v, want %v", err, ErrDisabled)
	}
}

func TestSnapshotGroupsRejectAnUnknownOrder(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: nil}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{}, &fakeSnapshots{}) //nolint:exhaustruct

	q := GroupsQuery{ScanID: uuid.New(), Order: "duration"} //nolint:exhaustruct

	_, err := svc.SnapshotGroups(context.Background(), q)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want %v", err, ErrInvalid)
	}
}

func TestPageBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		limit, offset      int
		wantLimit, wantOff int
	}{
		{0, 0, defaultGroupPageSize, 0},
		{-1, -5, defaultGroupPageSize, 0},
		{10_000, 20, maxGroupPageSize, 20},
	}

	for _, c := range cases {
		limit, offset := pageBounds(c.limit, c.offset)
		if limit != c.wantLimit || offset != c.wantOff {
			t.Errorf("pageBounds(%d, %d) = %d, %d; want %d, %d",
				c.limit, c.offset, limit, offset, c.wantLimit, c.wantOff)
		}
	}
}

func TestSnapshotGroupsDefaultOrder(t *testing.T) {
	t.Parallel()

	store := &orderSnapshots{} //nolint:exhaustruct
	p := &fakeProvider{fields: testFieldMap(t), records: nil}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{}, store)

	if _, err := svc.SnapshotGroups(context.Background(), GroupsQuery{ScanID: uuid.New()}); err != nil { //nolint:exhaustruct
		t.Fatalf("snapshot groups: %v", err)
	}

	if store.got.Order != GroupOrderSum || store.got.Limit != defaultGroupPageSize {
		t.Errorf("query = %+v, want %s and the default page", store.got, GroupOrderSum)
	}
}

type orderSnapshots struct {
	SnapshotStore
	got GroupsQuery
}

func (f *orderSnapshots) ListInsightsGroups(_ context.Context, q GroupsQuery) (GroupPage, error) {
	f.got = q

	return GroupPage{}, nil //nolint:exhaustruct
}
