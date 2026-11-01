package patroni

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newTestConsul(t *testing.T, cfg ConsulConfig, handler http.HandlerFunc) *consulStore {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	if cfg.Address == "" {
		cfg.Address = srv.URL
	}

	s, err := newConsulStore(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("newConsulStore() error = %v", err)
	}

	s.retryDelay = time.Millisecond

	t.Cleanup(func() { _ = s.Close() })

	return s
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestConsulListOK(t *testing.T) {
	t.Parallel()

	seen := make(chan [3]string, 1)

	s := newTestConsul(t, ConsulConfig{Token: "secret", Datacenter: "dc2"}, //nolint:exhaustruct
		func(w http.ResponseWriter, r *http.Request) {
			seen <- [3]string{r.URL.Path, r.URL.RawQuery, r.Header.Get(consulTokenHdr)}

			_, _ = fmt.Fprintf(w, `[
				{"Key": "service/alpha/members/alpha-1", "Value": %q},
				{"Key": "service/alpha/", "Value": null}
			]`, b64(`{"conn_url":"postgres://10.0.0.1:5432/postgres"}`))
		})

	got, err := s.list(t.Context(), "service/")
	if err != nil {
		t.Fatalf("list() error = %v", err)
	}

	req := <-seen
	gotPath, gotQuery, gotToken := req[0], req[1], req[2]

	if gotPath != "/v1/kv/service/" {
		t.Errorf("path = %q, want /v1/kv/service/", gotPath)
	}

	if gotQuery != "dc=dc2&recurse=true" {
		t.Errorf("query = %q, want dc=dc2&recurse=true", gotQuery)
	}

	if gotToken != "secret" {
		t.Errorf("token header = %q, want secret", gotToken)
	}

	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2", len(got))
	}

	if got[0].Key != "service/alpha/members/alpha-1" || !strings.Contains(string(got[0].Value), "10.0.0.1") {
		t.Errorf("got[0] = %q %q, want the decoded member", got[0].Key, got[0].Value)
	}

	if got[1].Value != nil {
		t.Errorf("folder key value = %q, want nil", got[1].Value)
	}
}

func TestConsulListEmptyPrefixAndNoToken(t *testing.T) {
	t.Parallel()

	type request struct {
		path     string
		hasToken bool
	}

	seen := make(chan request, 1)

	s := newTestConsul(t, ConsulConfig{}, func(w http.ResponseWriter, r *http.Request) { //nolint:exhaustruct
		_, hasToken := r.Header[consulTokenHdr]
		seen <- request{path: r.URL.Path, hasToken: hasToken}

		_, _ = w.Write([]byte(`[]`))
	})

	if _, err := s.list(t.Context(), ""); err != nil {
		t.Fatalf("list() error = %v", err)
	}

	req := <-seen

	if req.path != "/v1/kv/" {
		t.Errorf("path = %q, want /v1/kv/", req.path)
	}

	if req.hasToken {
		t.Error("token header sent without a configured token")
	}
}

func TestConsulListStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statuses   []int // recursive list, by attempt; 204 stands for 200 with []
		probe      int   // single-key read of the prefix; 0 is 404
		wantErr    error
		wantEmpty  bool
		wantCalls  int32
		wantProbes int32
	}{
		{name: "404 readable is an empty namespace", statuses: []int{404}, wantEmpty: true, wantCalls: 1, wantProbes: 1},
		{name: "404 hidden by ACL", statuses: []int{404}, probe: 403, wantErr: errConsulForbidden, wantCalls: 1, wantProbes: 1},
		{name: "empty list hidden by ACL", statuses: []int{204}, probe: 403, wantErr: errConsulForbidden, wantCalls: 1, wantProbes: 1},
		{name: "probe 5xx retries the list", statuses: []int{404}, probe: 503, wantErr: errConsulStatus, wantCalls: 2, wantProbes: 2},
		{name: "403 points at the ACL", statuses: []int{403}, wantErr: errConsulForbidden, wantCalls: 1},
		{name: "400 is not retried", statuses: []int{400}, wantErr: errConsulStatus, wantCalls: 1},
		{name: "5xx retried once, then keys", statuses: []int{503, 200}, wantCalls: 2},
		{name: "5xx twice fails", statuses: []int{500, 502}, wantErr: errConsulStatus, wantCalls: 2},
		{name: "5xx then 404", statuses: []int{500, 404}, wantEmpty: true, wantCalls: 2, wantProbes: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls, probes atomic.Int32

			s := newTestConsul(t, ConsulConfig{}, func(w http.ResponseWriter, r *http.Request) { //nolint:exhaustruct
				if !r.URL.Query().Has("recurse") {
					probes.Add(1)
					w.WriteHeader(cmp.Or(tt.probe, http.StatusNotFound))

					return
				}

				n := calls.Add(1)

				switch status := tt.statuses[min(int(n), len(tt.statuses))-1]; status {
				case http.StatusOK:
					_, _ = fmt.Fprintf(w, `[{"Key": "service/a/members/m", "Value": %q}]`, b64("{}"))
				case http.StatusNoContent:
					_, _ = w.Write([]byte(`[]`))
				default:
					w.WriteHeader(status)
				}
			})

			got, err := s.list(t.Context(), "service/")

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("list() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("list() error = %v", err)
			}

			if tt.wantEmpty && (got == nil || len(got) != 0) {
				t.Errorf("got %v, want an empty non-nil result", got)
			}

			if c := calls.Load(); c != tt.wantCalls {
				t.Errorf("list requests = %d, want %d", c, tt.wantCalls)
			}

			if c := probes.Load(); c != tt.wantProbes {
				t.Errorf("probe requests = %d, want %d", c, tt.wantProbes)
			}
		})
	}
}

