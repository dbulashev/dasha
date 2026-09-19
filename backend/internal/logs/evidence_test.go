package logs

import (
	"context"
	"errors"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
)

func evidenceConfig() config.LogInsightsConfig {
	return config.LogInsightsConfig{IndexAdvisorEvidence: true} //nolint:exhaustruct
}

func plansForIDs(t *testing.T, svc Service, ids ...int64) ([]int64, bool) {
	t.Helper()

	w, err := svc.PlansForQueryIDs(
		context.Background(), "prod", testStream, testWindow.from, testWindow.to, ids)
	if err != nil {
		t.Fatalf("plans for query ids: %v", err)
	}

	got := make([]int64, 0, len(w.Groups))
	for _, g := range w.Groups {
		got = append(got, g.QueryID)
	}

	return got, w.Partial
}

func TestPlansForQueryIDsKeepsOnlyTheRequestedStatements(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, evidenceConfig())

	got, partial := plansForIDs(t, svc, 111)
	if len(got) != 1 || got[0] != 111 {
		t.Fatalf("query ids = %v, want [111]", got)
	}

	if partial {
		t.Error("window read whole, reported partial")
	}
}

func TestPlansForQueryIDsKeepsEveryRequestedStatement(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, evidenceConfig())

	got, _ := plansForIDs(t, svc, 111, 222, 333)
	if len(got) != 2 {
		t.Fatalf("query ids = %v, want the two that are in the window", got)
	}
}

// Off by default: the index advisor report has to build exactly as it does on a
// cluster with no log source.
func TestPlansForQueryIDsDisabledByDefault(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, config.LogInsightsConfig{}) //nolint:exhaustruct

	_, err := svc.PlansForQueryIDs(
		context.Background(), "prod", testStream, testWindow.from, testWindow.to, []int64{111})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestPlansForQueryIDsDisabledWithInsights(t *testing.T) {
	t.Parallel()

	off := false
	cfg := config.LogInsightsConfig{Enabled: &off, IndexAdvisorEvidence: true} //nolint:exhaustruct

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, cfg)

	_, err := svc.PlansForQueryIDs(
		context.Background(), "prod", testStream, testWindow.from, testWindow.to, []int64{111})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

// No identifiers, no scan: a report whose candidates carry none must not spend a
// read of a foreign log store on it.
func TestPlansForQueryIDsWithoutIDsDoesNotScan(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, evidenceConfig())

	w, err := svc.PlansForQueryIDs(
		context.Background(), "prod", testStream, testWindow.from, testWindow.to, nil)
	if err != nil {
		t.Fatalf("plans for query ids: %v", err)
	}

	if len(w.Groups) != 0 {
		t.Errorf("groups = %v, want none", w.Groups)
	}

	if p.calls != 0 {
		t.Errorf("provider streamed %d times, want none", p.calls)
	}
}

// Without the query_id role every record fails the match, and the scan would
// spend a whole window to answer nothing.
func TestPlansForQueryIDsWithoutQueryIDRole(t *testing.T) {
	t.Parallel()

	fm := testFieldMap(t)
	fm.QueryID = ""

	p := &fakeProvider{fields: fm, records: planRecords()}
	svc := newInsightsService(t, p, evidenceConfig())

	_, err := svc.PlansForQueryIDs(
		context.Background(), "prod", testStream, testWindow.from, testWindow.to, []int64{111})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}

	if p.calls != 0 {
		t.Errorf("provider streamed %d times, want none", p.calls)
	}
}

func TestPlansForQueryIDsUnknownCluster(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, evidenceConfig())

	_, err := svc.PlansForQueryIDs(
		context.Background(), "staging", testStream, testWindow.from, testWindow.to, []int64{111})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// The window held plans, only none of the statement asked for. That is a
// negative answer about the statement; a window auto_explain never wrote to is
// not, and PlanRecords is what tells the two apart.
func TestPlansForQueryIDsCountsEveryPlanRecord(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: planRecords()}
	svc := newInsightsService(t, p, evidenceConfig())

	w, err := svc.PlansForQueryIDs(
		context.Background(), "prod", testStream, testWindow.from, testWindow.to, []int64{999})
	if err != nil {
		t.Fatalf("plans for query ids: %v", err)
	}

	if len(w.Groups) != 0 {
		t.Errorf("groups = %v, want none", w.Groups)
	}

	if w.PlanRecords != 2 {
		t.Errorf("plan records = %d, want the two plans the window holds", w.PlanRecords)
	}
}
