package mcpserver

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxCachedServers bounds the per-token server cache. It is an LRU, so a flood of
// distinct tokens evicts cold entries (including revoked ones) instead of either
// growing without limit or degrading to a per-request rebuild once full.
const maxCachedServers = 1024

// HTTPHandler returns a streamable-HTTP MCP handler with per-user passthrough:
// each request's token (Authorization: Bearer … or X-API-Key) is bound to a
// DashaClient copy, so every caller acts with their own Dasha identity and RBAC.
// A request that carries no token falls back to the server's configured token
// (DASHA_MCP_TOKEN) when one is set — leave it unset to require per-user
// credentials. Runs stateless (one logical session per request), which suits a
// read-only request/response tool server.
//
// One MCP server is built and cached per distinct token (tools/prompts and their
// schemas are derived once per identity, not per request). The cache is keyed by
// a hash of the token so raw secrets are not retained as map keys, and bounded
// (LRU) so it cannot grow without limit.
func HTTPHandler(base *DashaClient, version, lang string) http.Handler {
	// One schema cache shared across all per-token servers: the tool input
	// schemas are identical for every identity, so derive them once by reflection
	// rather than per token.
	schemas := mcp.NewSchemaCache()
	cache := newServerCache(maxCachedServers)

	return mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		token := tokenFromRequest(req)

		return cache.get(tokenCacheKey(token), func() *mcp.Server {
			return newServer(base.withToken(token), version, lang, schemas)
		})
	}, &mcp.StreamableHTTPOptions{Stateless: true}) //nolint:exhaustruct
}

// serverCache is a bounded LRU of per-token MCP servers.
type serverCache struct {
	mu    sync.Mutex
	ll    *list.List // front = most recently used
	items map[string]*list.Element
	cap   int
}

type serverCacheItem struct {
	key    string
	server *mcp.Server
}

func newServerCache(capacity int) *serverCache {
	return &serverCache{
		ll:    list.New(),
		items: make(map[string]*list.Element, capacity),
		cap:   capacity,
	}
}

// get returns the cached server for key, building and inserting one via build on
// a miss (evicting the least-recently-used entry when over capacity). build runs
// outside the lock so concurrent misses for distinct tokens don't serialize.
func (c *serverCache) get(key string, build func() *mcp.Server) *mcp.Server {
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		srv := el.Value.(*serverCacheItem).server
		c.mu.Unlock()

		return srv
	}
	c.mu.Unlock()

	srv := build()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Another goroutine may have inserted this key while we were building.
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)

		return el.Value.(*serverCacheItem).server
	}

	c.items[key] = c.ll.PushFront(&serverCacheItem{key: key, server: srv})

	if c.ll.Len() > c.cap {
		if oldest := c.ll.Back(); oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*serverCacheItem).key)
		}
	}

	return srv
}

// tokenFromRequest extracts the caller's Dasha token from a bearer Authorization
// header or the X-API-Key header. The auth scheme is matched case-insensitively
// per RFC 7235, so "bearer …" is accepted as well as "Bearer …".
func tokenFromRequest(r *http.Request) string {
	const scheme = "bearer "
	if h := r.Header.Get("Authorization"); len(h) >= len(scheme) && strings.EqualFold(h[:len(scheme)], scheme) {
		return strings.TrimSpace(h[len(scheme):])
	}

	return r.Header.Get("X-API-Key")
}

// tokenCacheKey hashes the token so the raw secret is never used as a cache key
// (and never appears in a memory dump as a plain map key).
func tokenCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
