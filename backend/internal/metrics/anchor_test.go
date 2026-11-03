package metrics

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHostMatches(t *testing.T) {
	fqdn := ResolvedTarget{Host: "db1.prod.example", Instance: "db1.prod.example", Container: "db1", Port: "5432"}
	patroni := ResolvedTarget{Host: "10.0.0.7", Instance: "pg-a", Container: "pg-node-1"}

	cases := []struct {
		name string
		host string
		rt   ResolvedTarget
		want bool
	}{
		{"fqdn", "db1.prod.example", fqdn, true},
		{"case", "DB1.Prod.Example", fqdn, true},
		{"short reported", "db1", fqdn, true},
		{"fqdn reported, short known", "db1.prod.example", ResolvedTarget{Host: "db1"}, true},
		{"other domain", "db1.stage.example", fqdn, false},
		{"ip", "10.0.0.7", patroni, true},
		{"ip prefix is not a match", "10.0.0.70", patroni, false},
		{"patroni member", "pg-node-1", patroni, true},
		{"patroni member fqdn", "pg-node-1.dc.local", patroni, true},
		{"patroni member fqdn, fqdn instance", "pg-node-1.dc.local", ResolvedTarget{Host: "10.0.0.7", Instance: "pg-a.dc.local", Container: "pg-node-1"}, true},
		{"derived container is not an alias", "db1.stage.example", ResolvedTarget{Host: "db1.prod.example", Instance: "db1.prod.example", Container: "db1"}, false},
		{"empty", "", fqdn, false},
	}

	for _, tc := range cases {
		if got := hostMatches(tc.host, tc.rt); got != tc.want {
			t.Errorf("%s: hostMatches(%q) = %v, want %v", tc.name, tc.host, got, tc.want)
		}
	}
}

func TestScrapeSelector_Port(t *testing.T) {
	spec := anchorSpecs[ProviderPostgresExporter]
	rt := ResolvedTarget{Host: "db1.example.com", Port: "6432"}

	cases := []struct {
		name   string
		series []map[string]string
		want   string
	}{
		{"single scrape, other port", []map[string]string{
			pgStatic("pgexp:9187", "pg", "db1.example.com:5432"),
		}, `instance="pgexp:9187",job="pg"`},
		{"two scrapes, port picks one", []map[string]string{
			pgStatic("pgexp-a:9187", "pg", "db1.example.com:5432"),
			pgStatic("pgexp-b:9187", "pg", "db1.example.com:6432"),
		}, `instance="pgexp-b:9187",job="pg"`},
		{"two scrapes, no port match", []map[string]string{
			pgStatic("pgexp-a:9187", "pg", "db1.example.com:5432"),
			pgStatic("pgexp-b:9187", "pg", "db1.example.com:5433"),
		}, ""},
	}

	for _, tc := range cases {
		samples := make([]Sample, 0, len(tc.series))
		for _, l := range tc.series {
			samples = append(samples, Sample{Labels: l, Value: 1})
		}

		if got := scrapeSelector(samples, spec, rt); got != tc.want {
			t.Errorf("%s: scrapeSelector = %q, want %q", tc.name, got, tc.want)
		}
	}
}

type anchorClient struct {
	mu      sync.Mutex
	queries map[string]int
	series  map[string][]map[string]string // metric in the anchor query -> series labels
	err     error
}

func (c *anchorClient) QueryInstant(_ context.Context, q string, _ time.Time) ([]Sample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.queries == nil {
		c.queries = map[string]int{}
	}

	c.queries[q]++

	if c.err != nil {
		return nil, c.err
	}

	for metric, labels := range c.series {
		if strings.Contains(q, "("+metric) {
			out := make([]Sample, 0, len(labels))
			for _, l := range labels {
				out = append(out, Sample{Labels: l, Value: 1})
			}

			return out, nil
		}
	}

	return nil, nil
}

