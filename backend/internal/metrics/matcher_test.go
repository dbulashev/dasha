package metrics

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	c := Default()
	c.Enabled = true
	c.Datasource.URL = "http://vm.example:8428"
	c.Targets = []TargetMapping{
		{
			Cluster: "prod-mdb", Instance: "rc1a-abc.mdb.yandexcloud.net",
			Env: "dev", Service: "pharma_stocks",
			Host: "rc1a-abc.mdb.yandexcloud.net", Container: "rc1a-abc",
		},
	}

	return c
}

func TestMatcher_ResolveAndSelectors(t *testing.T) {
	m, err := NewMatcher(testConfig(), nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	rt, err := m.Resolve("prod-mdb", "rc1a-abc.mdb.yandexcloud.net")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	cases := []struct {
		provider Provider
		role     Role
		contains []string
	}{
		{ProviderPgSCV, RoleCore, []string{`cluster="dev"`, `service_id="pharma_stocks"`, `container="rc1a-abc"`}},
		{ProviderYCNative, RoleHost, []string{`cluster="dev"`, `resource_id="pharma_stocks"`}},
		{ProviderYCNative, RolePooler, []string{`cluster="dev"`, `subcluster_name="pharma_stocks"`}},
		{ProviderPgBouncer, RolePooler, []string{`service_id="pharma_stocks"`, `container="rc1a-abc"`}},
	}

	for _, tc := range cases {
		sel, err := m.Selector(tc.provider, tc.role, rt)
		if err != nil {
			t.Fatalf("Selector(%s,%s): %v", tc.provider, tc.role, err)
		}

		for _, want := range tc.contains {
			if !strings.Contains(sel, want) {
				t.Errorf("selector %s/%s = %q, missing %q", tc.provider, tc.role, sel, want)
			}
		}
	}
}

func TestMatcher_ResolveUnmapped(t *testing.T) {
	m, _ := NewMatcher(testConfig(), nil)

	if _, err := m.Resolve("unknown", "nope"); err == nil {
		t.Fatal("expected ErrTargetNotMapped for unmapped target")
	}
}

// fakeClient returns a fixed sample count per validation metric.
type fakeClient struct {
	counts map[string]int
}

func (f fakeClient) QueryInstant(_ context.Context, expr string, _ time.Time) ([]Sample, error) {
	for metric, n := range f.counts {
		if strings.HasPrefix(expr, metric+"{") {
			out := make([]Sample, n)
			for i := range out {
				out[i] = Sample{Labels: map[string]string{"i": "x"}}
			}

			return out, nil
		}
	}

	return nil, nil
}

func (f fakeClient) QueryRange(_ context.Context, _ string, _ Range) ([]Series, error) {
	return nil, nil
}

func TestMatcher_Validate(t *testing.T) {
	m, _ := NewMatcher(testConfig(), nil)

	client := fakeClient{counts: map[string]int{
		"postgres_up":     1, // core OK
		"n_cpus":          1, // host OK
		"pooler_is_alive": 2, // pooler ambiguous (>1)
	}}

	diag, err := m.Validate(context.Background(), client, "prod-mdb", "rc1a-abc.mdb.yandexcloud.net")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if len(diag.Roles) != 3 {
		t.Fatalf("expected 3 role diagnostics, got %d", len(diag.Roles))
	}

	got := map[Role]RoleDiagnostic{}
	for _, rd := range diag.Roles {
		got[rd.Role] = rd
	}

	if !got[RoleCore].OK || got[RoleCore].Matched != 1 {
		t.Errorf("core: want OK/1, got %+v", got[RoleCore])
	}

	if got[RolePooler].OK || got[RolePooler].Matched != 2 {
		t.Errorf("pooler: want not-OK/2, got %+v", got[RolePooler])
	}
}

// TestMatcher_ValidateAgainstRealDatasource is an opt-in harness for the
// Phase-1 gate: point it at the real VM and confirm the selector templates
// match. Skipped unless METRICS_DATASOURCE_URL is set. Required env:
// METRICS_DATASOURCE_URL and target identifiers MS_ENV/MS_SERVICE/MS_HOST/MS_CONTAINER.
func TestMatcher_ValidateAgainstRealDatasource(t *testing.T) {
	url := os.Getenv("METRICS_DATASOURCE_URL")
	if url == "" {
		t.Skip("set METRICS_DATASOURCE_URL to validate label matching against the real datasource")
	}

	cfg := Default()
	cfg.Enabled = true
	cfg.Datasource.URL = url
	cfg.Targets = []TargetMapping{{
		Cluster: "real", Instance: "real",
		Env:       os.Getenv("MS_ENV"),
		Service:   os.Getenv("MS_SERVICE"),
		Host:      os.Getenv("MS_HOST"),
		Container: os.Getenv("MS_CONTAINER"),
	}}

	m, err := NewMatcher(cfg, nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	diag, err := m.Validate(context.Background(), NewVMClient(cfg.Datasource), "real", "real")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	for _, rd := range diag.Roles {
		t.Logf("role=%-6s provider=%-10s metric=%-16s matched=%d ok=%v selector=%q err=%q sample=%v",
			rd.Role, rd.Provider, rd.Metric, rd.Matched, rd.OK, rd.Selector, rd.Err, rd.Sample)
	}

	for _, rd := range diag.Roles {
		if rd.Role == RoleCore && !rd.OK {
			t.Errorf("core role did not match exactly one series (matched=%d) — fix pgscv selector/target mapping", rd.Matched)
		}
	}
}

// fakeMeta implements MetadataProvider for the auto-mapping tests.
type fakeMeta map[string]DiscoveryMeta

func (f fakeMeta) LookupMeta(cluster, instance string) (DiscoveryMeta, bool) {
	md, ok := f[cluster+"/"+instance]

	return md, ok
}

func TestMatcher_AutoMapDiscovered(t *testing.T) {
	cfg := Default()
	cfg.Enabled = true
	cfg.Datasource.URL = "http://vm:8428"
	// No static targets[] — the discovered cluster must be auto-mapped.

	meta := fakeMeta{
		"folderA_prod/rc1a-abc.mdb.yandexcloud.net": {
			Source:     "yandex-mdb",
			ProviderID: "mdbcluster123",
			Labels:     map[string]string{"folder_id": "b1gxxxx"},
		},
		"static_x/h1": {Source: "static"},
	}

	m, err := NewMatcher(cfg, meta)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	rt, err := m.Resolve("folderA_prod", "rc1a-abc.mdb.yandexcloud.net")
	if err != nil {
		t.Fatalf("Resolve discovered: %v", err)
	}

	if rt.Service != "mdbcluster123" {
		t.Errorf("Service = %q, want mdbcluster123 (ProviderID)", rt.Service)
	}

	if rt.Host != "rc1a-abc.mdb.yandexcloud.net" {
		t.Errorf("Host = %q, want the instance FQDN", rt.Host)
	}

	if rt.Container != "rc1a-abc" {
		t.Errorf("Container = %q, want short host rc1a-abc", rt.Container)
	}

	if rt.Env != "b1gxxxx" {
		t.Errorf("Env = %q, want folder_id label b1gxxxx", rt.Env)
	}

	if rt.Providers.Core != ProviderPgSCV || rt.Providers.Host != ProviderYCNative {
		t.Errorf("providers = %+v, want core=pgscv host=yc_native (providers_default)", rt.Providers)
	}

	// A static cluster without a targets[] entry stays unmapped.
	if _, err := m.Resolve("static_x", "h1"); err == nil {
		t.Error("static cluster without target should stay unmapped")
	}

	// Unknown target stays unmapped.
	if _, err := m.Resolve("nope", "nope"); err == nil {
		t.Error("unknown target should stay unmapped")
	}
}

func TestMatcher_AutoMapDisabled(t *testing.T) {
	cfg := Default()
	cfg.Enabled = true
	cfg.Datasource.URL = "http://vm:8428"
	off := false
	cfg.AutoMapDiscovered = &off

	meta := fakeMeta{"c/h": {Source: "yandex-mdb", ProviderID: "x"}}

	m, _ := NewMatcher(cfg, meta)
	if _, err := m.Resolve("c", "h"); err == nil {
		t.Error("auto_map_discovered=false: discovered target should stay unmapped")
	}
}
