package metrics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExporterCatalogGolden(t *testing.T) {
	const (
		fs  = `fstype=~"ext[34]|xfs|btrfs",device_error=""`
		dt  = `pg_stat_user_tables_n_dead_tup{S}`
		lt  = `pg_stat_user_tables_n_live_tup{S}`
		upd = `sum(rate(pg_stat_user_tables_n_tup_upd{S}[W]))`
	)

	golden := map[catalogKey]string{
		{ProviderPostgresExporter, SigTotalConns}:        `sum(pg_stat_activity_count{S,backend_type="client backend"})`,
		{ProviderPostgresExporter, SigActiveConns}:       `sum(pg_stat_activity_count{S,backend_type="client backend",state="active"}) or 0 * sum(pg_stat_activity_count{S})`,
		{ProviderPostgresExporter, SigIdleInTx}:          `sum(pg_stat_activity_count{S,backend_type="client backend",state="idle in transaction"}) or 0 * sum(pg_stat_activity_count{S})`,
		{ProviderPostgresExporter, SigActiveLockWaiters}: `sum(pg_stat_activity_count{S,backend_type="client backend",wait_event_type="Lock"}) or 0 * sum(pg_stat_activity_count{S})`,
		{ProviderPostgresExporter, SigMaxConns}:          `max(pg_settings_max_connections{S})`,
		{ProviderPostgresExporter, SigAutovacuumOff}:     `min(pg_settings_autovacuum{S}) == bool 0`,
		{ProviderPostgresExporter, SigTrackCountsOff}:    `min(pg_settings_track_counts{S}) == bool 0`,
		{ProviderPostgresExporter, SigCacheHitRatio}: `100 * sum(rate(pg_stat_database_blks_hit{S}[W])) / ` +
			`clamp_min(sum(rate(pg_stat_database_blks_hit{S}[W])) + sum(rate(pg_stat_database_blks_read{S}[W])), 1)`,
		{ProviderPostgresExporter, SigDeadlocksTotal}: `sum(pg_stat_database_deadlocks{S})`,
		{ProviderPostgresExporter, SigTimedCheckpoints}: `sum(pg_stat_checkpointer_num_timed_total{S}) or ` +
			`sum(pg_stat_bgwriter_checkpoints_timed_total{S})`,
		{ProviderPostgresExporter, SigRequestedCheckpoints}: `sum(pg_stat_checkpointer_num_requested_total{S}) or ` +
			`sum(pg_stat_bgwriter_checkpoints_req_total{S})`,
		{ProviderPostgresExporter, SigCheckpointsReqRate}: `sum(rate(pg_stat_checkpointer_num_requested_total{S}[W])) or ` +
			`sum(rate(pg_stat_bgwriter_checkpoints_req_total{S}[W]))`,
		{ProviderPostgresExporter, SigCheckpointsAllRate}: `sum(rate(pg_stat_checkpointer_num_timed_total{S}[W])) + ` +
			`sum(rate(pg_stat_checkpointer_num_requested_total{S}[W])) or ` +
			`sum(rate(pg_stat_bgwriter_checkpoints_timed_total{S}[W])) + sum(rate(pg_stat_bgwriter_checkpoints_req_total{S}[W]))`,
		{ProviderPostgresExporter, SigReplLagBytes}:  `max(pg_stat_replication_pg_wal_lsn_diff{S})`,
		{ProviderPostgresExporter, SigXactsLeftWrap}: `2147483648 - max(pg_database_wraparound_age_datfrozenxid_seconds{S})`,
		{ProviderPostgresExporter, SigMaxDeadRatio}: `100 * max((` + dt + ` / clamp_min(` + dt + ` + ` + lt + `, 1)) and (` +
			dt + ` + ` + lt + ` > 10000))`,
		{ProviderPostgresExporter, SigAvgDeadRatio}: `100 * avg((` + dt + ` / clamp_min(` + dt + ` + ` + lt + `, 1)) and (` +
			dt + ` + ` + lt + ` > 10000))`,
		{ProviderPostgresExporter, SigHotUpdateRatio}: `(sum(rate(pg_stat_user_tables_n_tup_hot_upd{S}[W])) / ` + upd +
			` and ` + upd + ` > 0.1) or (` + upd + ` >= bool 0)`,
		{ProviderPostgresExporter, SigSeqScanRate}: `sum(rate(pg_stat_user_tables_seq_tup_read{S}[W]))`,
		{ProviderPostgresExporter, SigLatencyMs}: `1000 * sum(rate(pg_stat_statements_seconds_total{S}[W])) / ` +
			`clamp_min(sum(rate(pg_stat_statements_calls_total{S}[W])), 1)`,

		{ProviderNodeExporter, SigLoadAvg15}: `max(node_load15{S})`,
		{ProviderNodeExporter, SigNumVCPU}:   `count(count by (cpu) (node_cpu_seconds_total{S}))`,
		{ProviderNodeExporter, SigDiskUsedRatio}: `max((node_filesystem_size_bytes{S,` + fs + `} - node_filesystem_free_bytes{S,` +
			fs + `}) / node_filesystem_size_bytes{S,` + fs + `})`,

		{ProviderPgBouncerExporter, SigPoolerClients}: `sum({__name__=~"pgbouncer_pools_client_(active|waiting)_connections",` +
			`S,database!="pgbouncer"})`,
		{ProviderPgBouncerExporter, SigPoolerServers}: `sum({__name__=~"pgbouncer_pools_server_(active|idle|used|testing|login)_connections",` +
			`S,database!="pgbouncer"})`,
		{ProviderPgBouncerExporter, SigPoolerPoolSize}: `sum(pgbouncer_databases_pool_size{S,database!="pgbouncer"})`,
	}

	cat := NewQueryCatalog()

	for k, want := range golden {
		got, ok := cat.Expr(k.provider, k.signal, "S", "W", "X")
		if !ok {
			t.Errorf("%s/%s: not catalogued", k.provider, k.signal)

			continue
		}

		if got != want {
			t.Errorf("%s/%s:\n got  %s\n want %s", k.provider, k.signal, got, want)
		}
	}

	for k := range cat.templates {
		if isExporterProvider(k.provider) {
			if _, ok := golden[k]; !ok {
				t.Errorf("%s/%s: catalogued but has no golden expression", k.provider, k.signal)
			}
		}
	}
}

