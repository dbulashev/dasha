package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDashaClient_CrossOriginRedirectKeepsTheToken(t *testing.T) {
	t.Parallel()

	var gotKey atomic.Value

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.Header.Get("X-API-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(target.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	c, err := NewDashaClient(Config{DashaURL: origin.URL, Token: "dasha_pat_secret"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	if _, err = c.Clusters(context.Background()); err == nil {
		t.Error("Clusters must fail rather than follow a redirect to another origin")
	}

	if k, _ := gotKey.Load().(string); k != "" {
		t.Errorf("redirect target received X-API-Key %q, want none", k)
	}
}

func TestDashaClient_SameOriginRedirectIsFollowed(t *testing.T) {
	t.Parallel()

	var gotKey atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/moved" {
			http.Redirect(w, r, "/moved", http.StatusFound)

			return
		}

		gotKey.Store(r.Header.Get("X-API-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewDashaClient(Config{DashaURL: srv.URL, Token: "dasha_pat_secret"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	if _, err = c.Clusters(context.Background()); err != nil {
		t.Fatalf("Clusters over a same-origin redirect: %v", err)
	}

	if k, _ := gotKey.Load().(string); k != "dasha_pat_secret" {
		t.Errorf("same-origin redirect target received X-API-Key %q, want the token", k)
	}
}
