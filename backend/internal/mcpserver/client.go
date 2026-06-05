package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dbulashev/dasha/gen/apiclient"
)

// tokenKey carries the per-request Dasha API token so the HTTP transport can pass
// each MCP client's identity through to Dasha — there is no shared server token
// in HTTP mode, which keeps users isolated and RBAC intact.
type tokenKey struct{}

// WithToken returns a context carrying the Dasha API token (a static token or a
// personal access token) used for outbound calls made within it.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

func tokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(tokenKey{}).(string)

	return t
}

// DashaClient is a thin, identity-passthrough wrapper over the generated Dasha
// API client: every call forwards the caller's token as the X-API-Key header.
type DashaClient struct {
	api   *apiclient.ClientWithResponses
	token string // default token (stdio single-identity); a per-request ctx token wins
}

// NewDashaClient builds a client against the configured Dasha API.
func NewDashaClient(cfg Config) (*DashaClient, error) {
	cfg = cfg.withDefaults()

	hc := &http.Client{Timeout: cfg.Timeout} //nolint:exhaustruct

	api, err := apiclient.NewClientWithResponses(cfg.DashaURL, apiclient.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("mcp: build dasha client: %w", err)
	}

	return &DashaClient{api: api, token: cfg.Token}, nil
}

// editor injects the effective token as X-API-Key: the per-request token from
// the context (HTTP passthrough) when present, otherwise the configured default
// (stdio single identity).
func (d *DashaClient) editor(ctx context.Context) apiclient.RequestEditorFn {
	token := tokenFromContext(ctx)
	if token == "" {
		token = d.token
	}

	return func(_ context.Context, req *http.Request) error {
		if token != "" {
			req.Header.Set("X-API-Key", token)
		}

		return nil
	}
}

// Clusters lists the configured/discovered clusters of the fleet — the entry
// point for an LLM to pick a (cluster, instance) target.
func (d *DashaClient) Clusters(ctx context.Context) ([]apiclient.Cluster, error) {
	resp, err := d.api.GetClustersWithResponse(ctx, d.editor(ctx))
	if err != nil {
		return nil, fmt.Errorf("mcp: clusters: %w", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("clusters", resp.HTTPResponse)
	}

	return *resp.JSON200, nil
}

// HealthScore returns the instance-level composite health score.
func (d *DashaClient) HealthScore(ctx context.Context, cluster, instance string) (*apiclient.HealthScore, error) {
	resp, err := d.api.GetHealthScoreWithResponse(ctx, &apiclient.GetHealthScoreParams{
		ClusterName: cluster,
		Instance:    instance,
	}, d.editor(ctx))
	if err != nil {
		return nil, fmt.Errorf("mcp: health_score: %w", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("health_score", resp.HTTPResponse)
	}

	return resp.JSON200, nil
}

// Recommendations returns the health-score recommendations; pass a non-nil
// database for the per-database drill-down.
func (d *DashaClient) Recommendations(
	ctx context.Context,
	cluster, instance string,
	database *string,
) (*apiclient.HealthScoreRecommendations, error) {
	resp, err := d.api.GetHealthScoreRecommendationsWithResponse(ctx, &apiclient.GetHealthScoreRecommendationsParams{
		ClusterName: cluster,
		Instance:    instance,
		Database:    database,
	}, d.editor(ctx))
	if err != nil {
		return nil, fmt.Errorf("mcp: recommendations: %w", err)
	}

	if resp.JSON200 == nil {
		return nil, statusError("recommendations", resp.HTTPResponse)
	}

	return resp.JSON200, nil
}

// statusError maps a non-200 Dasha response to a message an LLM can act on.
func statusError(op string, resp *http.Response) error {
	code := 0
	if resp != nil {
		code = resp.StatusCode
	}

	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("dasha: access denied (%d) — the token's role is insufficient for %s", code, op)
	case http.StatusNotFound:
		return fmt.Errorf("dasha: not found (404) — unknown cluster/instance/database")
	default:
		return fmt.Errorf("dasha: %s returned status %d", op, code)
	}
}
