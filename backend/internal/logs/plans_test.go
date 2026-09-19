package logs

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// planRecords mixes two statements' plans with records that are not plans.
func planRecords() []source.Record {
	recs := []source.Record{
		record(0, insightsPlan),
		record(1, "checkpoint starting: time"),
		record(2, insightsPlan),
		record(3, "duration: 1500.000 ms  statement: SELECT pg_sleep(1.5)"),
	}

	for i, id := range []string{"111", "0", "222", "0"} {
		recs[i].Fields["error_severity"] = "LOG"
		recs[i].Fields["query_id"] = id
	}

	return recs
}

func plansQuery() PlansQuery {
	return PlansQuery{
		Cluster: "prod",
		Stream:  testStream,
		From:    testWindow.from,
		To:      testWindow.to,
	}
}

// stubSettings answers for every host with values, except the hosts listed in
// perHost: their own values, or an error when the entry is nil.
type stubSettings struct {
	values  map[string]string
	perHost map[string]map[string]string
	err     error
}

func (s *stubSettings) GetPlanLogSettings(_ context.Context, _, instance string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}

	if v, ok := s.perHost[instance]; ok {
		if v == nil {
			return nil, errors.New("no route to host")
		}

		return v, nil
	}

	return s.values, nil
}

func planSettings(level string) *stubSettings {
	return &stubSettings{
		values:  map[string]string{settingLogLevel: level, settingLogFormat: "text"},
		perHost: nil,
		err:     nil,
	}
}

func TestPlansGroupsPlanRecordsAndCountsNoCategories(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	res, err := svc.Plans(context.Background(), plansQuery())
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if res.Categories != nil {
		t.Errorf("categories = %v, want none on a plans scan", res.Categories)
	}

	if res.Scanned != 4 || res.Plans.Records != 2 || res.Plans.TotalGroups != 2 {
		t.Fatalf("scanned = %d, plans = %+v; want 2 plan records in 2 groups", res.Scanned, res.Plans)
	}

	for _, g := range res.Plans.Groups {
		if !g.HasQueryID || (g.QueryID != 111 && g.QueryID != 222) {
			t.Errorf("group query id = %d/%v, want one of the two statements", g.QueryID, g.HasQueryID)
		}
	}
}

func TestPlansAsksTheStoreToNarrowAndSaysSo(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsServiceWithSettings(t, p, config.LogInsightsConfig{}, nil, planSettings("log"))

	q := plansQuery()
	q.QueryID, q.HasQueryID = 111, true

	res, err := svc.Plans(context.Background(), q)
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if !slices.Equal(p.filter.Severities, []string{"LOG"}) ||
		p.filter.QueryID == nil || *p.filter.QueryID != 111 ||
		!slices.Equal(p.filter.Contains, []string{planPhrase}) {
		t.Errorf("filter = %+v, want severity LOG, query_id 111 and the plan phrase", p.filter)
	}

	want := []string{"severity=LOG", NarrowedQueryID, NarrowedText}
	if !slices.Equal(res.NarrowedBy, want) {
		t.Errorf("narrowed_by = %v, want %v", res.NarrowedBy, want)
	}

	if res.Plans.TotalGroups != 1 || res.Plans.Groups[0].QueryID != 111 {
		t.Errorf("plans = %+v, want the one statement asked for", res.Plans)
	}
}

// A store that cannot run part of the filter must not make the answer claim it
// did, and the records it hands back are filtered here regardless.
func TestPlansReportsOnlyTheNarrowingTheStoreAccepted(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields:  testFieldMap(t),
		records: planRecords(),
		narrow: func(f source.Filter) source.Filter {
			return source.Filter{Severities: f.Severities, Host: f.Host}
		},
	}
	svc := newInsightsServiceWithSettings(t, p, config.LogInsightsConfig{}, nil, planSettings("log"))

	q := plansQuery()
	q.QueryID, q.HasQueryID = 222, true

	res, err := svc.Plans(context.Background(), q)
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if !slices.Equal(res.NarrowedBy, []string{"severity=LOG"}) {
		t.Errorf("narrowed_by = %v, want severity alone", res.NarrowedBy)
	}

	if res.Plans.TotalGroups != 1 || res.Plans.Groups[0].QueryID != 222 {
		t.Errorf("plans = %+v, want the one statement asked for", res.Plans)
	}
}