func TestConsulListFilteredByACL(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			s := newTestConsul(t, ConsulConfig{}, func(w http.ResponseWriter, _ *http.Request) { //nolint:exhaustruct
				w.Header().Set(consulFilteredHdr, "true")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`[]`))
			})

			// An empty list here must not read as "every cluster was removed".
			if _, err := s.list(t.Context(), "service/"); !errors.Is(err, errConsulForbidden) {
				t.Fatalf("list() error = %v, want errConsulForbidden", err)
			}
		})
	}
}

func TestConsulListBadJSON(t *testing.T) {
	t.Parallel()

	s := newTestConsul(t, ConsulConfig{}, func(w http.ResponseWriter, _ *http.Request) { //nolint:exhaustruct
		_, _ = w.Write([]byte(`{"not": "an array"}`))
	})

	if _, err := s.list(t.Context(), "service/"); err == nil {
		t.Fatal("list() error = nil, want a decode error")
	}
}

func TestConsulListNetworkErrorRetried(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()

	s, err := newConsulStore(ConsulConfig{Address: addr}, zap.NewNop()) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("newConsulStore() error = %v", err)
	}

	s.retryDelay = time.Millisecond

	if _, err := s.list(t.Context(), "service/"); err == nil {
		t.Fatal("list() error = nil, want a connection error")
	}
}

func TestConsulListCanceledDuringRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	s := newTestConsul(t, ConsulConfig{}, func(w http.ResponseWriter, _ *http.Request) { //nolint:exhaustruct
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	s.retryDelay = time.Hour

	ctx, cancel := context.WithCancel(t.Context())

	go func() {
		for calls.Load() == 0 {
			time.Sleep(time.Millisecond)
		}

		cancel()
	}()

	_, err := s.list(ctx, "service/")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("list() error = %v, want context.Canceled", err)
	}

	if c := calls.Load(); c != 1 {
		t.Errorf("requests = %d, want 1", c)
	}
}

func TestNewConsulStoreAddress(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"consul:8500", "ftp://consul", "http://", "://bad"} {
		if _, err := newConsulStore(ConsulConfig{Address: addr}, zap.NewNop()); !errors.Is(err, errConsulAddress) { //nolint:exhaustruct
			t.Errorf("newConsulStore(%q) error = %v, want errConsulAddress", addr, err)
		}
	}
}

func TestConsulKVURL(t *testing.T) {
	t.Parallel()

	s, err := newConsulStore(ConsulConfig{Address: "https://consul.local:8501/proxy/"}, zap.NewNop()) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("newConsulStore() error = %v", err)
	}

	if got, want := s.kvURL("a b/", true), "https://consul.local:8501/proxy/v1/kv/a%20b/?recurse=true"; got != want {
		t.Errorf("kvURL(recurse) = %q, want %q", got, want)
	}

	if got, want := s.kvURL("a b/", false), "https://consul.local:8501/proxy/v1/kv/a%20b/"; got != want {
		t.Errorf("kvURL() = %q, want %q", got, want)
	}
}

func TestNewConsulStoreTLSError(t *testing.T) {
	t.Parallel()

	cfg := ConsulConfig{ //nolint:exhaustruct
		Address: "https://consul.local:8501",
		TLS:     TLSConfig{CertFile: "/nonexistent.pem"}, //nolint:exhaustruct
	}

	if _, err := newConsulStore(cfg, zap.NewNop()); !errors.Is(err, errCertKeyPair) {
		t.Fatalf("newConsulStore() error = %v, want errCertKeyPair", err)
	}
}