func (c *anchorClient) QueryRange(context.Context, string, Range) ([]Series, error) {
	return nil, nil
}

func (c *anchorClient) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0
	for _, v := range c.queries {
		n += v
	}

	return n
}

func pgStatic(instance, job, server string) map[string]string {
	return map[string]string{"instance": instance, "job": job, "server": server}
}

func TestResolveCtx_Anchor(t *testing.T) {
	cases := []struct {
		name   string
		series []map[string]string
		want   string
	}{
		{"none", []map[string]string{pgStatic("pgexp:9187", "pg", "db9.example.com:5432")}, ""},
		{"one", []map[string]string{
			pgStatic("pgexp-1:9187", "pg", "db1.example.com:5432"),
			pgStatic("pgexp-2:9187", "pg", "db2.example.com:5432"),
		}, `instance="pgexp-1:9187",job="pg"`},
		{"two exporters on one server", []map[string]string{
			pgStatic("pgexp-a:9187", "all", "db1.example.com:5432"),
			pgStatic("pgexp-b:9187", "default", "db1.example.com:5432"),
		}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := NewMatcher(exporterConfig(), nil)
			c := &anchorClient{series: map[string][]map[string]string{"pg_static": tc.series}}

			rt, err := m.ResolveCtx(context.Background(), c, "self", "db1", false)
			if err != nil {
				t.Fatalf("ResolveCtx: %v", err)
			}

			if got := rt.Scrape[RoleCore]; got != tc.want {
				t.Errorf("Scrape[core] = %q, want %q", got, tc.want)
			}

			sel, _ := m.Selector(ProviderPostgresExporter, RoleCore, rt)
			if tc.want == "" && sel != `instance=~"db1\\.example\\.com(:[0-9]+)?"` {
				t.Errorf("selector = %s, want the fallback template", sel)
			} else if tc.want != "" && sel != tc.want {
				t.Errorf("selector = %s, want %s", sel, tc.want)
			}
		})
	}
}

func TestResolveCtx_ExplicitTemplateSkipsLookup(t *testing.T) {
	c := exporterConfig()
	c.Selectors = map[string]string{
		SelectorPostgresExporter:  `job="pg",instance="{{.HostShort}}:9187"`,
		SelectorNodeExporter:      `job="node",instance="{{.HostShort}}:9100"`,
		SelectorPgBouncerExporter: `job="pgb",instance="{{.HostShort}}:9127"`,
	}

	m, _ := NewMatcher(c.WithDefaults(), nil)
	client := &anchorClient{}

	if _, err := m.ResolveCtx(context.Background(), client, "self", "db1", false); err != nil {
		t.Fatalf("ResolveCtx: %v", err)
	}

	if n := client.total(); n != 0 {
		t.Errorf("%d anchor queries with explicit templates, want 0", n)
	}
}

func TestResolveCtx_Cache(t *testing.T) {
	ctx := context.Background()
	m, _ := NewMatcher(exporterConfig(), nil)

	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	client := &anchorClient{series: map[string][]map[string]string{
		"pg_static": {pgStatic("pgexp:9187", "pg", "db1.example.com:5432")},
	}}

	resolve := func(fresh bool) {
		t.Helper()

		if _, err := m.ResolveCtx(ctx, client, "self", "db1", fresh); err != nil {
			t.Fatalf("ResolveCtx: %v", err)
		}
	}

	resolve(false)

	first := client.total()
	if first != 3 {
		t.Fatalf("%d anchor queries on a cold cache, want one per role (3)", first)
	}

	resolve(false)

	if n := client.total(); n != first {
		t.Errorf("warm cache issued %d more queries", n-first)
	}

	resolve(true)

	if n := client.total(); n != 2*first {
		t.Errorf("fresh issued %d queries, want %d", n-first, first)
	}

	now = now.Add(anchorTTL)
	resolve(false)

	if n := client.total(); n != 3*first {
		t.Errorf("expired cache issued %d queries, want %d", n-2*first, first)
	}
}

