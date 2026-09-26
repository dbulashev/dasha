//go:build integration

package metrics

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var vmURL string

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        vmImage(),
			ExposedPorts: []string{"8428/tcp"},
			Cmd:          []string{"-retentionPeriod=100y", "-search.latencyOffset=0s", "-search.disableCache"},
			WaitingFor: wait.ForHTTP("/health").
				WithPort("8428/tcp").
				WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("start victoriametrics: %v", err))
	}

	host, err := container.Host(ctx)
	if err != nil {
		panic(err)
	}

	port, err := container.MappedPort(ctx, "8428/tcp")
	if err != nil {
		panic(err)
	}

	vmURL = fmt.Sprintf("http://%s:%s", host, port.Port())

	code := m.Run()

	_ = container.Terminate(ctx)

	os.Exit(code)
}

func vmImage() string {
	if v := os.Getenv("VICTORIAMETRICS_IMAGE"); v != "" {
		return v
	}

	return "victoriametrics/victoria-metrics:v1.110.0"
}

const parityExclude = `,query_type!~"postgres_exporter"`

var (
	selectorRe = regexp.MustCompile(`([a-zA-Z_:][a-zA-Z0-9_:]*)\{([^}]*)\}`)
	matcherRe  = regexp.MustCompile(`(\w+)\s*(=~|!~|!=|=)\s*"([^"]*)"`)
)

func parityConfig() Config {
	c := Default()
	c.Enabled = true
	c.Datasource.URL = "unused"
	c.Targets = []TargetMapping{
		{Cluster: "a", Instance: "a1.example", Env: "dev", Service: "svc_a", Host: "a1.example", Container: "a1"},
		{Cluster: "a", Instance: "a2.example", Env: "dev", Service: "svc_a", Host: "a2.example", Container: "a2"},
		{Cluster: "b", Instance: "b1.example", Env: "dev", Service: "svc_b", Host: "b1.example", Container: "b1"},
	}

	return c
}

// positiveLabels turns `k="v"` / `k=~"v|w"` matchers into concrete labels.
func positiveLabels(s string) map[string]string {
	out := map[string]string{}

	for _, m := range matcherRe.FindAllStringSubmatch(s, -1) {
		switch m[2] {
		case "=":
			out[m[1]] = m[3]
		case "=~":
			out[m[1]], _, _ = strings.Cut(m[3], "|")
		}
	}

	return out
}

type parityPair struct {
	key  batchKey
	expr string
}

func parityPairs(t *testing.T) []parityPair {
	t.Helper()

	cfg := parityConfig()

	m, err := NewMatcher(cfg, nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	cat := NewQueryCatalog()

	var out []parityPair

	for _, tm := range cfg.Targets {
		rt, err := m.Resolve(tm.Cluster, tm.Instance)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		for ck := range cat.templates {
			sel, err := m.Selector(ck.provider, signalRole(ck.signal), rt)
			if err != nil {
				continue
			}

			expr, _ := cat.Expr(ck.provider, ck.signal, sel, "5m", parityExclude)
			key := batchKey{
				Target: TargetRef{Cluster: tm.Cluster, Instance: tm.Instance},
				Signal: SignalKind(string(ck.provider) + "/" + string(ck.signal)),
			}
			out = append(out, parityPair{key: key, expr: expr})
		}
	}

	return out
}

// seedParity writes rising series for every metric the pairs select, over the
// 15 minutes before end.
func seedParity(t *testing.T, pairs []parityPair, end time.Time) {
	t.Helper()

	seen := map[string]bool{}

	var buf bytes.Buffer

	for _, p := range pairs {
		for _, sm := range selectorRe.FindAllStringSubmatch(p.expr, -1) {
			labels := positiveLabels(sm[2])
			labels["__name__"] = sm[1]

			keys := make([]string, 0, len(labels))
			for k := range labels {
				if k != "__name__" {
					keys = append(keys, k)
				}
			}

			slices.Sort(keys)

			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
			}

			series := sm[1] + "{" + strings.Join(parts, ",") + "}"
			if seen[series] {
				continue
			}

			seen[series] = true

			base := float64(len(seen) * 10)
			inc := float64(1 + len(seen)%3)

			for i := range 31 {
				ts := end.Add(-time.Duration(30-i) * 30 * time.Second)
				fmt.Fprintf(&buf, "%s %g %d\n", series, base+float64(i)*inc, ts.UnixMilli())
			}
		}
	}

	resp, err := http.Post(vmURL+"/api/v1/import/prometheus", "text/plain", &buf)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		t.Fatalf("import returned %d", resp.StatusCode)
	}

	resp, err = http.Get(vmURL + "/internal/force_flush")
	if err != nil {
		t.Fatalf("force_flush: %v", err)
	}

	_ = resp.Body.Close()
}

