package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/insights"
	"github.com/dbulashev/dasha/internal/logs/source"
)

const insightsPlan = "duration: 25.000 ms  plan:\n" +
	"Query Text: SELECT count(*) FROM orders WHERE status = 'new'\n" +
	"Aggregate  (cost=1000.00..1000.01 rows=1 width=8) (actual time=24.000..24.001 rows=1 loops=1)\n" +
	"  ->  Seq Scan on orders  (cost=0.00..900.00 rows=100 width=0) (actual time=0.010..23.000 rows=120 loops=1)\n" +
	"        Filter: (status = 'new'::text)\n" +
	"        Rows Removed by Filter: 99880\n"

func insightsRecords() []source.Record {
	recs := []source.Record{
		record(0, insightsPlan),
		record(1, "checkpoint starting: time"),
		record(2, insightsPlan),
		record(3, "duration: 1500.000 ms  statement: SELECT pg_sleep(1.5)"),
		record(4, "database system is ready to accept connections"),
	}

	for i := range recs {
		recs[i].Fields["error_severity"] = "LOG"
		recs[i].Fields["query_id"] = "-4452854032459450605"
	}

	return recs
}

func newInsightsService(t *testing.T, p *fakeProvider, cfg config.LogInsightsConfig) Service {
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

	return NewService(clusters, reg, config.LogSearchConfig{}, cfg, zap.NewNop())
}

func insightsQuery() InsightsQuery {
	return InsightsQuery{Cluster: "prod", Stream: testStream, From: testWindow.from, To: testWindow.to}
}

func category(res InsightsResult, code string) (insights.CategorySummary, bool) {
	i := slices.IndexFunc(res.Categories, func(c insights.CategorySummary) bool { return c.Code == code })
	if i < 0 {
		return insights.CategorySummary{}, false
	}

	return res.Categories[i], true
}

func TestInsightsClassifiesAndGroupsInOneRead(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: insightsRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	if p.calls != 1 {
		t.Errorf("source read %d times, want once", p.calls)
	}

	if res.Scanned != 5 || res.Partial || res.EmptyReason != "" {
		t.Errorf("scanned = %d, partial = %v, empty = %q", res.Scanned, res.Partial, res.EmptyReason)
	}

	for code, want := range map[string]int{
		insights.CategoryPlan:       2,
		insights.CategoryCheckpoint: 1,
		insights.CategorySlowQuery:  1,
		insights.CategoryOther:      1,
	} {
		if c, ok := category(res, code); !ok || c.Count != want {
			t.Errorf("category %s = %+v, want count %d", code, c, want)
		}
	}

	if res.Plans.Records != 2 || res.Plans.TotalGroups != 1 {
		t.Fatalf("plans = %+v, want 2 records in one group", res.Plans)
	}

	g := res.Plans.Groups[0]
	if !g.HasQueryID || g.QueryID != -4452854032459450605 || g.Count != 2 {
		t.Errorf("group query id = %d/%v, count = %d", g.QueryID, g.HasQueryID, g.Count)
	}

	if len(g.Findings) == 0 {
		t.Error("no findings on a Seq Scan discarding 99.9% of its rows")
	}
}

func TestInsightsDisabledReadsNothing(t *testing.T) {
	t.Parallel()

	off := false
	p := &fakeProvider{fields: testFieldMap(t), records: insightsRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{Enabled: &off})

	if _, err := svc.Insights(context.Background(), insightsQuery()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}

	if p.calls != 0 {
		t.Errorf("source read %d times, want none", p.calls)
	}
}