func TestPlansRejectsAStatementTheStreamCannotIdentify(t *testing.T) {
	t.Parallel()

	fm := testFieldMap(t)
	fm.QueryID = ""

	p := &fakeProvider{fields: fm, records: planRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	q := plansQuery()
	q.QueryID, q.HasQueryID = 111, true

	if _, err := svc.Plans(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}

	if p.calls != 0 {
		t.Errorf("source read %d times, want none", p.calls)
	}
}

func TestPlansNarrowsByTheLevelAutoExplainLogsWith(t *testing.T) {
	t.Parallel()

	settings := &stubSettings{err: nil, perHost: nil, values: map[string]string{
		settingLogMinDuration: "200",
		settingLogAnalyze:     "on",
		settingLogFormat:      "text",
		settingLogLevel:       "notice",
		settingComputeQueryID: "on",
	}}

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsServiceWithSettings(t, p, config.LogInsightsConfig{}, nil, settings)

	res, err := svc.Plans(context.Background(), plansQuery())
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if !slices.Equal(p.filter.Severities, []string{"NOTICE"}) {
		t.Errorf("severities = %v, want the configured level in the source's spelling", p.filter.Severities)
	}

	c := res.Configuration
	if c == nil || c.Instance != "db-1" || !c.AutoExplain || c.LogMinDurationMs == nil || *c.LogMinDurationMs != 200 ||
		c.LogAnalyze == nil || !*c.LogAnalyze || c.LogFormat != "text" || c.ComputeQueryID != "on" {
		t.Errorf("configuration = %+v, want the settings read back", c)
	}
}

// A guessed level is worse than a wide scan: the store would drop every plan
// record of a cluster that logs at another one, and the answer would read as an
// empty window.
func TestPlansRunsOnAClusterThatDoesNotAnswer(t *testing.T) {
	t.Parallel()

	settings := &stubSettings{values: nil, perHost: nil, err: errors.New("no route to host")}

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsServiceWithSettings(t, p, config.LogInsightsConfig{}, nil, settings)

	res, err := svc.Plans(context.Background(), plansQuery())
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if res.Configuration != nil {
		t.Errorf("configuration = %+v, want none", res.Configuration)
	}

	if len(p.filter.Severities) != 0 {
		t.Errorf("severities = %v, want none: no level was read", p.filter.Severities)
	}

	if slices.Contains(res.NarrowedBy, "severity=LOG") {
		t.Errorf("narrowed_by = %v, want no severity", res.NarrowedBy)
	}

	if res.Plans.TotalGroups != 2 {
		t.Errorf("plans = %+v, want the scan to have run anyway", res.Plans)
	}
}

// Hosts of one cluster may log plans at different levels; narrowing by one of
// them would hide the records of the others.
func TestPlansNarrowsByEveryLevelTheHostsLogWith(t *testing.T) {
	t.Parallel()

	settings := &stubSettings{
		values: nil,
		perHost: map[string]map[string]string{
			"db-1": {settingLogLevel: "log"},
			"db-2": {settingLogLevel: "notice"},
		},
		err: nil,
	}

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsServiceWithSettings(t, p, config.LogInsightsConfig{}, nil, settings)

	res, err := svc.Plans(context.Background(), plansQuery())
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if !slices.Equal(p.filter.Severities, []string{"LOG", "NOTICE"}) {
		t.Errorf("severities = %v, want the level of both hosts", p.filter.Severities)
	}

	if res.Configuration == nil || res.Configuration.Instance != "db-1" {
		t.Errorf("configuration = %+v, want the host it was read from", res.Configuration)
	}
}

func TestPlansDoesNotNarrowWhenAHostIsSilent(t *testing.T) {
	t.Parallel()

	settings := &stubSettings{
		values:  map[string]string{settingLogLevel: "log"},
		perHost: map[string]map[string]string{"db-2": nil},
		err:     nil,
	}

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsServiceWithSettings(t, p, config.LogInsightsConfig{}, nil, settings)

	res, err := svc.Plans(context.Background(), plansQuery())
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if len(p.filter.Severities) != 0 {
		t.Errorf("severities = %v, want none: db-2 did not answer", p.filter.Severities)
	}

	if res.Configuration == nil || res.Configuration.Instance != "db-1" {
		t.Errorf("configuration = %+v, want the diagnosis of the host that answered", res.Configuration)
	}
}

func TestPlansCapsTheGroupsItAnswersWith(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	q := plansQuery()
	q.Limit = 1

	res, err := svc.Plans(context.Background(), q)
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	if res.Plans.TotalGroups != 2 {
		t.Errorf("total groups = %d, want both counted", res.Plans.TotalGroups)
	}

	if len(res.Plans.Groups) != 1 {
		t.Errorf("groups = %d, want the one the limit allows", len(res.Plans.Groups))
	}
}

func TestPlansStoresItsScanUnderItsOwnKind(t *testing.T) {
	t.Parallel()

	snaps := &fakeSnapshots{err: nil}
	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsServiceWithSnapshots(t, p, config.LogInsightsConfig{}, snaps)

	res, err := svc.Plans(context.Background(), plansQuery())
	if err != nil {
		t.Fatalf("plans: %v", err)
	}

	awaitSnapshotWrite(t, svc, res.ScanID)

	if len(snaps.saved) != 1 {
		t.Fatalf("stored %d scans, want one", len(snaps.saved))
	}

	scan := snaps.saved[0]
	if scan.Kind != ScanPlans || scan.ID != res.ScanID {
		t.Errorf("scan = %+v, want the plans scan the answer names", scan)
	}

	if len(snaps.groups[0]) != 2 {
		t.Errorf("stored %d groups, want both", len(snaps.groups[0]))
	}
}