func TestResolveCtx_ErrorCachedBriefly(t *testing.T) {
	ctx := context.Background()
	m, _ := NewMatcher(exporterConfig(), nil)
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }
	client := &anchorClient{err: errors.New("boom")}

	rt, err := m.ResolveCtx(ctx, client, "self", "db1", false)
	if err != nil {
		t.Fatalf("ResolveCtx: %v", err)
	}

	if len(rt.Scrape) != 0 {
		t.Errorf("Scrape = %v after a failed lookup, want empty", rt.Scrape)
	}

	q := anchorSpecs[ProviderPostgresExporter].query

	client.err = nil
	client.series = map[string][]map[string]string{"pg_static": {pgStatic("pgexp:9187", "pg", "db1.example.com:5432")}}

	rt, _ = m.ResolveCtx(ctx, client, "self", "db1", false)
	if len(rt.Scrape) != 0 || client.queries[q] != 1 {
		t.Errorf("failure not cached: Scrape = %v, queries = %d", rt.Scrape, client.queries[q])
	}

	rt, _ = m.ResolveCtx(ctx, client, "self", "db1", true)
	if rt.Scrape[RoleCore] == "" {
		t.Error("fresh lookup did not bypass the cached failure")
	}

	client.err = errors.New("boom")
	_, _ = m.ResolveCtx(ctx, client, "self", "db1", true)
	client.err = nil

	rt, _ = m.ResolveCtx(ctx, client, "self", "db1", false)
	if rt.Scrape[RoleCore] == "" {
		t.Error("failed fresh lookup overwrote the cached anchor")
	}

	m.anchors = map[Provider]anchorSnap{}
	client.err = errors.New("boom")
	_, _ = m.ResolveCtx(ctx, client, "self", "db1", false)
	client.err = nil
	now = now.Add(anchorErrorTTL)

	rt, _ = m.ResolveCtx(ctx, client, "self", "db1", false)
	if rt.Scrape[RoleCore] == "" {
		t.Error("lookup not retried after anchorErrorTTL")
	}
}

func TestResolveCtx_CallerCancelNotCached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m, _ := NewMatcher(exporterConfig(), nil)
	client := &anchorClient{err: context.Canceled}

	_, _ = m.ResolveCtx(ctx, client, "self", "db1", false)

	client.err = nil
	client.series = map[string][]map[string]string{"pg_static": {pgStatic("pgexp:9187", "pg", "db1.example.com:5432")}}

	rt, _ := m.ResolveCtx(context.Background(), client, "self", "db1", false)
	if rt.Scrape[RoleCore] == "" {
		t.Error("caller cancellation was cached as a datasource failure")
	}
}

func staticMeta() fakeMeta {
	return fakeMeta{"lab/db1.lab": {Source: "static", Port: "6543"}}
}

