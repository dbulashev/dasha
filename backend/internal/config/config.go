package config

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dbulashev/dasha/internal/metrics"
)

var (
	errTokenRequired  = errors.New("auth.mode=token requires at least one token")
	errOIDCRequired   = errors.New("auth.mode=oidc requires oidc section")
	errOIDCIncomplete = errors.New("oidc requires issuer_url and client_id")
)

type AuthMode string

const (
	AuthModeNone  AuthMode = "none"
	AuthModeToken AuthMode = "token"
	AuthModeOIDC  AuthMode = "oidc"
)

type AuthToken struct {
	Name         string `mapstructure:"name"`
	Token        string `mapstructure:"token"`
	TokenFromEnv string `mapstructure:"token_from_env"`
	Role         string `mapstructure:"role"` // default: "viewer"
}

type OIDCConfig struct {
	IssuerURL           string            `mapstructure:"issuer_url"`
	ClientID            string            `mapstructure:"client_id"`
	ClientSecret        string            `mapstructure:"client_secret"`
	ClientSecretFromEnv string            `mapstructure:"client_secret_from_env"`
	Scopes              []string          `mapstructure:"scopes"`
	RedirectURL         string            `mapstructure:"redirect_url"`
	RoleClaim           string            `mapstructure:"role_claim"`   // default: "realm_access.roles"
	RoleMapping         map[string]string `mapstructure:"role_mapping"` // e.g. {"dba_team": "admin", "dev_team": "viewer"}
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second"` // 0 = disabled
	Burst             int     `mapstructure:"burst"`               // max burst size
}

type AuthConfig struct {
	Mode                AuthMode         `mapstructure:"mode"` // default: "none"
	Tokens              []AuthToken      `mapstructure:"tokens"`
	OIDC                *OIDCConfig      `mapstructure:"oidc"`
	CookieSecret        string           `mapstructure:"cookie_secret"`
	CookieSecretFromEnv string           `mapstructure:"cookie_secret_from_env"`
	CookieMaxAge        int              `mapstructure:"cookie_max_age"` // seconds, default: 86400
	RequireHTTPS        bool             `mapstructure:"require_https"`
	RateLimit           *RateLimitConfig `mapstructure:"rate_limit"`
}

func (a *AuthConfig) Validate() error {
	switch a.Mode {
	case AuthModeNone, "":
		return nil
	case AuthModeToken:
		if len(a.Tokens) == 0 {
			return errTokenRequired
		}
	case AuthModeOIDC:
		if a.OIDC == nil {
			return errOIDCRequired
		}

		if a.OIDC.IssuerURL == "" || a.OIDC.ClientID == "" {
			return errOIDCIncomplete
		}
	default:
		return fmt.Errorf("unknown auth.mode: %q", a.Mode)
	}

	return nil
}

type Database string

func (d Database) String() string {
	return string(d)
}

type Host string

func (h Host) String() string {
	return string(h)
}

type ClusterName string

func (n ClusterName) String() string {
	return string(n)
}

// Cluster represents a PostgreSQL cluster connection configuration.
type Cluster struct {
	Name            ClusterName
	UserName        string
	Password        string
	PasswordFromEnv string `mapstructure:"password_from_env"`
	Port            string
	Databases       []Database
	Hosts           []Host

	// Extended attributes for service discovery.
	Source     string            `mapstructure:"source"`
	ProviderID string            `mapstructure:"provider_id"`
	Labels     map[string]string `mapstructure:"labels"`
}

// SourceYandexMDB marks clusters discovered from Yandex Managed Databases.
const SourceYandexMDB = "yandex-mdb"

// SupportsLogs reports whether cluster logs can be searched via the provider
// API. Single source of truth for the capability — exposed to the frontend as
// Cluster.supports_logs and checked by the logs service.
func (c Cluster) SupportsLogs() bool {
	return c.Source == SourceYandexMDB && c.ProviderID != "" && c.Labels["folder_id"] != ""
}

// DiscoveryClusterFilter defines regex matching rules for discovered clusters.
type DiscoveryClusterFilter struct {
	Name        string  `mapstructure:"name"`
	Db          *string `mapstructure:"db"`
	ExcludeName *string `mapstructure:"exclude_name"`
	ExcludeDb   *string `mapstructure:"exclude_db"`
}

// YandexMDBConfig holds Yandex Managed Database discovery settings.
type YandexMDBConfig struct {
	AuthorizedKey   string                   `mapstructure:"authorized_key"`
	FolderID        string                   `mapstructure:"folder_id"`
	User            string                   `mapstructure:"user"`
	Password        string                   `mapstructure:"password"`
	PasswordFromEnv string                   `mapstructure:"password_from_env"`
	RefreshInterval int                      `mapstructure:"refresh_interval"`
	Clusters        []DiscoveryClusterFilter `mapstructure:"clusters"`
}