func TestInsightsSaysWhyItStopped(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: insightsRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{MaxRecords: 2})

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	if !res.Partial || !slices.Equal(res.PartialReasons, []string{PartialRecords}) {
		t.Errorf("partial = %v, reasons = %v; want records", res.Partial, res.PartialReasons)
	}

	recs := insightsRecords()
	twoRecords := recordBytes(recs[0]) + recordBytes(recs[1])

	for _, tt := range []struct {
		cfg  config.LogInsightsConfig
		want []string
	}{
		{cfg: config.LogInsightsConfig{MaxBytes: twoRecords}, want: []string{PartialBytes}},
		{cfg: config.LogInsightsConfig{MaxRecords: 2, MaxBytes: twoRecords}, want: []string{PartialRecords, PartialBytes}},
	} {
		p = &fakeProvider{fields: testFieldMap(t), records: insightsRecords()}

		res, err = newInsightsService(t, p, tt.cfg).Insights(context.Background(), insightsQuery())
		if err != nil {
			t.Fatalf("insights: %v", err)
		}

		if res.Scanned != 2 || !slices.Equal(res.PartialReasons, tt.want) {
			t.Errorf("%+v: scanned = %d, reasons = %v; want 2, %v", tt.cfg, res.Scanned, res.PartialReasons, tt.want)
		}
	}

	p = &fakeProvider{
		fields:  testFieldMap(t),
		records: insightsRecords(),
		err:     fmt.Errorf("%w: boundary overflow", source.ErrPartial),
	}
	svc = newInsightsService(t, p, config.LogInsightsConfig{})

	res, err = svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights on a partial source: %v", err)
	}

	if !slices.Equal(res.PartialReasons, []string{PartialSource}) || res.Scanned != 5 {
		t.Errorf("reasons = %v, scanned = %d; want source and the records delivered", res.PartialReasons, res.Scanned)
	}
}

func TestInsightsReportsTheSpanItCovered(t *testing.T) {
	t.Parallel()

	recs := insightsRecords()
	ts := func(i int) time.Time { return recs[i].Timestamp }

	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newInsightsService(t, p, config.LogInsightsConfig{MaxPlans: 1})

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	if !res.Covered.From.Equal(ts(0)) || !res.Covered.To.Equal(ts(4)) {
		t.Errorf("covered = %v, want %v..%v", res.Covered, ts(0), ts(4))
	}

	// The second plan, record 2, is the first one turned away.
	if !res.PlansCovered.From.Equal(ts(0)) || !res.PlansCovered.To.Equal(ts(1)) {
		t.Errorf("plans covered = %v, want %v..%v", res.PlansCovered, ts(0), ts(1))
	}

	p = &fakeProvider{fields: testFieldMap(t), records: nil}
	svc = newInsightsService(t, p, config.LogInsightsConfig{})

	res, err = svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights on an empty window: %v", err)
	}

	if res.Covered != (Span{}) || res.PlansCovered != (Span{}) {
		t.Errorf("covered = %v / %v on an empty window, want zero spans", res.Covered, res.PlansCovered)
	}
}

func TestInsightsEmptyReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records []source.Record
		want    string
	}{
		{name: "empty window", records: nil, want: EmptyNoRecords},
		{name: "no plans", records: records(3), want: EmptyNoPlanRecords},
		{
			name:    "yaml only",
			records: []source.Record{record(0, "duration: 1.000 ms  plan:\nQuery Text: \"SELECT 1\"\nPlan:\n  Node Type: \"Result\"\n")},
			want:    EmptyUnsupportedFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &fakeProvider{fields: testFieldMap(t), records: tt.records}
			svc := newInsightsService(t, p, config.LogInsightsConfig{})

			res, err := svc.Insights(context.Background(), insightsQuery())
			if err != nil {
				t.Fatalf("insights: %v", err)
			}

			if res.EmptyReason != tt.want {
				t.Errorf("empty reason = %q, want %q", res.EmptyReason, tt.want)
			}
		})
	}
}

func TestInsightsStaysEncodableOnHugeDurations(t *testing.T) {
	t.Parallel()

	recs := make([]source.Record, 0, 4)

	for i, d := range []string{"3000000000000.000", "1e308", "3000000000000.000", "1e308"} {
		r := record(i, strings.Replace(insightsPlan, "25.000", d, 1))
		r.Fields["error_severity"] = "LOG"
		r.Fields["query_id"] = "-4452854032459450605"
		recs = append(recs, r)
	}

	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	res, err := svc.Insights(context.Background(), insightsQuery())
	if err != nil {
		t.Fatalf("insights: %v", err)
	}

	if res.Plans.Records != 2 || res.Plans.TotalGroups != 1 || res.Plans.TotalDurationMs != 6e12 {
		t.Errorf("plans = %d in %d groups, total %v ms; want 2 in 1, 6e12",
			res.Plans.Records, res.Plans.TotalGroups, res.Plans.TotalDurationMs)
	}

	if _, err := json.Marshal(res); err != nil {
		t.Errorf("marshal: %v", err)
	}
}

func TestInsightsRejectsAForeignHost(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: insightsRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{})

	q := insightsQuery()
	q.Host = "db-9"

	if _, err := svc.Insights(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}
}
