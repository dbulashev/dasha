// Package metrics implements the metrics-backed Health Score data path: a thin
// client over a Prometheus/VictoriaMetrics-compatible datasource, a label
// matcher that maps a Dasha (cluster, instance) target to provider-specific
// series selectors, and a catalog of MetricsQL templates per signal/provider.
//
// It is additive to the existing SQL snapshot path: when disabled or when a
// target cannot be matched, callers fall back to the snapshot score. See
// plans/health-score-history-design.md.
package metrics

import (
	"errors"
	"fmt"
	"time"
)

// Provider identifies a metrics source kind.
type Provider string

const (
	ProviderPgSCV       Provider = "pgscv"        // PG internals (incl. YC MDB via remote scrape)
	ProviderYCNative    Provider = "yc_native"    // YC managed host + pooler metrics
	ProviderPgBouncer   Provider = "pgbouncer"    // self-managed pooler via pgSCV
	ProviderPgSCVSystem Provider = "pgscv_system" // self-managed host (system collector)
)

// Role is the responsibility a provider fulfils for a target.
type Role string

const (
	RoleCore   Role = "core"   // PG internals
	RolePooler Role = "pooler" // connection pooler
	RoleHost   Role = "host"   // OS/host saturation
)

// Config is the global `health_score.metrics` configuration block.
type Config struct {
	Enabled    bool              `mapstructure:"enabled"`
	Datasource DatasourceConfig  `mapstructure:"datasource"`
	Baseline   BaselineConfig    `mapstructure:"baseline"`
	Dips       DipsConfig        `mapstructure:"dips"`
	Floor      FloorConfig       `mapstructure:"floor"`
	Providers  ProvidersConfig   `mapstructure:"providers_default"`
	Selectors  map[string]string `mapstructure:"selectors"` // selector-key -> Go-template
	Targets    []TargetMapping   `mapstructure:"targets"`

	// AutoMapDiscovered derives a label mapping for service-discovered clusters
	// that are not enumerated in Targets (default true). Only non-static
	// clusters qualify; a self-managed cluster without a target stays unmapped
	// and falls back to the SQL snapshot. A static Targets[] entry always wins.
	AutoMapDiscovered *bool `mapstructure:"auto_map_discovered"`
	// DiscoveryEnvLabel is the discovery Labels key whose value feeds the
	// {{.Env}} selector variable for auto-mapped targets (default "folder_id").
	DiscoveryEnvLabel string `mapstructure:"discovery_env_label"`
}

// autoMapEnabled reports whether discovered clusters absent from Targets should
// be auto-mapped. Unset (nil) defaults to true.
func (c Config) autoMapEnabled() bool {
	return c.AutoMapDiscovered == nil || *c.AutoMapDiscovered
}

// envLabelKey is the discovery label feeding {{.Env}} for auto-mapped targets.
func (c Config) envLabelKey() string {
	if c.DiscoveryEnvLabel == "" {
		return "folder_id"
	}

	return c.DiscoveryEnvLabel
}

// DatasourceConfig describes the TSDB endpoint.
type DatasourceConfig struct {
	URL           string        `mapstructure:"url"`
	Auth          AuthConfig    `mapstructure:"auth"`
	Timeout       time.Duration `mapstructure:"timeout"`
	QueryCacheTTL time.Duration `mapstructure:"query_cache_ttl"`
}

// AuthConfig holds datasource credentials. Treat token/password as secrets:
// prefer the *_from_env variants so they are injected from a Secret at runtime
// instead of being stored inline (e.g. in a ConfigMap). When both are set, the
// resolved env value wins.
type AuthConfig struct {
	Type            string `mapstructure:"type"` // none|bearer|basic
	Token           string `mapstructure:"token"`
	TokenFromEnv    string `mapstructure:"token_from_env"` // env var holding the bearer token
	Username        string `mapstructure:"username"`
	Password        string `mapstructure:"password"`
	PasswordFromEnv string `mapstructure:"password_from_env"` // env var holding the basic-auth password
}

// BaselineConfig tunes the seasonal baseline window.
type BaselineConfig struct {
	Window     time.Duration `mapstructure:"window"`
	MinHistory time.Duration `mapstructure:"min_history"`
}

// DipsConfig sets dip-detection thresholds.
type DipsConfig struct {
	ScorePoints   float64 `mapstructure:"score_points"`
	LatencyFactor float64 `mapstructure:"latency_factor"`
}

// FloorConfig calibrates metric-driven critical conditions.
type FloorConfig struct {
	WraparoundLeft int64 `mapstructure:"wraparound_left"` // xacts_left below -> critical
}

// ProvidersConfig assigns a provider to each role.
type ProvidersConfig struct {
	Core   Provider `mapstructure:"core"`
	Pooler Provider `mapstructure:"pooler"`
	Host   Provider `mapstructure:"host"`
}

// TargetMapping projects a Dasha (cluster, instance) onto datasource identifiers.
type TargetMapping struct {
	Cluster   string           `mapstructure:"cluster"`   // Dasha cluster name
	Instance  string           `mapstructure:"instance"`  // Dasha instance (host)
	Env       string           `mapstructure:"env"`       // -> cluster label
	Service   string           `mapstructure:"service"`   // -> service_id/resource_id/subcluster_name
	Host      string           `mapstructure:"host"`      // -> host label (full FQDN, join key)
	Container string           `mapstructure:"container"` // -> container label (pgSCV short FQDN)
	Providers *ProvidersConfig `mapstructure:"providers"` // per-target override
}

