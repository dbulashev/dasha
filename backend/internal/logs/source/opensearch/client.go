package opensearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// errBodyLimit caps how much of an error response is quoted back.
const errBodyLimit = 512

type client struct {
	addresses []string
	auth      config.LogSourceAuthConfig
	http      *http.Client
}

func newClient(cfg config.LogSourceConfig, timeout time.Duration) (*client, error) {
	tlsCfg := &tls.Config{ //nolint:exhaustruct
		MinVersion: tls.VersionTLS12,
		// #nosec G402 -- opting out of verification is an explicit operator choice.
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
	}

	if cfg.TLS.CAFile != "" {
		pem, err := os.ReadFile(cfg.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("%w: read ca_file: %w", source.ErrConfig, err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%w: ca_file %q holds no certificate", source.ErrConfig, cfg.TLS.CAFile)
		}

		tlsCfg.RootCAs = pool
	}

	addresses := make([]string, 0, len(cfg.Addresses))
	for _, a := range cfg.Addresses {
		addresses = append(addresses, strings.TrimRight(a, "/"))
	}

	return &client{
		addresses: addresses,
		auth:      cfg.Auth,
		http: &http.Client{ //nolint:exhaustruct
			Timeout:   timeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg}, //nolint:exhaustruct
		},
	}, nil
}

// call sends one request, trying the configured addresses in order until one
// answers. A store that answers with an error status ends the attempt: only a
// transport failure moves on to the next address.
func (c *client) call(ctx context.Context, method, path string, body, out any) error {
	var payload []byte

	if body != nil {
		var err error

		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	var lastErr error

	for _, addr := range c.addresses {
		err := c.callOne(ctx, method, addr+path, payload, out)
		if err == nil {
			return nil
		}

		var transport *transportError
		if !errors.As(err, &transport) {
			return err
		}

		// A cancelled or timed-out request fails the same way on every address.
		if ctx.Err() != nil {
			return err
		}

		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("%w: no addresses configured", source.ErrConfig)
	}

	return lastErr
}

func (c *client) callOne(ctx context.Context, method, url string, payload []byte, out any) error {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	switch c.auth.Kind {
	case config.LogAuthBasic:
		req.SetBasicAuth(c.auth.User, c.auth.Password)
	case config.LogAuthAPIKey:
		req.Header.Set("Authorization", "ApiKey "+c.auth.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &transportError{err: err}
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// statusError turns a non-2xx answer into a classified error: the statuses an
// operator can fix are configuration errors, the rest are upstream failures.
func statusError(resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
	detail := strings.TrimSpace(string(snippet))

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: log store rejected the credentials (%s)", source.ErrConfig, resp.Status)
	case http.StatusNotFound:
		return fmt.Errorf("%w: index not found (%s): %s", source.ErrConfig, resp.Status, detail)
	case http.StatusBadRequest:
		return fmt.Errorf("%w: log store rejected the query (%s): %s", source.ErrConfig, resp.Status, detail)
	default:
		return fmt.Errorf("log store answered %s: %s", resp.Status, detail)
	}
}

type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }
