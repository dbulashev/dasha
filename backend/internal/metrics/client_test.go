package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector",` +
			`"result":[{"metric":{"service_id":"svc"},"value":[1700000000,"42.5"]}]}}`))
	})

	mux.HandleFunc("/api/v1/query_range", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix",` +
			`"result":[{"metric":{"service_id":"svc"},"values":[[1700000000,"1"],[1700000060,"2.5"]]}]}}`))
	})

	return httptest.NewServer(mux)
}

func TestVMClient_QueryInstant(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	c := NewVMClient(DatasourceConfig{URL: srv.URL}, nil)

	samples, err := c.QueryInstant(context.Background(), "postgres_up{}", time.Time{})
	if err != nil {
		t.Fatalf("QueryInstant: %v", err)
	}

	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}

	if samples[0].Value != 42.5 {
		t.Errorf("value: want 42.5, got %v", samples[0].Value)
	}

	if samples[0].Labels["service_id"] != "svc" {
		t.Errorf("label service_id missing: %+v", samples[0].Labels)
	}
}

func TestVMClient_QueryRange(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	c := NewVMClient(DatasourceConfig{URL: srv.URL}, nil)

	series, err := c.QueryRange(context.Background(), "rate(x[5m])", Range{
		Start: time.Unix(1700000000, 0), End: time.Unix(1700000060, 0), Step: time.Minute,
	})
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}

	if len(series) != 1 || len(series[0].Points) != 2 {
		t.Fatalf("expected 1 series of 2 points, got %d series", len(series))
	}

	if series[0].Points[1].Value != 2.5 {
		t.Errorf("second point: want 2.5, got %v", series[0].Points[1].Value)
	}
}

func TestVMClient_QueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"boom"}`))
	}))
	defer srv.Close()

	c := NewVMClient(DatasourceConfig{URL: srv.URL}, nil)

	if _, err := c.QueryInstant(context.Background(), "x", time.Time{}); err == nil {
		t.Fatal("expected error on status=error response")
	}
}
