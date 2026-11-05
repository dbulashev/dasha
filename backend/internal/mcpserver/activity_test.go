package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func activityFakeAPI(t *testing.T, status int, body string, seen *url.Values) *DashaClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/activity/profile" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		if seen != nil {
			*seen = r.URL.Query()
		}

		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	return c
}

func activityProfileJSON(text string) string {
	return `{"from":"2026-11-04T12:00:00Z","to":"2026-11-04T12:00:14Z","samples":15,"failed_samples":0,` +
		`"truncated_samples":0,"can_read_all_stats":true,"query_id_available":true,"aas":2.5,` +
		`"by_class":[{"class":"CPU","aas":2.5,"share_pct":100}],"top_events":[],` +
		`"top_queries":[{"query_key":"q:1","query_key_kind":"query_id","query_id":"1","aas":2.5,` +
		`"share_pct":100,"by_class":{"CPU":100},"distinct_pids":3,"text":"` + text + `"}],` +
		`"by_database":[],"by_user":[],"by_application":[]}`
}

func TestSampleActivityClipsTexts(t *testing.T) {
	t.Parallel()

	var seen url.Values

	c := activityFakeAPI(t, http.StatusOK, activityProfileJSON(strings.Repeat("x", 1000)), &seen)

	got, err := sampleActivity(context.Background(), c, sampleActivityArgs{ //nolint:exhaustruct
		Cluster: "c", Instance: "h", DurationSeconds: 5, Class: "Lock",
	})
	if err != nil {
		t.Fatalf("sampleActivity: %v", err)
	}

	res, ok := got.(sampleActivityResult)
	if !ok {
		t.Fatalf("result type %T", got)
	}

	if !res.TextsTruncated {
		t.Error("texts_truncated not set")
	}

	if n := len(*res.TopQueries[0].Text); n > activityMaxTextBytes+64 {
		t.Errorf("text is %d bytes", n)
	}

	if seen.Get("duration_seconds") != "5" || seen.Get("class") != "Lock" || seen.Has("top") || seen.Has("period_seconds") {
		t.Errorf("query = %v", seen)
	}
}

func TestSampleActivityShortTextUntouched(t *testing.T) {
	t.Parallel()

	c := activityFakeAPI(t, http.StatusOK, activityProfileJSON("select 1"), nil)

	got, err := sampleActivity(context.Background(), c, sampleActivityArgs{Cluster: "c", Instance: "h"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("sampleActivity: %v", err)
	}

	res := got.(sampleActivityResult) //nolint:forcetypeassert

	if res.TextsTruncated || *res.TopQueries[0].Text != "select 1" {
		t.Errorf("text = %q, truncated = %v", *res.TopQueries[0].Text, res.TextsTruncated)
	}
}

func TestSampleActivityErrors(t *testing.T) {
	t.Parallel()

	_, err := sampleActivity(context.Background(), activityFakeAPI(t, http.StatusNotImplemented, "", nil),
		sampleActivityArgs{Cluster: "c", Instance: "h"}) //nolint:exhaustruct
	if !errors.Is(err, errActivityUnsupported) {
		t.Errorf("501: err = %v", err)
	}

	_, err = sampleActivity(context.Background(), activityFakeAPI(t, http.StatusGatewayTimeout, `{"message":"all 15 samples failed"}`, nil),
		sampleActivityArgs{Cluster: "c", Instance: "h"}) //nolint:exhaustruct
	if !errors.Is(err, errQueryTimeout) || !strings.Contains(err.Error(), "all 15 samples failed") {
		t.Errorf("504: err = %v", err)
	}

	_, err = sampleActivity(context.Background(), activityFakeAPI(t, http.StatusBadRequest, `{"message":"duration_seconds must be between 5 and 60"}`, nil),
		sampleActivityArgs{Cluster: "c", Instance: "h", DurationSeconds: 3}) //nolint:exhaustruct
	if err == nil || !strings.Contains(err.Error(), "duration_seconds") {
		t.Errorf("400: err = %v", err)
	}

	_, err = sampleActivity(context.Background(), activityFakeAPI(t, http.StatusOK, "", nil),
		sampleActivityArgs{Cluster: "c", Instance: "h", Top: 21}) //nolint:exhaustruct
	if err == nil {
		t.Error("top above 20 accepted")
	}
}