func isExporterProvider(p Provider) bool {
	return p == ProviderPostgresExporter || p == ProviderNodeExporter || p == ProviderPgBouncerExporter
}

func TestExporterFlagsCatalogued(t *testing.T) {
	cat := NewQueryCatalog()

	for sig, flag := range exporterFlags {
		if !cat.Supports(ProviderPostgresExporter, sig) {
			t.Errorf("%s (%s) is not catalogued for postgres_exporter", sig, flag)
		}
	}
}

func TestExporterValidationMetrics(t *testing.T) {
	for p, want := range map[Provider]string{
		ProviderPostgresExporter:  "pg_up",
		ProviderNodeExporter:      "node_uname_info",
		ProviderPgBouncerExporter: "pgbouncer_up",
		ProviderNone:              "",
	} {
		if got := ValidationMetric(p); got != want {
			t.Errorf("ValidationMetric(%s) = %q, want %q", p, got, want)
		}
	}
}

var (
	exprMetricRe = regexp.MustCompile(`([a-zA-Z_:][a-zA-Z0-9_:]*)\{`)
	exprNameRe   = regexp.MustCompile(`__name__=~"([^"]*)"`)
	nameAltRe    = regexp.MustCompile(`^([^(]*)\(([^)]*)\)(.*)$`)
)

