package metrics

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type gluedTerm struct {
	expr  string
	idx   int
	glued bool
}

func (t gluedTerm) labels() map[string]string {
	if !t.glued {
		return map[string]string{}
	}

	return map[string]string{batchLabel: strconv.Itoa(t.idx)}
}

// splitGlued inverts batcher.pack for one request.
func splitGlued(q string) []gluedTerm {
	const prefix = "label_replace("

	if !strings.HasPrefix(q, prefix) || !strings.Contains(q, `"`+batchLabel+`"`) {
		return []gluedTerm{{expr: q}}
	}

	var out []gluedTerm

	rest := q
	for i := 0; rest != ""; i++ {
		rest = strings.TrimPrefix(strings.TrimPrefix(rest, " or "), prefix)
		suffix := `, "` + batchLabel + `", "` + strconv.Itoa(i) + `", "", "")`

		end := strings.Index(rest, suffix)
		if end < 0 {
			return out
		}

		out = append(out, gluedTerm{expr: rest[:end], idx: i, glued: true})
		rest = rest[end+len(suffix):]
	}

	return out
}

// exprClient answers each term of a query by its expression and records requests.
type exprClient struct {
	mu      sync.Mutex
	queries []string
	value   func(expr string) (float64, bool)
	fail    func(query string) error
}

func (c *exprClient) record(ctx context.Context, q string) error {
	c.mu.Lock()
	c.queries = append(c.queries, q)
	c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if c.fail != nil {
		return c.fail(q)
	}

	return nil
}

func (c *exprClient) terms() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out []string

	for _, q := range c.queries {
		for _, t := range splitGlued(q) {
			out = append(out, t.expr)
		}
	}

	return out
}

func (c *exprClient) QueryInstant(ctx context.Context, q string, _ time.Time) ([]Sample, error) {
	if err := c.record(ctx, q); err != nil {
		return nil, err
	}

	var out []Sample

	for _, t := range splitGlued(q) {
		if v, ok := c.value(t.expr); ok {
			out = append(out, Sample{Labels: t.labels(), Value: v})
		}
	}

	return out, nil
}

func (c *exprClient) QueryRange(ctx context.Context, q string, _ Range) ([]Series, error) {
	if err := c.record(ctx, q); err != nil {
		return nil, err
	}

	var out []Series

	for _, t := range splitGlued(q) {
		if v, ok := c.value(t.expr); ok {
			out = append(out, Series{Labels: t.labels(), Points: []SeriesPoint{{Time: time.Unix(100, 0), Value: v}}})
		}
	}

	return out, nil
}

func constValue(v float64) func(string) (float64, bool) {
	return func(string) (float64, bool) { return v, true }
}

func sizedItems(n, size int) []batchItem {
	items := make([]batchItem, n)

	for i := range items {
		id := strconv.Itoa(i)
		items[i] = batchItem{
			Expr: "m" + id + "_" + strings.Repeat("x", size-len(id)-2),
			Keys: []batchKey{{Target: TargetRef{Cluster: "c", Instance: id}, Signal: SigTotalConns}},
		}
	}

	return items
}

func valueByPrefix(expr string) (float64, bool) {
	id, _, _ := strings.Cut(strings.TrimPrefix(expr, "m"), "_")
	v, err := strconv.Atoi(id)

	return float64(v), err == nil
}

func TestBatcher_PacksWithinLimit(t *testing.T) {
	client := &exprClient{value: valueByPrefix}
	b := &batcher{client: client, maxBytes: 1024, maxConc: 2}

	vals, fails := b.instant(context.Background(), sizedItems(10, 280), time.Now())
	if len(fails) != 0 {
		t.Fatalf("unexpected failures: %+v", fails)
	}

	if len(client.queries) < 2 || len(client.queries) >= 10 {
		t.Errorf("want several glued requests, got %d", len(client.queries))
	}

	for _, q := range client.queries {
		if len(q) > 1024 {
			t.Errorf("request of %d bytes exceeds the limit", len(q))
		}
	}

	for i := range 10 {
		k := batchKey{Target: TargetRef{Cluster: "c", Instance: strconv.Itoa(i)}, Signal: SigTotalConns}
		if v, ok := vals[k]; !ok || v != float64(i) {
			t.Errorf("key %d: want %d, got %v ok=%v", i, i, v, ok)
		}
	}
}

