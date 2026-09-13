package logs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

func scanParams() source.StreamParams {
	return source.StreamParams{
		Cluster: config.Cluster{Name: "prod", Hosts: []config.Host{"db-1", "db-2"}},
		Stream:  testStream,
		From:    testWindow.from,
		To:      testWindow.to,
		Filter:  source.Filter{Severities: nil, Host: ""},
		Token:   "",
	}
}

func newScanService(t *testing.T, p *fakeProvider) *service {
	t.Helper()

	svc, ok := newTestService(t, p, config.LogSearchConfig{}).(*service)
	if !ok {
		t.Fatal("service type")
	}

	return svc
}

func TestScanStopsAtTheRecordLimit(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(10)}
	svc := newScanService(t, p)

	visited := 0

	st, err := svc.scan(context.Background(), p, scanParams(),
		scanLimits{MaxRecords: 3, MaxBytes: 0},
		func(source.Record) bool {
			visited++

			return true
		})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if st.Records != 3 || visited != 3 || !st.Capped {
		t.Errorf("records = %d, visited = %d, capped = %v; want 3, 3, true", st.Records, visited, st.Capped)
	}

	if st.Bytes != 0 {
		t.Errorf("bytes = %d, want none charged without a byte budget", st.Bytes)
	}
}

func TestScanStopsAtTheByteBudget(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(10)}
	svc := newScanService(t, p)

	budget := recordBytes(p.records[0]) + recordBytes(p.records[1])

	st, err := svc.scan(context.Background(), p, scanParams(),
		scanLimits{MaxRecords: 0, MaxBytes: budget},
		func(source.Record) bool { return true })
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if st.Records != 2 || !st.Capped {
		t.Errorf("records = %d, capped = %v; want 2, true", st.Records, st.Capped)
	}

	if st.Bytes < budget {
		t.Errorf("bytes = %d, want at least the budget %d", st.Bytes, budget)
	}
}

func TestScanChargesAHugeRecordOnlyUpToTheRecordCap(t *testing.T) {
	t.Parallel()

	recs := records(5)
	recs[0].Fields["message"] = strings.Repeat("x", 1<<20)

	p := &fakeProvider{fields: testFieldMap(t), records: recs}
	svc := newScanService(t, p)

	st, err := svc.scan(context.Background(), p, scanParams(),
		scanLimits{MaxRecords: 0, MaxBytes: 1 << 16, MaxRecordBytes: 1 << 10},
		func(source.Record) bool { return true })
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if st.Records != 5 || st.Capped {
		t.Errorf("records = %d, capped = %v; want the whole window read", st.Records, st.Capped)
	}
}

func TestScanKeepsWhatAPartialSourceDelivered(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{
		fields:  testFieldMap(t),
		records: records(4),
		err:     fmt.Errorf("%w: too many records share one timestamp", source.ErrPartial),
	}
	svc := newScanService(t, p)

	st, err := svc.scan(context.Background(), p, scanParams(),
		scanLimits{MaxRecords: 100, MaxBytes: 0},
		func(source.Record) bool { return true })

	if err == nil {
		t.Fatal("scan: want the source error")
	}

	if st.Records != 4 || !st.Partial {
		t.Errorf("records = %d, partial = %v; want 4, true", st.Records, st.Partial)
	}

	if st.Capped {
		t.Error("capped = true, want false: no limit was reached")
	}
}

func TestScanTimeoutKeepsTheCountAndSaysSo(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(3), hang: true}
	svc := newScanService(t, p)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	st, err := svc.scan(ctx, p, scanParams(),
		scanLimits{MaxRecords: 100, MaxBytes: 0},
		func(source.Record) bool { return true })

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}

	if st.Records != 3 || st.Partial {
		t.Errorf("records = %d, partial = %v; want 3, false", st.Records, st.Partial)
	}
}

// TestScanDoesNotChargeARejectedRecord: the page lookahead rests on it — a
// record the caller refuses must stay readable through the resume cursor.
func TestScanDoesNotChargeARejectedRecord(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{fields: testFieldMap(t), records: records(10)}
	svc := newScanService(t, p)

	seen := 0

	st, err := svc.scan(context.Background(), p, scanParams(),
		scanLimits{MaxRecords: 100, MaxBytes: 1 << 20},
		func(source.Record) bool {
			seen++

			return seen < 3
		})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if st.Records != 2 || st.Capped {
		t.Errorf("records = %d, capped = %v; want 2, false", st.Records, st.Capped)
	}

	if st.Bytes != recordBytes(p.records[0])+recordBytes(p.records[1]) {
		t.Errorf("bytes = %d, want only the two consumed records", st.Bytes)
	}
}
