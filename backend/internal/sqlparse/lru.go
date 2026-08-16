package sqlparse

import (
	"container/list"
	"sync"
)

// cacheKey is sha256 of the query text: pg_stat_statements texts run to kilobytes
// and the top of the list barely moves between polls, so the same texts are
// re-parsed on every report unless they are cached.
type cacheKey [32]byte

type cacheEntry struct {
	key cacheKey
	st  Statement
	err error
}

// lru caches parse outcomes, failures included — a clipped or unparseable
// statement stays clipped, and re-parsing it every poll only burns WASM time.
type lru struct {
	mu    sync.Mutex
	max   int
	order *list.List
	items map[cacheKey]*list.Element
}

func newLRU(size int) *lru {
	return &lru{
		max:   size,
		order: list.New(),
		items: make(map[cacheKey]*list.Element, size),
	}
}

func (c *lru) get(key cacheKey) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return cacheEntry{}, false
	}

	c.order.MoveToFront(el)

	return *el.Value.(*cacheEntry), true //nolint:forcetypeassert // only *cacheEntry is ever stored
}

func (c *lru) put(key cacheKey, st Statement, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		el.Value = &cacheEntry{key: key, st: st, err: err}
		c.order.MoveToFront(el)

		return
	}

	c.items[key] = c.order.PushFront(&cacheEntry{key: key, st: st, err: err})

	for c.order.Len() > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}

		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheEntry).key) //nolint:forcetypeassert // only *cacheEntry is ever stored
	}
}
