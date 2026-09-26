package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fleetResponse = `{
  "items": [
    {"cluster_name": "c1", "instance": "h1", "score": 0, "source": "metrics", "in_recovery": false},
    {"cluster_name": "c2", "instance": "h2", "score": null, "source": "none", "in_recovery": false, "error": "budget exceeded"}
  ],
  "instances_total": 40, "instances_scored": 39, "candidates": 20, "uncomputed": 1,
  "incomplete": true, "metrics_unavailable": false,
  "computed_at": "2026-09-26T10:00:00Z", "duration_ms": 9800
}`

func TestFleetHealth_OneCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	var gotLimit string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.URL.Path != "/api/common/health-score/fleet" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		gotLimit = r.URL.Query().Get("limit")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fleetResponse))
	}))
	defer backend.Close()

	res, err := connect(t, backend.URL).CallTool(context.Background(), &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "fleet_health",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if res.IsError {
		t.Fatalf("IsError: %s", firstText(res))
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("%d HTTP calls, want 1", n)
	}

	if gotLimit != "5" {
		t.Errorf("limit %q, want the default 5", gotLimit)
	}

	var got fleetResult
	if err := json.Unmarshal([]byte(firstText(res)), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, firstText(res))
	}

	if got.Limit != 5 || len(got.Worst) != 2 {
		t.Fatalf("limit %d rows %d", got.Limit, len(got.Worst))
	}

	if w := got.Worst[0]; w.Score == nil || *w.Score != 0 || w.Source != "metrics" {
		t.Errorf("zero score lost: %+v", w)
	}

	if w := got.Worst[1]; w.Score != nil || w.Error != "budget exceeded" {
		t.Errorf("uncomputed row: %+v", w)
	}

	if f := got.Fleet; f.Total != 40 || f.Scored != 39 || f.Uncomputed != 1 || !f.Incomplete || f.MetricsUnavailable {
		t.Errorf("fleet meta %+v", f)
	}
}
