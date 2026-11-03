//go:build integration

package metrics

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var exporterVersions = []string{"pg16", "pg17"}

type exporterScrape struct {
	file, instance, job string
	rewrite             func(line string) string // optional
}

var nodenameRe = regexp.MustCompile(`nodename="[^"]*"`)

// anchorScrapes hides the target in instance and names it only in the address
// labels, so nothing but the anchor lookup can find it.
func anchorScrapes(v string) []exporterScrape {
	server := func(from, to string) func(string) string {
		return func(l string) string {
			return strings.Replace(l, `server="`+from+`:5432"`, `server="`+to+`:5432"`, 1)
		}
	}

	return []exporterScrape{
		{"postgres_exporter_all_primary", "a-" + v + "-1:9187", "postgres", server("dasha-exp-primary", v+"-a-primary")},
		{"postgres_exporter_default_primary", "a-" + v + "-2:9187", "postgres_default", server("dasha-exp-primary", v+"-a-default")},
		{"postgres_exporter_all_replica", "a-" + v + "-3:9187", "postgres", server("dasha-exp-replica", v+"-a-replica")},
		{"node_exporter", "a-" + v + "-4:9100", "node", func(l string) string {
			return nodenameRe.ReplaceAllString(l, `nodename="`+v+`-a-primary"`)
		}},
		{"pgbouncer_exporter", "a-" + v + "-5:9127", "pgbouncer", func(l string) string {
			return strings.Replace(l, `host="dasha-exp-primary"`, `host="`+v+`-a-primary"`, 1)
		}},
	}
}

func anchorTestConfig() Config {
	c := Default()
	c.Enabled = true
	c.Datasource.URL = vmURL
	c.Providers = ProvidersConfig{Core: ProviderPostgresExporter, Pooler: ProviderPgBouncerExporter, Host: ProviderNodeExporter}

	return c
}

func anchorMeta(v string) fakeMeta {
	out := fakeMeta{}
	for _, name := range []string{"primary", "default", "replica"} {
		out[v+"-a/"+v+"-a-"+name] = DiscoveryMeta{Source: "static", Port: "5432"}
	}

	return out
}

func exporterScrapes(v string) []exporterScrape {
	return []exporterScrape{
		{"postgres_exporter_all_primary", v + "-primary:9187", "postgres", nil},
		{"postgres_exporter_default_primary", v + "-default:9187", "postgres_default", nil},
		{"postgres_exporter_all_replica", v + "-replica:9187", "postgres", nil},
		{"node_exporter", v + "-primary:9100", "node", nil},
		{"pgbouncer_exporter", v + "-primary:9127", "pgbouncer", nil},
	}
}

func exporterTestConfig(v string) Config {
	c := Default()
	c.Enabled = true
	c.Datasource.URL = vmURL
	c.Providers = ProvidersConfig{Core: ProviderPostgresExporter, Pooler: ProviderPgBouncerExporter, Host: ProviderNodeExporter}

	coreOnly := &ProvidersConfig{Pooler: ProviderNone, Host: ProviderNone}
	c.Targets = []TargetMapping{
		{Cluster: v, Instance: "primary", Host: v + "-primary"},
		{Cluster: v, Instance: "default", Host: v + "-default", Providers: coreOnly},
		{Cluster: v, Instance: "replica", Host: v + "-replica", Providers: coreOnly},
	}

	return c
}

func fixtureDir(v string) string {
	return filepath.Join("testdata", "exporters", v)
}

func fixtureTime(t *testing.T, dir string) time.Time {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, "timestamp.txt"))
	if err != nil {
		t.Fatal(err)
	}

	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}

	return ts
}

// promSamples returns the sample lines of an exposition file keyed by series.
func promSamples(t *testing.T, path string) map[string]string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	out := map[string]string{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if i := strings.LastIndexByte(line, ' '); i > 0 {
			out[line[:i]] = line
		}
	}

	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	return out
}

func importProm(t *testing.T, lines []string, s exporterScrape, at time.Time) {
	t.Helper()

	var sb strings.Builder

	for _, l := range lines {
		if s.rewrite != nil {
			l = s.rewrite(l)
		}

		fmt.Fprintf(&sb, "%s %d\n", l, at.UnixMilli())
	}

	q := url.Values{"extra_label": {"instance=" + s.instance, "job=" + s.job}}

	resp, err := http.Post(vmURL+"/api/v1/import/prometheus?"+q.Encode(), "text/plain", strings.NewReader(sb.String()))
	if err != nil {
		t.Fatalf("import %s: %v", s.file, err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		t.Fatalf("import %s returned %d", s.file, resp.StatusCode)
	}
}

// loadExporterFixtures imports t0 and t1 under each scrape's instance/job,
// keeping their interval with t1 a minute ago, and returns the t1 time.
func loadExporterFixtures(t *testing.T, v string, scrapes []exporterScrape) time.Time {
	t.Helper()

	dir := fixtureDir(v)
	interval := fixtureTime(t, filepath.Join(dir, "t1")).Sub(fixtureTime(t, filepath.Join(dir, "t0")))
	t1 := time.Now().Truncate(time.Second).Add(-time.Minute)
	t0 := t1.Add(-interval)

	for _, s := range scrapes {
		before := promSamples(t, filepath.Join(dir, "t0", s.file+".prom"))
		after := promSamples(t, filepath.Join(dir, "t1", s.file+".prom"))

		// A text import carries no staleness markers: a t0 series gone at t1
		// would still answer at t1, so only t0 series that survive are loaded.
		var old []string

		for k, line := range before {
			if _, ok := after[k]; ok {
				old = append(old, line)
			}
		}

		importProm(t, old, s, t0)
		importProm(t, slices.Collect(maps.Values(after)), s, t1)
	}

	resp, err := http.Get(vmURL + "/internal/force_flush")
	if err != nil {
		t.Fatalf("force_flush: %v", err)
	}

	_ = resp.Body.Close()

	return t1
}

func loadExpected(t *testing.T, v string) map[string]map[SignalKind]float64 {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(fixtureDir(v), "expected.json"))
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]map[SignalKind]float64
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func exporterSignals(cat *QueryCatalog) []SignalKind {
	seen := map[SignalKind]bool{}

	var out []SignalKind

	for k := range cat.templates {
		if isExporterProvider(k.provider) && !seen[k.signal] {
			seen[k.signal] = true
			out = append(out, k.signal)
		}
	}

	return out
}