// Selector-key constants used in the Selectors template map.
const (
	SelectorPgSCV          = "pgscv"
	SelectorYCNativeHost   = "yc_native_host"
	SelectorYCNativePooler = "yc_native_pooler"
	SelectorPgBouncer      = "pgbouncer"
	SelectorPgSCVSystem    = "pgscv_system"
)

// Default returns the built-in defaults (feature disabled). Selector templates
// match the real label schemes captured in the requirements (Appendix A).
func Default() Config {
	return Config{
		Enabled: false,
		Datasource: DatasourceConfig{
			Timeout:       10 * time.Second,
			QueryCacheTTL: 30 * time.Second,
		},
		Baseline: BaselineConfig{
			Window:     28 * 24 * time.Hour,
			MinHistory: 14 * 24 * time.Hour,
		},
		Dips:  DipsConfig{ScorePoints: 10, LatencyFactor: 2.0},
		Floor: FloorConfig{WraparoundLeft: 200_000_000},
		Providers: ProvidersConfig{
			Core:   ProviderPgSCV,
			Pooler: ProviderYCNative,
			Host:   ProviderYCNative,
		},
		Selectors: map[string]string{
			SelectorPgSCV:          `cluster="{{.Env}}",service_id="{{.Service}}",container="{{.Container}}"`,
			SelectorYCNativeHost:   `cluster="{{.Env}}",resource_id="{{.Service}}"`,
			SelectorYCNativePooler: `cluster="{{.Env}}",subcluster_name="{{.Service}}"`,
			SelectorPgBouncer:      `cluster="{{.Env}}",service_id="{{.Service}}",container="{{.Container}}"`,
			SelectorPgSCVSystem:    `cluster="{{.Env}}",service_id="{{.Service}}",container="{{.Container}}"`,
		},
	}
}

// WithDefaults returns a copy with zero-valued fields filled from Default(),
// including any selector keys missing from a partial override map (mapstructure
// replaces maps wholesale, so a user overriding one selector would otherwise
// drop the rest).
func (c Config) WithDefaults() Config {
	d := Default()

	if c.Datasource.Timeout <= 0 {
		c.Datasource.Timeout = d.Datasource.Timeout
	}

	if c.Datasource.QueryCacheTTL <= 0 {
		c.Datasource.QueryCacheTTL = d.Datasource.QueryCacheTTL
	}

	if c.Baseline.Window <= 0 {
		c.Baseline.Window = d.Baseline.Window
	}

	if c.Baseline.MinHistory <= 0 {
		c.Baseline.MinHistory = d.Baseline.MinHistory
	}

	if c.Dips.ScorePoints <= 0 {
		c.Dips.ScorePoints = d.Dips.ScorePoints
	}

	if c.Dips.LatencyFactor <= 0 {
		c.Dips.LatencyFactor = d.Dips.LatencyFactor
	}

	if c.Floor.WraparoundLeft <= 0 {
		c.Floor.WraparoundLeft = d.Floor.WraparoundLeft
	}

	if c.Providers.Core == "" {
		c.Providers.Core = d.Providers.Core
	}

	if c.Providers.Pooler == "" {
		c.Providers.Pooler = d.Providers.Pooler
	}

	if c.Providers.Host == "" {
		c.Providers.Host = d.Providers.Host
	}

	if c.Selectors == nil {
		c.Selectors = map[string]string{}
	}

	for k, v := range d.Selectors {
		if c.Selectors[k] == "" {
			c.Selectors[k] = v
		}
	}

	return c
}

// ErrInvalidConfig is returned when the enabled config is missing required fields.
var ErrInvalidConfig = errors.New("invalid metrics config")

// Validate checks the configuration. A disabled config is always valid.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Datasource.URL == "" {
		return fmt.Errorf("%w: datasource.url is required when enabled", ErrInvalidConfig)
	}

	if c.Baseline.Window <= 0 || c.Baseline.MinHistory <= 0 || c.Baseline.MinHistory > c.Baseline.Window {
		return fmt.Errorf("%w: baseline window/min_history out of range", ErrInvalidConfig)
	}

	for _, k := range []string{SelectorPgSCV, SelectorYCNativeHost, SelectorYCNativePooler, SelectorPgBouncer} {
		if c.Selectors[k] == "" {
			return fmt.Errorf("%w: selectors[%s] is empty", ErrInvalidConfig, k)
		}
	}

	switch c.Datasource.Auth.Type {
	case "", "none", "bearer", "basic":
	default:
		return fmt.Errorf("%w: datasource.auth.type %q (want none|bearer|basic)", ErrInvalidConfig, c.Datasource.Auth.Type)
	}

	return nil
}

// providersFor returns the effective role->provider mapping for a target,
// applying a per-target override on top of the global default.
func (c Config) providersFor(t *TargetMapping) ProvidersConfig {
	p := c.Providers
	if t != nil && t.Providers != nil {
		if t.Providers.Core != "" {
			p.Core = t.Providers.Core
		}

		if t.Providers.Pooler != "" {
			p.Pooler = t.Providers.Pooler
		}

		if t.Providers.Host != "" {
			p.Host = t.Providers.Host
		}
	}

	return p
}