func sameValue(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}

	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(a))
}

func parityItems(pairs []parityPair) []batchItem {
	byExpr := map[string]int{}

	var items []batchItem

	for _, p := range pairs {
		if i, ok := byExpr[p.expr]; ok {
			items[i].Keys = append(items[i].Keys, p.key)

			continue
		}

		byExpr[p.expr] = len(items)
		items = append(items, batchItem{Expr: p.expr, Keys: []batchKey{p.key}})
	}

	return items
}

func TestBatchParity(t *testing.T) {
	ctx := context.Background()
	end := time.Now().Truncate(time.Second).Add(-time.Minute)

	pairs := parityPairs(t)
	seedParity(t, pairs, end)

	client := NewVMClient(DatasourceConfig{URL: vmURL}, nil)
	b := newBatcher(client, 2000, 4)

	t.Run("instant", func(t *testing.T) {
		glued, fails := b.instant(ctx, parityItems(pairs), end)
		for _, f := range fails {
			t.Errorf("glued request failed for %d keys: %v", len(f.Keys), f.Err)
		}

		present := 0

		for _, p := range pairs {
			samples, err := client.QueryInstant(ctx, p.expr, end)
			if err != nil {
				t.Errorf("%s %s: %v", p.key.Target.Instance, p.key.Signal, err)

				continue
			}

			gv, gok := glued[p.key]

			if len(samples) == 0 {
				if gok {
					t.Errorf("%s %s: glued has %v, single has nothing", p.key.Target.Instance, p.key.Signal, gv)
				}

				continue
			}

			present++

			if !gok || !sameValue(gv, samples[0].Value) {
				t.Errorf("%s %s: single %v, glued %v (present=%v)", p.key.Target.Instance, p.key.Signal, samples[0].Value, gv, gok)
			}
		}

		if present*2 < len(pairs) {
			t.Errorf("seed too thin: %d of %d pairs have a value", present, len(pairs))
		}
	})

	t.Run("range", func(t *testing.T) {
		r := Range{Start: end.Add(-5 * time.Minute), End: end, Step: time.Minute}

		glued, fails := b.rangeQ(ctx, parityItems(pairs), r)
		for _, f := range fails {
			t.Errorf("glued request failed for %d keys: %v", len(f.Keys), f.Err)
		}

		for _, p := range pairs {
			series, err := client.QueryRange(ctx, p.expr, r)
			if err != nil {
				t.Errorf("%s %s: %v", p.key.Target.Instance, p.key.Signal, err)

				continue
			}

			gp := glued[p.key]

			var sp []SeriesPoint
			if len(series) > 0 {
				sp = series[0].Points
			}

			if len(gp) != len(sp) {
				t.Errorf("%s %s: single %d points, glued %d", p.key.Target.Instance, p.key.Signal, len(sp), len(gp))

				continue
			}

			for i := range sp {
				if !sp[i].Time.Equal(gp[i].Time) || !sameValue(sp[i].Value, gp[i].Value) {
					t.Errorf("%s %s: point %d single %+v, glued %+v", p.key.Target.Instance, p.key.Signal, i, sp[i], gp[i])

					break
				}
			}
		}
	})
}
