package sqlparse

import (
	"strings"
	"sync"
	"testing"
)

func TestParseRejectsTextThatCannotYieldCandidates(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		cfg  Config
		code string
	}{
		{
			name: "empty",
			sql:  "   \n\t ",
			code: ReasonEmpty,
		},
		{
			name: "hidden by pg_stat_statements",
			sql:  insufficientPrivilege,
			code: ReasonInsufficientPrivilege,
		},
		{
			// A clipped statement often parses — into half a WHERE clause, which
			// would produce a confidently wrong index.
			name: "clipped by track_activity_query_size",
			sql:  `SELECT * FROM orders WHERE tenant_id = $1 AND created_at > $2 ORDER BY...`,
			code: ReasonTruncated,
		},
		{
			name: "over the size limit",
			sql:  `SELECT * FROM orders WHERE id = $1`,
			cfg:  Config{MaxQueryBytes: 10},
			code: ReasonTooLong,
		},
		{
			name: "not sql at all",
			sql:  `SELEKT * FRUM orders`,
			code: ReasonParseError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg).Parse(tc.sql)
			if err == nil {
				t.Fatal("expected an error")
			}

			if got := ReasonOf(err); got != tc.code {
				t.Errorf("reason = %q, want %q (%v)", got, tc.code, err)
			}
		})
	}
}

// A clipped statement must be reported as clipped even when the tail happens to
// leave valid SQL behind — the mark is the only evidence there is.
func TestParseTreatsTruncationMarkAsTruncated(t *testing.T) {
	_, err := New(Config{}).Parse(`SELECT * FROM orders WHERE tenant_id = $1...`)
	if err == nil {
		t.Fatal("expected an error")
	}

	if got := ReasonOf(err); got != ReasonTruncated {
		t.Errorf("reason = %q, want %q", got, ReasonTruncated)
	}
}

// FR-2.1: statements differing only in constants and formatting collapse into one
// workload entry, which is also what removes the pgpro_stats planid duplicates.
func TestFingerprintIgnoresConstantsAndFormatting(t *testing.T) {
	p := New(Config{})

	same := []string{
		`SELECT * FROM orders WHERE id = 1`,
		`select  *  from orders where id = 42`,
		"SELECT *\nFROM orders\nWHERE id = $1",
	}

	first, err := p.Fingerprint(same[0])
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	for _, sql := range same[1:] {
		fp, err := p.Fingerprint(sql)
		if err != nil {
			t.Fatalf("fingerprint %q: %v", sql, err)
		}

		if fp != first {
			t.Errorf("fingerprint(%q) = %q, want %q", sql, fp, first)
		}
	}

	other, err := p.Fingerprint(`SELECT * FROM orders WHERE tenant_id = $1`)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	if other == first {
		t.Error("different statements share a fingerprint")
	}
}

func TestParseIsCached(t *testing.T) {
	const sql = `SELECT * FROM orders WHERE tenant_id = $1`

	p := New(Config{})

	first, err := p.Parse(sql)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	second, err := p.Parse(sql)
	if err != nil {
		t.Fatalf("parse again: %v", err)
	}

	if first.Fingerprint != second.Fingerprint || len(first.Usages) != len(second.Usages) {
		t.Errorf("cached statement differs: %+v vs %+v", first, second)
	}

	impl, ok := p.(*parser)
	if !ok {
		t.Fatalf("unexpected parser type %T", p)
	}

	if got := impl.cache.order.Len(); got != 1 {
		t.Errorf("cache holds %d entries, want 1", got)
	}
}

// Failures are cached too: a clipped statement stays clipped, and re-parsing it
// on every poll would burn WASM time for the same answer.
func TestParseCachesFailures(t *testing.T) {
	p := New(Config{})

	if _, err := p.Parse(`SELECT * FROM orders WHERE id = ...`); err == nil {
		t.Fatal("expected an error")
	}

	if _, err := p.Parse(`SELECT * FROM orders WHERE id = ...`); err == nil {
		t.Fatal("expected an error from the cache")
	}

	impl, ok := p.(*parser)
	if !ok {
		t.Fatalf("unexpected parser type %T", p)
	}

	if got := impl.cache.order.Len(); got != 1 {
		t.Errorf("cache holds %d entries, want 1", got)
	}
}

// NFR-3/R-6: a report parses its whole top at once, so the parser is used from
// several goroutines. Run with -race.
//
// MaxParallel stays at 2: every slot beyond the first makes the library compile
// another copy of the libpg_query module, which costs seconds of test time and
// proves nothing extra about the locking here.
func TestParseIsConcurrencySafe(t *testing.T) {
	p := New(Config{CacheSize: 4, MaxParallel: 2})

	statements := []string{
		`SELECT * FROM orders WHERE tenant_id = $1`,
		`SELECT * FROM customers WHERE country = $1 ORDER BY created_at DESC`,
		`UPDATE orders SET status = $1 WHERE id = $2`,
		`DELETE FROM sessions WHERE expires_at < $1`,
		`SELECT * FROM orders o JOIN customers c ON c.id = o.customer_id WHERE c.status = $1`,
		`SELEKT nonsense`,
	}

	var wg sync.WaitGroup

	for range 8 {
		for _, sql := range statements {
			wg.Add(1)

			go func(sql string) {
				defer wg.Done()

				if _, err := p.Parse(sql); err != nil && !strings.Contains(sql, "SELEKT") {
					t.Errorf("parse %q: %v", sql, err)
				}
			}(sql)
		}
	}

	wg.Wait()
}

func TestLRUEvictsOldestAndPromotesOnUse(t *testing.T) {
	cache := newLRU(2)
	keys := [3]cacheKey{{1}, {2}, {3}}

	cache.put(keys[0], Statement{Kind: KindSelect}, nil)
	cache.put(keys[1], Statement{Kind: KindUpdate}, nil)

	if _, ok := cache.get(keys[0]); !ok {
		t.Fatal("first key evicted too early")
	}

	// keys[0] was just used, so keys[1] is the one that must go.
	cache.put(keys[2], Statement{Kind: KindDelete}, nil)

	if _, ok := cache.get(keys[1]); ok {
		t.Error("least recently used key survived")
	}

	if _, ok := cache.get(keys[0]); !ok {
		t.Error("recently used key was evicted")
	}

	if got := cache.order.Len(); got != 2 {
		t.Errorf("cache holds %d entries, want 2", got)
	}
}
