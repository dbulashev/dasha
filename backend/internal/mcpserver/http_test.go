package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTokenFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"bearer", map[string]string{"Authorization": "Bearer dasha_pat_abc"}, "dasha_pat_abc"},
		{"bearer trimmed", map[string]string{"Authorization": "Bearer  dasha_pat_abc "}, "dasha_pat_abc"},
		{"x-api-key", map[string]string{"X-API-Key": "dasha_pat_xyz"}, "dasha_pat_xyz"},
		{"non-bearer authorization ignored", map[string]string{"Authorization": "Basic zzz"}, ""},
		{"none", map[string]string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodPost, "/", nil)
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}

			if got := tokenFromRequest(r); got != tt.want {
				t.Errorf("tokenFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTokenCacheKey(t *testing.T) {
	t.Parallel()

	// Deterministic, hashed (64 hex chars), and never the raw secret.
	const secret = "dasha_pat_secret"

	k1 := tokenCacheKey(secret)
	if k1 != tokenCacheKey(secret) {
		t.Errorf("tokenCacheKey is not deterministic")
	}

	if k1 == secret {
		t.Errorf("tokenCacheKey must not return the raw token")
	}

	if len(k1) != 64 {
		t.Errorf("tokenCacheKey length = %d, want 64 (sha256 hex)", len(k1))
	}

	if tokenCacheKey("a") == tokenCacheKey("b") {
		t.Errorf("distinct tokens must not collide")
	}
}

func TestEditorHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      Config
		ctxProto string // scheme of the inbound request, if any
		token    string
		proto    string
	}{
		// No inbound scheme (stdio): the header is omitted rather than guessed.
		{"token only", Config{DashaURL: "http://localhost", Token: "tok"}, "", "tok", ""}, //nolint:exhaustruct
		{
			"inbound proto is forwarded",
			Config{DashaURL: "http://localhost", Token: "tok"}, //nolint:exhaustruct
			"https", "tok", "https",
		},
		{
			"plain-http caller is reported as such",
			Config{DashaURL: "http://localhost", Token: "tok"}, //nolint:exhaustruct
			"http", "tok", "http",
		},
		{"neither", Config{DashaURL: "http://localhost"}, "", "", ""}, //nolint:exhaustruct
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewDashaClient(tt.cfg)
			if err != nil {
				t.Fatalf("NewDashaClient: %v", err)
			}

			ctx := t.Context()
			if tt.ctxProto != "" {
				ctx = withForwardedProto(ctx, tt.ctxProto)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if err := c.editor(ctx)(ctx, req); err != nil {
				t.Fatalf("editor: %v", err)
			}

			if got := req.Header.Get("X-API-Key"); got != tt.token {
				t.Errorf("X-API-Key = %q, want %q", got, tt.token)
			}

			if got := req.Header.Get("X-Forwarded-Proto"); got != tt.proto {
				t.Errorf("X-Forwarded-Proto = %q, want %q", got, tt.proto)
			}
		})
	}
}

func TestNormalizeProto(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https":            "https",
		"http":             "http",
		"HTTPS":            "https",
		"  https  ":        "https",
		"https, http":      "https", // chained proxies append; the first is the client
		"":                 "",
		"ftp":              "",
		"https\r\nX-A: b":  "", // never forward an attacker-shaped value
		"javascript:alert": "",
	}

	for in, want := range tests {
		if got := normalizeProto(in); got != want {
			t.Errorf("normalizeProto(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServerCache_LRUEvictionAndReuse(t *testing.T) {
	t.Parallel()

	client, err := NewDashaClient(Config{DashaURL: "http://localhost"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	cache := newServerCache(2)

	var builds int
	build := func() *mcp.Server {
		builds++

		return newServer(client, "test", "en", nil)
	}

	cache.get("a", build)
	cache.get("a", build) // cached, no rebuild
	if builds != 1 {
		t.Fatalf("builds after a,a = %d, want 1 (second is cached)", builds)
	}

	cache.get("b", build) // builds=2
	cache.get("c", build) // builds=3, evicts LRU "a"
	if builds != 3 {
		t.Fatalf("builds after b,c = %d, want 3", builds)
	}

	cache.get("a", build) // "a" was evicted -> rebuild (builds=4)
	if builds != 4 {
		t.Errorf("builds after re-get evicted a = %d, want 4 (LRU eviction)", builds)
	}

	if cache.ll.Len() != 2 {
		t.Errorf("cache size = %d, want 2 (bounded)", cache.ll.Len())
	}
}
