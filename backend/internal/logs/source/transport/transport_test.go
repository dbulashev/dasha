package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

func testClient(t *testing.T, addresses []string, auth config.LogSourceAuthConfig) *Client {
	t.Helper()

	c, err := New(config.LogSourceConfig{ //nolint:exhaustruct
		Addresses: addresses,
		Auth:      auth,
	}, 5*time.Second, Options{NotFound: "index not found", Headers: map[string]string{"AccountID": "7"}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	return c
}

func discard(io.Reader) error { return nil }

func get(path string) Request {
	return Request{Method: http.MethodGet, Path: path, ContentType: "", Body: nil}
}

func TestStatusClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status  int
		config  bool
		message string
	}{
		{http.StatusUnauthorized, true, "credentials"},
		{http.StatusForbidden, true, "credentials"},
		{http.StatusNotFound, true, "index not found"},
		{http.StatusBadRequest, true, "rejected the query"},
		{http.StatusInternalServerError, false, "log store answered"},
	}

	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte("detail"))
		}))

		c := testClient(t, []string{srv.URL}, config.LogSourceAuthConfig{Kind: config.LogAuthNone}) //nolint:exhaustruct
		err := c.Do(context.Background(), get("/x"), discard)

		srv.Close()

		if err == nil {
			t.Fatalf("status %d produced no error", tt.status)
		}

		if got := errors.Is(err, source.ErrConfig); got != tt.config {
			t.Errorf("status %d: ErrConfig = %v, want %v (%v)", tt.status, got, tt.config, err)
		}

		if !strings.Contains(err.Error(), tt.message) {
			t.Errorf("status %d: error %q does not mention %q", tt.status, err, tt.message)
		}
	}
}

// TestCallFallsBackOnlyOnTransportFailure: an address that cannot be reached
// moves on to the next, a store that answers with an error does not.
func TestCallFallsBackOnlyOnTransportFailure(t *testing.T) {
	t.Parallel()

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++

		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	var out map[string]bool

	c := testClient(t, []string{deadURL, srv.URL}, config.LogSourceAuthConfig{Kind: config.LogAuthNone}) //nolint:exhaustruct
	if err := c.JSON(context.Background(), http.MethodGet, "/x", nil, &out); err != nil {
		t.Fatalf("call: %v", err)
	}

	if !out["ok"] || hits != 1 {
		t.Fatalf("second address answered %v after %d requests", out, hits)
	}

	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer rejecting.Close()

	before := hits

	rc := testClient(t, []string{rejecting.URL, srv.URL}, config.LogSourceAuthConfig{Kind: config.LogAuthNone}) //nolint:exhaustruct
	if err := rc.Do(context.Background(), get("/x"), discard); err == nil {
		t.Fatal("a rejected request fell through to the next address")
	}

	if hits != before {
		t.Fatalf("the next address was tried after a store answered")
	}
}

func TestRequestCarriesCredentialsAndHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		auth config.LogSourceAuthConfig
		want string
	}{
		{config.LogSourceAuthConfig{Kind: config.LogAuthBearer, Token: "t0k"}, "Bearer t0k"},            //nolint:exhaustruct
		{config.LogSourceAuthConfig{Kind: config.LogAuthAPIKey, APIKey: "k3y"}, "ApiKey k3y"},           //nolint:exhaustruct
		{config.LogSourceAuthConfig{Kind: config.LogAuthBasic, User: "u", Password: "p"}, "Basic dTpw"}, //nolint:exhaustruct
		{config.LogSourceAuthConfig{Kind: config.LogAuthNone}, ""},                                      //nolint:exhaustruct
	}

	for _, tt := range tests {
		var gotAuth, gotAccount, gotType string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			gotAccount = r.Header.Get("AccountID")
			gotType = r.Header.Get("Content-Type")

			_, _ = w.Write([]byte("{}"))
		}))

		req := Request{
			Method:      http.MethodPost,
			Path:        "/x",
			ContentType: "application/x-www-form-urlencoded",
			Body:        []byte("query=*"),
		}

		if err := testClient(t, []string{srv.URL}, tt.auth).Do(context.Background(), req, discard); err != nil {
			t.Fatalf("call: %v", err)
		}

		srv.Close()

		if gotAuth != tt.want {
			t.Errorf("auth %q: Authorization = %q, want %q", tt.auth.Kind, gotAuth, tt.want)
		}

		if gotAccount != "7" {
			t.Errorf("auth %q: AccountID = %q", tt.auth.Kind, gotAccount)
		}

		if gotType != "application/x-www-form-urlencoded" {
			t.Errorf("auth %q: Content-Type = %q", tt.auth.Kind, gotType)
		}
	}
}

// TestRedirectIsNotFollowed: credentials must not be replayed to whatever a
// redirect points at.
func TestRedirectIsNotFollowed(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	}))
	defer srv.Close()

	c := testClient(t, []string{srv.URL}, config.LogSourceAuthConfig{Kind: config.LogAuthBearer, Token: "t0k"}) //nolint:exhaustruct
	if err := c.Do(context.Background(), get("/x"), discard); err == nil {
		t.Fatal("a redirect was followed")
	}
}

func TestNoAddressesConfigured(t *testing.T) {
	t.Parallel()

	c := testClient(t, nil, config.LogSourceAuthConfig{Kind: config.LogAuthNone}) //nolint:exhaustruct

	err := c.Do(context.Background(), get("/x"), discard)
	if !errors.Is(err, source.ErrConfig) {
		t.Fatalf("error = %v, want ErrConfig", err)
	}
}
