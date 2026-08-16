package sqlparse

import (
	"fmt"
	"testing"
	"time"
)

// workload builds n distinct statements shaped like a real pg_stat_statements
// top: filters, a join, a sort, an aggregate, a write.
func workload(n int) []string {
	shapes := []string{
		`SELECT id, status FROM orders_%d WHERE tenant_id = $1 AND created_at > $2 ORDER BY created_at DESC LIMIT $3`,
		`SELECT o.id, c.name FROM orders_%d o JOIN customers c ON c.id = o.customer_id WHERE c.status = $1 AND o.tenant_id = $2`,
		`SELECT kind, count(*) FROM events_%d WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 GROUP BY kind`,
		`UPDATE orders_%d SET status = $1 WHERE id = $2 AND tenant_id = $3`,
	}

	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf(shapes[i%len(shapes)], i))
	}

	return out
}

// BenchmarkParseUncached is the NFR-3 number: cost per statement once the module
// is up. CacheSize is 1 so every iteration is a genuine parse.
//
// It also reports the cold start, which is charged once per process and dwarfs
// everything else — the library compiles the whole libpg_query WASM module on
// first use. A first report that must not pay it should parse one statement at
// startup.
func BenchmarkParseUncached(b *testing.B) {
	p := New(Config{CacheSize: 1})
	statements := workload(512)

	start := time.Now()

	if _, err := p.Parse(statements[0]); err != nil {
		b.Fatalf("parse: %v", err)
	}

	b.Logf("cold start: %s", time.Since(start))

	b.ResetTimer()

	for i := range b.N {
		if _, err := p.Parse(statements[i%len(statements)]); err != nil {
			b.Fatalf("parse: %v", err)
		}
	}
}

// BenchmarkParseCached measures a repeat report: the pg_stat_statements top
// barely moves between polls, so most texts come back from the cache.
func BenchmarkParseCached(b *testing.B) {
	p := New(Config{})
	statements := workload(64)

	for _, sql := range statements {
		if _, err := p.Parse(sql); err != nil {
			b.Fatalf("warm up: %v", err)
		}
	}

	b.ResetTimer()

	for i := range b.N {
		if _, err := p.Parse(statements[i%len(statements)]); err != nil {
			b.Fatalf("parse: %v", err)
		}
	}
}