func TestBatcher_SharedExprAnswersAllKeys(t *testing.T) {
	client := &exprClient{value: constValue(7)}
	b := &batcher{client: client, maxBytes: 1024, maxConc: 1}

	a := batchKey{Target: TargetRef{Cluster: "c", Instance: "a"}, Signal: SigLoadAvg15}
	c := batchKey{Target: TargetRef{Cluster: "c", Instance: "b"}, Signal: SigLoadAvg15}

	vals, _ := b.instant(context.Background(), []batchItem{{Expr: "max(n)", Keys: []batchKey{a, c}}}, time.Now())

	if vals[a] != 7 || vals[c] != 7 {
		t.Errorf("both keys want 7, got %v", vals)
	}
}

func TestBatcher_OversizedExprGoesBare(t *testing.T) {
	client := &exprClient{value: valueByPrefix}
	b := &batcher{client: client, maxBytes: 1024, maxConc: 1}

	items := sizedItems(1, 1100)

	vals, fails := b.instant(context.Background(), items, time.Now())
	if len(fails) != 0 {
		t.Fatalf("unexpected failures: %+v", fails)
	}

	if len(client.queries) != 1 || client.queries[0] != items[0].Expr {
		t.Fatalf("want the bare expression sent alone, got %d requests", len(client.queries))
	}

	if _, ok := vals[items[0].Keys[0]]; !ok {
		t.Error("bare request result not mapped to its key")
	}
}

func TestBatcher_PartialFailure(t *testing.T) {
	boom := errors.New("boom")
	client := &exprClient{
		value: valueByPrefix,
		fail: func(q string) error {
			if strings.Contains(q, "m3_") {
				return boom
			}

			return nil
		},
	}
	b := &batcher{client: client, maxBytes: 1024, maxConc: 4}

	items := sizedItems(5, 600)

	vals, fails := b.instant(context.Background(), items, time.Now())

	if len(fails) != 1 || !errors.Is(fails[0].Err, boom) {
		t.Fatalf("want one failure with boom, got %+v", fails)
	}

	if len(fails[0].Keys) != 1 || fails[0].Keys[0] != items[3].Keys[0] {
		t.Errorf("failure keys: want item 3 only, got %+v", fails[0].Keys)
	}

	if len(vals) != 4 {
		t.Errorf("want 4 answered keys, got %d", len(vals))
	}
}

func twoHostConfig() Config {
	c := testConfig()
	c.Targets = append(c.Targets, TargetMapping{
		Cluster: "prod-mdb", Instance: "rc1b-def.mdb.yandexcloud.net",
		Env: "dev", Service: "my_cluster",
		Host: "rc1b-def.mdb.yandexcloud.net", Container: "rc1b-def",
	})

	return c
}

func TestCollector_InstantManySharesClusterExpr(t *testing.T) {
	m, err := NewMatcher(twoHostConfig(), nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	client := &exprClient{value: constValue(3)}
	co := NewCollector(m, NewQueryCatalog(), client, "5m", "", BatchLimits{}, nil)

	a := TargetRef{Cluster: "prod-mdb", Instance: "rc1a-abc.mdb.yandexcloud.net"}
	b := TargetRef{Cluster: "prod-mdb", Instance: "rc1b-def.mdb.yandexcloud.net"}

	out, errs := co.InstantMany(context.Background(), []TargetRef{a, b}, time.Now(), SigLoadAvg15, SigXactsLeftWrap)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	if len(client.queries) != 1 {
		t.Errorf("want one request, got %d", len(client.queries))
	}

	var load, xacts int

	for _, e := range client.terms() {
		switch {
		case strings.Contains(e, "load_avg_15min"):
			load++
		case strings.Contains(e, "xacts_left"):
			xacts++
		}
	}

	if load != 1 || xacts != 2 {
		t.Errorf("want 1 host-level and 2 per-instance terms, got load=%d xacts=%d", load, xacts)
	}

	for _, tr := range []TargetRef{a, b} {
		if !out[tr].Has(SigLoadAvg15) || !out[tr].Has(SigXactsLeftWrap) {
			t.Errorf("%s: missing signals %+v", tr.Instance, out[tr].Have)
		}
	}
}

func TestCollector_InstantManyUnmappedTarget(t *testing.T) {
	co := newTestCollector(t, 1)

	good := TargetRef{Cluster: "prod-mdb", Instance: "rc1a-abc.mdb.yandexcloud.net"}
	bad := TargetRef{Cluster: "nope", Instance: "nope"}

	out, errs := co.InstantMany(context.Background(), []TargetRef{good, bad}, time.Now(), SigXactsLeftWrap)

	if !errors.Is(errs[bad], ErrTargetNotMapped) {
		t.Errorf("want ErrTargetNotMapped for the unmapped target, got %v", errs[bad])
	}

	if _, ok := out[bad]; ok {
		t.Error("unmapped target must not be in the result")
	}

	if !out[good].Has(SigXactsLeftWrap) {
		t.Error("mapped target lost its signal")
	}
}
