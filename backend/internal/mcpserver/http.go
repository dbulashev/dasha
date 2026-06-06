package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxCachedServers bounds the per-token server cache so a flood of distinct
// tokens cannot grow it without limit (a token is accepted into the cache only
// when Dasha would later authorize it, but the cache is populated before that
// check). Beyond the cap, requests are served by an uncached server — correct,
// just not memoized.
const maxCachedServers = 1024

// HTTPHandler returns a streamable-HTTP MCP handler with per-user passthrough:
// each request's token (Authorization: Bearer … or X-API-Key) is bound to a
// DashaClient copy, so every caller acts with their own Dasha identity and RBAC.
// No shared server token is used. Runs stateless (one logical session per
// request), which suits a read-only request/response tool server.
//
// One MCP server is built and cached per distinct token (tools/prompts and their
// schemas are derived once per identity, not per request). The cache is keyed by
// a hash of the token so raw secrets are not retained as map keys, and bounded
// so it cannot grow without limit.
func HTTPHandler(base *DashaClient, version string) http.Handler {
	var (
		servers sync.Map // tokenCacheKey -> *mcp.Server
		cached  atomic.Int64
	)

	// One schema cache shared across all per-token servers: the tool input
	// schemas are identical for every identity, so derive them once by reflection
	// rather than per token.
	schemas := mcp.NewSchemaCache()

	return mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		token := tokenFromRequest(req)
		key := tokenCacheKey(token)

		if s, ok := servers.Load(key); ok {
			return s.(*mcp.Server)
		}

		s := newServer(base.withToken(token), version, schemas)

		// Soft-bound the cache: past the cap, serve an uncached server rather
		// than letting distinct tokens grow memory without limit.
		if cached.Load() >= maxCachedServers {
			return s
		}

		actual, loaded := servers.LoadOrStore(key, s)
		if !loaded {
			cached.Add(1)
		}

		return actual.(*mcp.Server)
	}, &mcp.StreamableHTTPOptions{Stateless: true}) //nolint:exhaustruct
}

// tokenFromRequest extracts the caller's Dasha token from a bearer Authorization
// header or the X-API-Key header.
func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}

	return r.Header.Get("X-API-Key")
}

// tokenCacheKey hashes the token so the raw secret is never used as a cache key
// (and never appears in a memory dump as a plain map key).
func tokenCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
