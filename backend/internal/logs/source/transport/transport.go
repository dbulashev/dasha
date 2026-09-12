// Package transport speaks HTTP to a log store: the address list with
// fallback, TLS, credentials and the status classification every source
// shares.
package transport

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

// Options carry the parts of an exchange that differ per store.
type Options struct {
	// NotFound describes what a 404 means for this store.
	NotFound string
	// Headers are sent with every request.
	Headers map[string]string
}

// Client is one configured log store endpoint.
type Client struct {
	addresses []string
	auth      config.LogSourceAuthConfig
	opts      Options
	http      *http.Client
}

// New builds the client of a source. A CA file that holds no certificate fails
// here, at startup.
func New(cfg config.LogSourceConfig, timeout time.Duration, opts Options) (*Client, error) {
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

	if opts.NotFound == "" {
		opts.NotFound = "not found"
	}

	return &Client{
		addresses: addresses,
		auth:      cfg.Auth,
		opts:      opts,
		http: &http.Client{ //nolint:exhaustruct
			Timeout: timeout,
			Transport: &http.Transport{ //nolint:exhaustruct
				TLSClientConfig: tlsCfg,
				Proxy:           http.ProxyFromEnvironment,
			},
			// Every request carries the store credentials, which must not be
			// replayed to whatever a redirect points at.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Request is one prepared exchange with a store.
type Request struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

// JSON sends body as a JSON document and decodes the answer into out. Both may
// be nil.
func (c *Client) JSON(ctx context.Context, method, path string, body, out any) error {
	var payload []byte

	if body != nil {
		var err error

		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	req := Request{Method: method, Path: path, ContentType: "application/json", Body: payload}

	return c.Do(ctx, req, func(r io.Reader) error {
		if out == nil {
			return nil
		}

		if err := json.NewDecoder(r).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		return nil
	})
}

// Do sends one request, trying the configured addresses in order until one
// answers, and hands the body of a successful answer to fn. A store that
// answers with an error status ends the attempt: only a transport failure
// moves on to the next address.
func (c *Client) Do(ctx context.Context, req Request, fn func(io.Reader) error) error {
	var lastErr error

	for _, addr := range c.addresses {
		err := c.doOne(ctx, addr+req.Path, req, fn)
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

func (c *Client) doOne(ctx context.Context, url string, req Request, fn func(io.Reader) error) error {
	var reader io.Reader
	if req.Body != nil {
		reader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}

	for k, v := range c.opts.Headers {
		httpReq.Header.Set(k, v)
	}

	switch c.auth.Kind {
	case config.LogAuthBasic:
		httpReq.SetBasicAuth(c.auth.User, c.auth.Password)
	case config.LogAuthAPIKey:
		httpReq.Header.Set("Authorization", "ApiKey "+c.auth.APIKey)
	case config.LogAuthBearer:
		httpReq.Header.Set("Authorization", "Bearer "+c.auth.Token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return &transportError{err: err}
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return c.statusError(resp)
	}

	return fn(resp.Body)
}

// statusError turns a non-2xx answer into a classified error: the statuses an
// operator can fix are configuration errors, the rest are upstream failures.
func (c *Client) statusError(resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
	detail := strings.TrimSpace(string(snippet))

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: log store rejected the credentials (%s)", source.ErrConfig, resp.Status)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s (%s): %s", source.ErrConfig, c.opts.NotFound, resp.Status, detail)
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