func closeTo(got, want float64) bool {
	return math.Abs(got-want) <= 1e-6*math.Max(1, math.Abs(want))
}

func TestExporters(t *testing.T) {
	ctx := context.Background()

	for _, v := range exporterVersions {
		t.Run(v, func(t *testing.T) {
			t1 := loadExporterFixtures(t, v, exporterScrapes(v))

			cfg := exporterTestConfig(v)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			m, err := NewMatcher(cfg, nil)
			if err != nil {
				t.Fatalf("NewMatcher: %v", err)
			}

			client := NewVMClient(DatasourceConfig{URL: vmURL}, nil)
			cat := NewQueryCatalog()
			co := NewCollector(m, cat, client, "5m", "", BatchLimits{}, nil)

			expected := loadExpected(t, v)

			targets := make([]TargetRef, 0, len(expected))
			for name := range expected {
				targets = append(targets, TargetRef{Cluster: v, Instance: name})
			}

			got, errs := co.InstantMany(ctx, targets, t1, exporterSignals(cat)...)

			for _, ref := range targets {
				t.Run(ref.Instance, func(t *testing.T) {
					if err := errs[ref]; err != nil {
						t.Fatalf("InstantMany: %v", err)
					}

					checkSignals(t, got[ref], expected[ref.Instance])
				})
			}

			t.Run("replica has no replica count", func(t *testing.T) {
				raw := got[TargetRef{Cluster: v, Instance: "replica"}].ToRawMetrics()
				if raw.ReplicaCount != 0 {
					t.Errorf("ReplicaCount = %d, want 0", raw.ReplicaCount)
				}
			})

			t.Run("validate", func(t *testing.T) {
				for _, ref := range targets {
					diag, err := m.Validate(ctx, client, ref.Cluster, ref.Instance)
					if err != nil {
						t.Fatalf("Validate %s: %v", ref.Instance, err)
					}

					wantRoles := 1
					if ref.Instance == "primary" {
						wantRoles = 3
					}

					if len(diag.Roles) != wantRoles {
						t.Errorf("%s: %d roles, want %d", ref.Instance, len(diag.Roles), wantRoles)
					}

					for _, rd := range diag.Roles {
						if !rd.OK || rd.Match != MatchTemplate {
							t.Errorf("%s %s/%s: match %s, matched %d via %s{%s}: %s",
								ref.Instance, rd.Role, rd.Provider, rd.Match, rd.Matched, rd.Metric, rd.Selector, rd.Err)
						}
					}
				}
			})
		})
	}
}

func checkSignals(t *testing.T, s Signals, want map[SignalKind]float64) {
	t.Helper()

	for sig, w := range want {
		g, ok := s.Get(sig)
		if !ok {
			t.Errorf("%s: absent, want %v", sig, w)
		} else if !closeTo(g, w) {
			t.Errorf("%s = %v, want %v", sig, g, w)
		}
	}

	for sig := range s.Have {
		if _, ok := want[sig]; !ok {
			t.Errorf("%s = %v, want absent", sig, s.Value[sig])
		}
	}
}

func TestExportersAnchor(t *testing.T) {
	ctx := context.Background()

	for _, v := range exporterVersions {
		t.Run(v, func(t *testing.T) {
			t1 := loadExporterFixtures(t, v, anchorScrapes(v))

			m, err := NewMatcher(anchorTestConfig(), anchorMeta(v))
			if err != nil {
				t.Fatalf("NewMatcher: %v", err)
			}

			client := NewVMClient(DatasourceConfig{URL: vmURL}, nil)
			cat := NewQueryCatalog()
			co := NewCollector(m, cat, client, "5m", "", BatchLimits{}, nil)

			expected := loadExpected(t, v)

			for name, want := range expected {
				ref := TargetRef{Cluster: v + "-a", Instance: v + "-a-" + name}

				t.Run(name, func(t *testing.T) {
					got, errs := co.InstantMany(ctx, []TargetRef{ref}, t1, exporterSignals(cat)...)
					if err := errs[ref]; err != nil {
						t.Fatalf("InstantMany: %v", err)
					}

					checkSignals(t, got[ref], want)

					diag, err := m.Validate(ctx, client, ref.Cluster, ref.Instance)
					if err != nil {
						t.Fatalf("Validate: %v", err)
					}

					for _, rd := range diag.Roles {
						if rd.Role != RoleCore && name != "primary" {
							continue
						}

						if !rd.OK || rd.Match != MatchAnchor {
							t.Errorf("%s/%s: match %s, matched %d via %s{%s}: %s",
								rd.Role, rd.Provider, rd.Match, rd.Matched, rd.Metric, rd.Selector, rd.Err)
						}
					}
				})
			}
		})
	}
}