// templateMetrics lists the metric names a template reads, expanding a single
// `prefix(a|b)suffix` group in a __name__ regex.
func templateMetrics(tpl string) []string {
	var out []string

	for _, m := range exprMetricRe.FindAllStringSubmatch(tpl, -1) {
		out = append(out, m[1])
	}

	for _, m := range exprNameRe.FindAllStringSubmatch(tpl, -1) {
		g := nameAltRe.FindStringSubmatch(m[1])
		if g == nil {
			out = append(out, m[1])

			continue
		}

		for alt := range strings.SplitSeq(g[2], "|") {
			out = append(out, g[1]+alt+g[3])
		}
	}

	return out
}

func fixtureFamilies(t *testing.T) map[string]bool {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("testdata", "exporters", "*", "t1", "*.prom"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no exporter fixtures: %v", err)
	}

	out := map[string]bool{}

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}

		for line := range strings.SplitSeq(string(b), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if i := strings.IndexAny(line, "{ "); i > 0 {
				out[line[:i]] = true
			}
		}
	}

	return out
}

func TestFixturesCoverCatalog(t *testing.T) {
	have := fixtureFamilies(t)
	cat := NewQueryCatalog()

	for k, tpl := range cat.templates {
		if !isExporterProvider(k.provider) {
			continue
		}

		for _, name := range templateMetrics(tpl) {
			if !have[name] {
				t.Errorf("%s/%s reads %s, missing from testdata/exporters", k.provider, k.signal, name)
			}
		}
	}

	for _, p := range []Provider{ProviderPostgresExporter, ProviderNodeExporter, ProviderPgBouncerExporter} {
		if m := ValidationMetric(p); !have[m] {
			t.Errorf("validation metric %s of %s missing from testdata/exporters", m, p)
		}
	}
}

func exporterConfig() Config {
	c := testConfig()
	c.Providers = ProvidersConfig{Core: ProviderPostgresExporter, Pooler: ProviderPgBouncerExporter, Host: ProviderNodeExporter}
	c.Targets = []TargetMapping{
		{Cluster: "self", Instance: "db1", Host: "db1.example.com"},
		{Cluster: "self", Instance: "db2", Host: "10.0.0.2", Providers: &ProvidersConfig{Pooler: ProviderNone}},
	}

	return c
}

func TestConfigValidateProviders(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"exporters", func(*Config) {}, ""},
		{"none in pooler and host", func(c *Config) { c.Providers.Pooler, c.Providers.Host = ProviderNone, ProviderNone }, ""},
		{"legacy providers untouched", func(c *Config) {
			c.Providers = ProvidersConfig{Core: ProviderYCNative, Pooler: ProviderPgSCV, Host: ProviderPgBouncer}
		}, ""},
		{"none in core", func(c *Config) { c.Providers.Core = ProviderNone }, "providers_default.core"},
		{"exporter in wrong role", func(c *Config) { c.Providers.Host = ProviderPostgresExporter }, "providers_default.host"},
		{"typo", func(c *Config) { c.Providers.Core = "postgres-exporter" }, `unknown provider "postgres-exporter"`},
		{"target override", func(c *Config) {
			c.Targets[1].Providers = &ProvidersConfig{Core: ProviderNodeExporter}
		}, "targets[1].providers.core"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := exporterConfig()
			tc.mutate(&c)

			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}

				return
			}

			if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate = %v, want ErrInvalidConfig mentioning %q", err, tc.wantErr)
			}
		})
	}
}

