package repository

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestForEachLimited_CapsConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const limit = 4

		var running, peak atomic.Int32

		items := make([]int, 17)
		seen := make([]bool, len(items))

		forEachLimited(limit, items, func(i int, _ int) {
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}

			time.Sleep(time.Second)
			seen[i] = true
			running.Add(-1)
		})

		if p := peak.Load(); p != limit {
			t.Errorf("peak concurrency %d, want %d", p, limit)
		}

		for i, ok := range seen {
			if !ok {
				t.Errorf("item %d not visited", i)
			}
		}
	})
}
