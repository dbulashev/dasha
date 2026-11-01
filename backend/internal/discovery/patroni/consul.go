package patroni

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/config"
)

const (
	consulMaxBody    = 64 << 20
	consulRetryDelay = time.Second
	consulTokenHdr   = "X-Consul-Token" //nolint:gosec // header name, not a credential
	// Set by some Consul versions when ACLs hid keys from a list.
	consulFilteredHdr = "X-Consul-Results-Filtered-By-ACLs"
)

var (
	errConsulAddress   = errors.New("consul.address must be an http(s)://host[:port] URL")
	errConsulForbidden = errors.New("consul denied the read: check ACL policy key_prefix read on the namespace")
	errConsulStatus    = errors.New("consul unexpected status")
	errConsulBodyLimit = errors.New("consul response exceeds the size limit")
	errTransport       = errors.New("http.DefaultTransport is not *http.Transport")
)

type consulStore struct {
	base       *url.URL
	datacenter string
	token      string
	client     *http.Client
	retryDelay time.Duration
}

type consulKV struct {
	Key   string `json:"Key"`
	Value []byte `json:"Value"` // base64 in JSON, null for folder keys
}

func newConsulStore(cfg ConsulConfig, logger *zap.Logger) (*consulStore, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.Address))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errConsulAddress
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errTransport
	}

	transport = transport.Clone()

	if cfg.TLS.isSet() || base.Scheme == "https" {
		tlsCfg, err := cfg.TLS.build()
		if err != nil {
			return nil, err
		}

		transport.TLSClientConfig = tlsCfg
	}

	token, unset := config.ResolveSecret(cfg.Token, cfg.TokenFromEnv)
	if unset {
		logger.Warn("consul.token_from_env is set but the variable is empty",
			zap.String("variable", cfg.TokenFromEnv),
		)
	}

	return &consulStore{
		base:       base,
		datacenter: cfg.Datacenter,
		token:      token,
		client:     &http.Client{Transport: transport, Timeout: dcsTimeout}, //nolint:exhaustruct
		retryDelay: consulRetryDelay,
	}, nil
}

// list retries once on a network error or a 5xx; a 404 is an empty prefix.
func (s *consulStore) list(ctx context.Context, prefix string) ([]kv, error) {
	out, retry, err := s.get(ctx, prefix)
	if err == nil || !retry {
		return out, err
	}

	timer := time.NewTimer(s.retryDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("consul list %q: %w", prefix, ctx.Err())
	case <-timer.C:
	}

	out, _, err = s.get(ctx, prefix)

	return out, err
}

// get lists prefix. A recursive list drops the keys the token may not read
// instead of refusing, so an empty answer is confirmed with checkRead.
func (s *consulStore) get(ctx context.Context, prefix string) (_ []kv, retry bool, _ error) {
	resp, retry, err := s.send(ctx, prefix, true)
	if err != nil {
		return nil, retry, err
	}
	defer resp.Body.Close()

	var out []kv

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.Header.Get(consulFilteredHdr) == "true":
		return nil, false, errConsulForbidden
	case resp.StatusCode == http.StatusOK:
		out, err = decodeKVs(resp.Body)
		if err != nil {
			return nil, false, err
		}
	case resp.StatusCode == http.StatusNotFound:
		out = []kv{}
	case resp.StatusCode >= http.StatusInternalServerError:
		return nil, true, fmt.Errorf("%w: %s", errConsulStatus, resp.Status)
	default:
		return nil, false, fmt.Errorf("%w: %s", errConsulStatus, resp.Status)
	}

	if len(out) == 0 && prefix != "" {
		if retry, err := s.checkRead(ctx, prefix); err != nil {
			return nil, retry, err
		}
	}

	return out, false, nil
}

// checkRead reads the prefix itself as a single key: unlike a list, a
// single-key read is refused with 403 when the token may not read it.
func (s *consulStore) checkRead(ctx context.Context, prefix string) (retry bool, _ error) {
	resp, retry, err := s.send(ctx, prefix, false)
	if err != nil {
		return retry, err
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, consulMaxBody))
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden:
		return false, errConsulForbidden
	case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= http.StatusInternalServerError:
		return true, fmt.Errorf("%w: %s", errConsulStatus, resp.Status)
	default:
		return false, fmt.Errorf("%w: %s", errConsulStatus, resp.Status)
	}
}

func (s *consulStore) send(ctx context.Context, prefix string, recurse bool) (_ *http.Response, retry bool, _ error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.kvURL(prefix, recurse), http.NoBody)
	if err != nil {
		return nil, false, fmt.Errorf("consul request: %w", err)
	}

	if s.token != "" {
		req.Header.Set(consulTokenHdr, s.token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, fmt.Errorf("consul get %q: %w", prefix, err)
	}

	return resp, false, nil
}

func decodeKVs(body io.Reader) ([]kv, error) {
	raw, err := io.ReadAll(io.LimitReader(body, consulMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("consul read body: %w", err)
	}

	if len(raw) > consulMaxBody {
		return nil, errConsulBodyLimit
	}

	var items []consulKV
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("consul decode: %w", err)
	}

	out := make([]kv, 0, len(items))
	for _, item := range items {
		out = append(out, kv{Key: item.Key, Value: item.Value})
	}

	return out, nil
}

// kvURL keeps the trailing slash of prefix: without it Consul would also match
// sibling namespaces ("service2/").
func (s *consulStore) kvURL(prefix string, recurse bool) string {
	u := *s.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/kv/" + prefix
	u.RawPath = ""

	q := url.Values{}
	if recurse {
		q.Set("recurse", "true")
	}

	if s.datacenter != "" {
		q.Set("dc", s.datacenter)
	}

	u.RawQuery = q.Encode()

	return u.String()
}

func (s *consulStore) Close() error {
	s.client.CloseIdleConnections()

	return nil
}