func TestMatcher_ExporterSelectors(t *testing.T) {
	c := exporterConfig()
	c.Selectors = map[string]string{SelectorNodeExporter: `job="node",instance="{{.HostShort}}:9100"`}
	c = c.WithDefaults()

	m, err := NewMatcher(c, nil)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	rt, err := m.Resolve("self", "db1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if rt.Instance != "db1" || rt.HostShort != "db1" {
		t.Errorf("Instance/HostShort = %q/%q, want db1/db1", rt.Instance, rt.HostShort)
	}

	cases := []struct {
		provider Provider
		role     Role
		want     string
	}{
		{ProviderPostgresExporter, RoleCore, `instance=~"db1\\.example\\.com(:[0-9]+)?"`},
		{ProviderPgBouncerExporter, RolePooler, `instance=~"db1\\.example\\.com(:[0-9]+)?"`},
		{ProviderNodeExporter, RoleHost, `job="node",instance="db1:9100"`},
	}

	for _, tc := range cases {
		got, err := m.Selector(tc.provider, tc.role, rt)
		if err != nil {
			t.Fatalf("Selector(%s): %v", tc.provider, err)
		}

		if got != tc.want {
			t.Errorf("Selector(%s) = %s, want %s", tc.provider, got, tc.want)
		}
	}

	rt, err = m.Resolve("self", "db2")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if rt.HostShort != "10.0.0.2" {
		t.Errorf("HostShort of an IP = %q, want it unchanged", rt.HostShort)
	}
}

func TestMatcher_ValidateSkipsNoneRole(t *testing.T) {
	m, _ := NewMatcher(exporterConfig(), nil)

	client := fakeClient{counts: map[string]int{"pg_up": 1, "node_uname_info": 1, "pgbouncer_up": 1}}

	diag, err := m.Validate(context.Background(), client, "self", "db2")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	roles := make([]Role, 0, len(diag.Roles))
	for _, rd := range diag.Roles {
		roles = append(roles, rd.Role)

		if !rd.OK {
			t.Errorf("%s: %+v", rd.Role, rd)
		}
	}

	if !slices.Equal(roles, []Role{RoleCore, RoleHost}) {
		t.Errorf("roles = %v, want [core host]", roles)
	}
}

func TestCollector_NoneRoleQueriesNothing(t *testing.T) {
	m, _ := NewMatcher(exporterConfig(), nil)
	client := &exprClient{value: constValue(1)}
	co := NewCollector(m, NewQueryCatalog(), client, "5m", "", BatchLimits{}, nil)

	s, err := co.Instant(context.Background(), "self", "db2", time.Now(),
		SigPoolerServers, SigPoolerPoolSize, SigLoadAvg15, SigTotalConns)
	if err != nil {
		t.Fatalf("Instant: %v", err)
	}

	if s.Has(SigPoolerServers) || s.Has(SigPoolerPoolSize) {
		t.Error("pooler signals present for a role set to none")
	}

	if !s.Has(SigLoadAvg15) || !s.Has(SigTotalConns) {
		t.Error("host/core signals missing")
	}

	for _, term := range client.terms() {
		if strings.Contains(term, "pgbouncer_") {
			t.Errorf("queried pooler metric for a role set to none: %s", term)
		}
	}
}

func TestPromRegexLiteral(t *testing.T) {
	for _, host := range []string{"db1.example.com", "10.0.0.7", "[::1]", "pg-node_1"} {
		lit := promRegexLiteral(host)

		re, err := strconv.Unquote(`"` + lit + `"`)
		if err != nil {
			t.Fatalf("%s: %q is not a valid PromQL string body: %v", host, lit, err)
		}

		rx := regexp.MustCompile(`^(?:` + re + `)(:[0-9]+)?$`)
		if !rx.MatchString(host) || !rx.MatchString(host+":9187") {
			t.Errorf("%s: %s does not match the host itself", host, lit)
		}
	}

	if promRegexLiteral("db1.example.com") != `db1\\.example\\.com` {
		t.Errorf("promRegexLiteral = %s", promRegexLiteral("db1.example.com"))
	}

	re, _ := strconv.Unquote(`"` + promRegexLiteral("db1.example.com") + `"`)
	if regexp.MustCompile(`^(?:` + re + `)$`).MatchString("db1xexample.com") {
		t.Error("escaped dot matches any character")
	}
}