// DiscoveryEntry represents a single discovery source (one folder).
type DiscoveryEntry struct {
	Type   string          `mapstructure:"type"`
	Config YandexMDBConfig `mapstructure:"config"`
}

// LogSearchConfig holds global limits for Yandex Cloud log search.
type LogSearchConfig struct {
	MaxScan        int `mapstructure:"max_scan"`        // max records scanned per search; default 5000
	MaxPageSize    int `mapstructure:"max_page_size"`   // upper bound for page_size; default 1000
	TimeoutSeconds int `mapstructure:"timeout_seconds"` // upstream read timeout; default 30

	// RateLimit / AdminRateLimit throttle GET /api/logs per user (per IP when
	// anonymous). Unset = built-in defaults; requests_per_second <= 0 disables
	// the corresponding limit.
	RateLimit      *RateLimitConfig `mapstructure:"rate_limit"`
	AdminRateLimit *RateLimitConfig `mapstructure:"admin_rate_limit"`
}

// Defaults for LogSearchConfig when values are unset (<= 0).
const (
	DefaultLogSearchMaxScan        = 5000
	DefaultLogSearchMaxPageSize    = 1000
	DefaultLogSearchTimeoutSeconds = 30
)

// Default log search rate limits: non-admins 1 req/30s with burst 10, admins
// 1 req/5s with burst 20.
var (
	DefaultLogSearchRateLimit      = RateLimitConfig{RequestsPerSecond: 1.0 / 30, Burst: 10}
	DefaultLogSearchAdminRateLimit = RateLimitConfig{RequestsPerSecond: 1.0 / 5, Burst: 20}
)

// WithDefaults returns a copy with unset (<=0) fields filled from defaults.
func (c LogSearchConfig) WithDefaults() LogSearchConfig {
	if c.MaxScan <= 0 {
		c.MaxScan = DefaultLogSearchMaxScan
	}

	if c.MaxPageSize <= 0 {
		c.MaxPageSize = DefaultLogSearchMaxPageSize
	}

	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = DefaultLogSearchTimeoutSeconds
	}

	if c.RateLimit == nil {
		rl := DefaultLogSearchRateLimit
		c.RateLimit = &rl
	}

	if c.AdminRateLimit == nil {
		rl := DefaultLogSearchAdminRateLimit
		c.AdminRateLimit = &rl
	}

	return c
}

// StorageConfig holds optional snapshot storage database settings.
type StorageConfig struct {
	// DSN is the service connection: regular reads/writes (DML). In hardened
	// installs this role has no DDL privileges.
	DSN        string `mapstructure:"dsn"`
	DSNFromEnv string `mapstructure:"dsn_from_env"`

	// DSNMigration is a privileged connection allowed to run DDL — migrations
	// (CREATE/ALTER tables) and daily partition creation. Falls back to DSN when
	// empty, so single-role installs keep working unchanged.
	DSNMigration        string `mapstructure:"dsn_migration"`
	DSNMigrationFromEnv string `mapstructure:"dsn_migration_from_env"`

	// LeaderElection enables advisory-lock leader election for the autosnapshot
	// daemon, making it safe to run multiple replicas (one becomes leader).
	// Disabled by default: a session-level advisory lock requires a dedicated,
	// long-lived connection, which is incompatible with transaction-pooling
	// proxies (e.g. PgBouncer in transaction mode). Enable only when the daemon
	// reaches the storage DB via a direct/session-pooled connection and you run
	// more than one replica.
	LeaderElection bool `mapstructure:"leader_election"`
}

// Enabled returns true if the storage DSN is configured.
func (s *StorageConfig) Enabled() bool {
	return s.DSN != ""
}

// MigrationDSN returns the DDL-capable connection string, falling back to the
// service DSN when no dedicated migration role is configured.
func (s *StorageConfig) MigrationDSN() string {
	if s.DSNMigration != "" {
		return s.DSNMigration
	}

	return s.DSN
}