func TestResolve_AutoMapStatic(t *testing.T) {
	off := false

	cases := []struct {
		name   string
		mutate func(*Config)
		mapped bool
	}{
		{"postgres_exporter core", func(*Config) {}, true},
		{"pgscv core", func(c *Config) { c.Providers.Core = ProviderPgSCV }, false},
		{"pgscv core, node_exporter host", func(c *Config) {
			c.Providers = ProvidersConfig{Core: ProviderPgSCV, Pooler: ProviderNone, Host: ProviderNodeExporter}
		}, false},
		{"auto_map_static off", func(c *Config) { c.AutoMapStatic = &off }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := exporterConfig()
			c.Targets = nil
			tc.mutate(&c)

			m, _ := NewMatcher(c, staticMeta())

			rt, err := m.Resolve("lab", "db1.lab")
			if !tc.mapped {
				if !errors.Is(err, ErrTargetNotMapped) {
					t.Fatalf("Resolve = %+v, %v; want ErrTargetNotMapped", rt, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			if rt.Host != "db1.lab" || rt.Instance != "db1.lab" || rt.HostShort != "db1" || rt.Port != "6543" {
				t.Errorf("Resolve = %+v", rt)
			}

			if rt.Env != "" || rt.Service != "" {
				t.Errorf("Env/Service = %q/%q, want empty", rt.Env, rt.Service)
			}
		})
	}
}

func TestResolve_DiscoveredPortDefault(t *testing.T) {
	c := Default()
	m, _ := NewMatcher(c, fakeMeta{"mdb/h1.mdb": {Source: "yandex-mdb"}})

	rt, err := m.Resolve("mdb", "h1.mdb")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if rt.Port != "5432" {
		t.Errorf("Port = %q, want 5432", rt.Port)
	}
}

func TestMatcher_ValidateMatchKind(t *testing.T) {
	c := exporterConfig()
	c.Providers.Host = ProviderPgSCVSystem
	c.Selectors = map[string]string{SelectorPgBouncerExporter: `job="pgb",instance="{{.HostShort}}:9127"`}
	c = c.WithDefaults()
	c.Targets[0].Env, c.Targets[0].Service, c.Targets[0].Container = "dev", "svc", "db1"

	m, _ := NewMatcher(c, nil)
	client := &anchorClient{series: map[string][]map[string]string{
		"pg_static": {pgStatic("pgexp:9187", "pg", "db1.example.com:5432")},
	}}

	diag, err := m.Validate(context.Background(), client, "self", "db1")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	got := map[Role]string{}
	for _, rd := range diag.Roles {
		got[rd.Role] = rd.Match
	}

	want := map[Role]string{RoleCore: MatchAnchor, RolePooler: MatchExplicit, RoleHost: MatchStatic}
	for role, w := range want {
		if got[role] != w {
			t.Errorf("%s: match %q, want %q", role, got[role], w)
		}
	}
}

func TestMatcher_ValidateTemplateMatch(t *testing.T) {
	m, _ := NewMatcher(exporterConfig(), nil)

	diag, err := m.Validate(context.Background(), &anchorClient{}, "self", "db1")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	for _, rd := range diag.Roles {
		if rd.Match != MatchTemplate {
			t.Errorf("%s: match %q, want template", rd.Role, rd.Match)
		}
	}
}

func TestService_ValidateTargetMissingSignals(t *testing.T) {
	cfg := exporterConfig().WithDefaults()
	m, _ := NewMatcher(cfg, nil)

	absent := []string{"pg_stat_statements", "checkpoint", "pg_stat_replication", "pg_stat_user_tables_n_dead_tup"}
	client := &exprClient{value: func(expr string) (float64, bool) {
		if strings.HasPrefix(expr, "count by") {
			return 0, false
		}

		for _, a := range absent {
			if strings.Contains(expr, a) {
				return 0, false
			}
		}

		return 1, true
	}}

	s := newService(cfg, m, client, nil)

	diag, err := s.ValidateTarget(context.Background(), "self", "db1")
	if err != nil {
		t.Fatalf("ValidateTarget: %v", err)
	}

	var core RoleDiagnostic

	for _, rd := range diag.Roles {
		if rd.Role == RoleCore {
			core = rd
		} else if len(rd.Missing) > 0 {
			t.Errorf("%s: missing %v, want none", rd.Role, rd.Missing)
		}
	}

	want := []MissingSignal{
		{SigCheckpointsAllRate, "--collector.stat_checkpointer"},
		{SigCheckpointsReqRate, "--collector.stat_checkpointer"},
		{SigLatencyMs, "--collector.stat_statements"},
		{SigRequestedCheckpoints, "--collector.stat_checkpointer"},
		{SigTimedCheckpoints, "--collector.stat_checkpointer"},
	}

	if !slices.Equal(core.Missing, want) {
		t.Errorf("core missing = %v, want %v", core.Missing, want)
	}
}
