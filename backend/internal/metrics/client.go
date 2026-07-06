package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// Range is an inclusive time window with a step for range queries.
type Range struct {
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// Sample is one labelled value at a point in time (instant query result).
type Sample struct {
	Labels map[string]string
	Value  float64
	Time   time.Time
}

// SeriesPoint is one value at a point in time within a Series.
type SeriesPoint struct {
	Time  time.Time
	Value float64
}

// Series is a labelled time series (range query result).
type Series struct {
	Labels map[string]string
	Points []SeriesPoint
}

// DatasourceClient queries a Prometheus/VictoriaMetrics-compatible TSDB.
type DatasourceClient interface {
	QueryInstant(ctx context.Context, expr string, at time.Time) ([]Sample, error)
	QueryRange(ctx context.Context, expr string, r Range) ([]Series, error)
}

// VMClient is the HTTP implementation of DatasourceClient.
type VMClient struct {
	baseURL string
	auth    AuthConfig
	http    *http.Client
	logger  *zap.Logger
}

// NewVMClient builds a client for the configured datasource. A nil logger is
// replaced with a no-op; with debug logging on, every query (and its matched
// series count) is logged so wrong selectors are obvious.
func NewVMClient(cfg DatasourceConfig, logger *zap.Logger) *VMClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &VMClient{
		baseURL: cfg.URL,
		auth:    cfg.Auth,
		http:    &http.Client{Timeout: timeout},
		logger:  logger,
	}
}

// apiResponse is the Prometheus HTTP API envelope.
type apiResponse struct {
	Status    string          `json:"status"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Data      apiResponseData `json:"data"`
}

type apiResponseData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

func (c *VMClient) QueryInstant(ctx context.Context, expr string, at time.Time) ([]Sample, error) {
	q := url.Values{}
	q.Set("query", expr)

	if !at.IsZero() {
		q.Set("time", strconv.FormatInt(at.Unix(), 10))
	}

	data, err := c.do(ctx, "/api/v1/query", q)
	if err != nil {
		return nil, err
	}

	if data.ResultType != "vector" {
		return nil, fmt.Errorf("metrics: unexpected resultType %q for instant query", data.ResultType)
	}

	var items []struct {
		Metric map[string]string `json:"metric"`
		Value  []any             `json:"value"`
	}

	if err := json.Unmarshal(data.Result, &items); err != nil {
		return nil, fmt.Errorf("metrics: decode vector: %w", err)
	}

	out := make([]Sample, 0, len(items))

	for _, it := range items {
		t, v, err := parseScalarPair(it.Value)
		if err != nil {
			return nil, err
		}

		out = append(out, Sample{Labels: it.Metric, Value: v, Time: t})
	}

	c.logger.Debug("vmselect instant query", zap.String("query", expr), zap.Int("series", len(out)))

	return out, nil
}

func (c *VMClient) QueryRange(ctx context.Context, expr string, r Range) ([]Series, error) {
	if r.Step <= 0 {
		return nil, fmt.Errorf("metrics: range step must be > 0")
	}

	q := url.Values{}
	q.Set("query", expr)
	q.Set("start", strconv.FormatInt(r.Start.Unix(), 10))
	q.Set("end", strconv.FormatInt(r.End.Unix(), 10))
	q.Set("step", strconv.FormatInt(int64(r.Step.Seconds()), 10))

	data, err := c.do(ctx, "/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}

	if data.ResultType != "matrix" {
		return nil, fmt.Errorf("metrics: unexpected resultType %q for range query", data.ResultType)
	}

	var items []struct {
		Metric map[string]string `json:"metric"`
		Values [][]any           `json:"values"`
	}

	if err := json.Unmarshal(data.Result, &items); err != nil {
		return nil, fmt.Errorf("metrics: decode matrix: %w", err)
	}

	out := make([]Series, 0, len(items))

	for _, it := range items {
		pts := make([]SeriesPoint, 0, len(it.Values))

		for _, pair := range it.Values {
			t, v, err := parseScalarPair(pair)
			if err != nil {
				return nil, err
			}

			pts = append(pts, SeriesPoint{Time: t, Value: v})
		}

		out = append(out, Series{Labels: it.Metric, Points: pts})
	}

	c.logger.Debug("vmselect range query", zap.String("query", expr), zap.Int("series", len(out)))

	return out, nil
}

// do issues the GET request and returns the decoded data envelope.
func (c *VMClient) do(ctx context.Context, path string, q url.Values) (apiResponseData, error) {
	var zero apiResponseData

	if c.baseURL == "" {
		return zero, fmt.Errorf("metrics: datasource url is not configured")
	}

	endpoint := c.baseURL + path + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return zero, fmt.Errorf("metrics: build request: %w", err)
	}

	switch c.auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.auth.Token)
	case "basic":
		req.SetBasicAuth(c.auth.Username, c.auth.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, fmt.Errorf("metrics: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return zero, fmt.Errorf("metrics: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("metrics: datasource returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return zero, fmt.Errorf("metrics: decode response: %w", err)
	}

	if ar.Status != "success" {
		return zero, fmt.Errorf("metrics: query error (%s): %s", ar.ErrorType, ar.Error)
	}

	return ar.Data, nil
}

// parseScalarPair decodes a Prometheus [<unix-seconds>, "<value>"] tuple.
func parseScalarPair(pair []any) (time.Time, float64, error) {
	if len(pair) != 2 {
		return time.Time{}, 0, fmt.Errorf("metrics: malformed sample tuple of len %d", len(pair))
	}

	ts, ok := pair[0].(float64)
	if !ok {
		return time.Time{}, 0, fmt.Errorf("metrics: sample timestamp is not a number")
	}

	str, ok := pair[1].(string)
	if !ok {
		return time.Time{}, 0, fmt.Errorf("metrics: sample value is not a string")
	}

	v, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("metrics: parse sample value %q: %w", str, err)
	}

	sec := int64(ts)

	return time.Unix(sec, int64((ts-float64(sec))*1e9)).UTC(), v, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}