// Config is the top-level application configuration.
type Config struct {
	Debug     bool                      `mapstructure:"debug"`
	Clusters  []Cluster                 `mapstructure:"clusters"`
	Discovery map[string]DiscoveryEntry `mapstructure:"discovery"`
	Auth      AuthConfig                `mapstructure:"auth"`
	Storage   StorageConfig             `mapstructure:"storage"`

	// PgStatsView is an optional custom view name to use instead of pg_catalog.pg_stats.
	// Useful when the connecting user lacks privileges to read pg_catalog.pg_stats
	// but a DBA has created an accessible view (e.g. "monitoring.pg_stats").
	// If empty, pg_catalog.pg_stats is used by default.
	PgStatsView string `mapstructure:"pg_stats_view"`

	// EnableQueryStatsReset allows resetting pg_stat_statements statistics via the UI.
	// Disabled by default for safety.
	EnableQueryStatsReset bool `mapstructure:"enable_query_stats_reset"`

	// LogSearch holds global limits for Yandex Cloud log search.
	LogSearch LogSearchConfig `mapstructure:"log_search"`

	// PgssResetFunction is an optional custom function (schema-qualified, no args)
	// to call instead of pg_stat_statements_reset(). Useful when the connecting
	// role lacks EXECUTE on pg_stat_statements_reset but a DBA exposes a SECURITY
	// DEFINER wrapper (e.g. "monitoring.reset_pgss"). Empty = pg_stat_statements_reset.
	PgssResetFunction string `mapstructure:"pgss_reset_function"`

	// DBPool tunes the connection pools to monitored clusters (one pool per
	// host/database). The storage pool is tuned via storage.dsn query params
	// (pool_max_conns, pool_max_conn_idle_time, ...) instead.
	DBPool PoolConfig `mapstructure:"db_pool"`

	// AutosnapshotDBPool overrides DBPool for the `dasha autosnapshot` daemon —
	// e.g. a short max_conn_idle_time so the daemon frees connections between
	// polls when the monitoring role has a tight connection budget. Per-field:
	// unset (zero) fields inherit DBPool.
	AutosnapshotDBPool PoolConfig `mapstructure:"autosnapshot_db_pool"`

	// HealthScore groups Health Score settings (metrics-backed mode).
	HealthScore HealthScoreConfig `mapstructure:"health_score"`
}

// PoolConfig tunes a pgx connection pool. Zero MaxConns/MaxConnIdleTime fall back
// to Dasha's pooler-friendly defaults (4 / 2m) rather than pgx's (max(4,NumCPU) /
// 30m), since Dasha opens one pool per (host,database) behind a per-user pooler.
// Zero MaxConnLifetime keeps the pgx default (1h).
type PoolConfig struct {
	MaxConns        int32         `mapstructure:"max_conns"`
	MaxConnIdleTime time.Duration `mapstructure:"max_conn_idle_time"`
	MaxConnLifetime time.Duration `mapstructure:"max_conn_lifetime"`
}

// EffectiveAutosnapshotPool returns DBPool with any non-zero AutosnapshotDBPool
// fields applied on top (per-field override).
func (c Config) EffectiveAutosnapshotPool() PoolConfig {
	p := c.DBPool

	if c.AutosnapshotDBPool.MaxConns != 0 {
		p.MaxConns = c.AutosnapshotDBPool.MaxConns
	}

	if c.AutosnapshotDBPool.MaxConnIdleTime != 0 {
		p.MaxConnIdleTime = c.AutosnapshotDBPool.MaxConnIdleTime
	}

	if c.AutosnapshotDBPool.MaxConnLifetime != 0 {
		p.MaxConnLifetime = c.AutosnapshotDBPool.MaxConnLifetime
	}

	return p
}

// HealthScoreConfig groups Health Score settings.
type HealthScoreConfig struct {
	Metrics metrics.Config `mapstructure:"metrics"`
}

// Clusters is the interface for obtaining the current list of clusters.
type Clusters interface {
	Get(ctx context.Context) ([]Cluster, error)
	// Update replaces discovered clusters while keeping static ones.
	Update(discovered []Cluster)
}

// ClustersFromConfig stores static + discovery clusters with thread-safe access.
type ClustersFromConfig struct {
	mu         sync.RWMutex
	static     []Cluster
	discovered []Cluster
}

func NewClustersFromConfig(cfg Config) Clusters {
	// Tag static clusters with source.
	for i := range cfg.Clusters {
		if cfg.Clusters[i].Source == "" {
			cfg.Clusters[i].Source = "static"
		}
	}

	return &ClustersFromConfig{ //nolint:exhaustruct
		static: cfg.Clusters,
	}
}

func (c *ClustersFromConfig) Get(_ context.Context) ([]Cluster, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	all := make([]Cluster, 0, len(c.static)+len(c.discovered))
	all = append(all, c.static...)
	all = append(all, c.discovered...)

	return all, nil
}

// Update replaces the set of discovered clusters.
func (c *ClustersFromConfig) Update(discovered []Cluster) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.discovered = discovered
}
