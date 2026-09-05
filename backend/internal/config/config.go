package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-viper/mapstructure/v2"

	"github.com/dbulashev/dasha/internal/indexadvisor"
	"github.com/dbulashev/dasha/internal/metrics"
	"github.com/dbulashev/dasha/internal/schemalint"
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

// Roles known to Dasha's RBAC, shared by config validation, PAT rules and the
// OIDC role mapping so the literals cannot drift apart.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
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

	// PATMinRole gates personal-access-token management: "admin" (default —
	// the feature rolls out to admins first) or "viewer" (any OIDC user).
	PATMinRole string `mapstructure:"pat_min_role"`
}

func (a *AuthConfig) Validate() error {
	switch a.PATMinRole {
	case "":
		a.PATMinRole = RoleAdmin
	case RoleViewer, RoleAdmin:
	default:
		return fmt.Errorf("unknown auth.pat_min_role: %q (want admin or viewer)", a.PATMinRole)
	}

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

	// LogSource names the entry in log_search.sources serving this cluster's
	// logs. Empty falls back to the default source, then to the implicit
	// binding of a discovery provider.
	LogSource string `mapstructure:"log_source"`
}

// SourceYandexMDB marks clusters discovered from Yandex Managed Databases.
const SourceYandexMDB = "yandex-mdb"

// SourcePostgres marks clusters whose databases are discovered by querying the
// cluster itself.
const SourcePostgres = "postgres"

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

// DiscoveryEntry represents a single discovery source (one folder). Config is
// kept raw: every discovery type has its own shape and decodes the block into
// its own struct via DecodeDiscoveryConfig.
type DiscoveryEntry struct {
	Type   string         `mapstructure:"type"`
	Config map[string]any `mapstructure:"config"`
}

// DecodeDiscoveryConfig decodes a raw discovery config block into a provider's
// own config struct. Decoder settings mirror viper's, so a block behaves the
// same whether viper unmarshals it directly or it is decoded here: notably
// weak typing, which lets `port: 5432` land in a string field.
//
// Keys the target struct has no field for are returned instead of rejected: an
// old config with a stale key must keep working, but a typo (`excludedb` for
// `exclude_db` silently widens what is monitored) is worth a warning from the
// caller, which has the entry name and a logger.
func DecodeDiscoveryConfig[T any](raw map[string]any) (T, []string, error) {
	var (
		out      T
		metadata mapstructure.Metadata
	)

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{ //nolint:exhaustruct
		Result:           &out,
		Metadata:         &metadata,
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	})
	if err != nil {
		return out, nil, fmt.Errorf("new decoder: %w", err)
	}

	if err := decoder.Decode(raw); err != nil {
		return out, nil, fmt.Errorf("decode discovery config: %w", err)
	}

	// Sorted so the warning reads the same on every start.
	slices.Sort(metadata.Unused)

	return out, metadata.Unused, nil
}

// LogSearchConfig holds global log search limits and the log sources clusters
// read their logs from.
type LogSearchConfig struct {
	MaxScan        int `mapstructure:"max_scan"`        // max records scanned per search; default 5000
	MaxPageSize    int `mapstructure:"max_page_size"`   // upper bound for page_size; default 1000
	TimeoutSeconds int `mapstructure:"timeout_seconds"` // upstream read timeout; default 30

	// RateLimit / AdminRateLimit throttle GET /api/logs per user (per IP when
	// anonymous). Unset = built-in defaults; requests_per_second <= 0 disables
	// the corresponding limit.
	RateLimit      *RateLimitConfig `mapstructure:"rate_limit"`
	AdminRateLimit *RateLimitConfig `mapstructure:"admin_rate_limit"`

	// DefaultSource serves clusters that name no source of their own.
	DefaultSource string `mapstructure:"default_source"`
	// Sources are the configured log stores, keyed by the name a cluster
	// references in log_source.
	Sources map[string]LogSourceConfig `mapstructure:"sources"`
}

// LogSourceTypeOpenSearch is the source type reading an OpenSearch or
// Elasticsearch index.
const LogSourceTypeOpenSearch = "opensearch"

// Stream names the log API serves; a source may declare no others.
const (
	LogStreamPostgreSQL = "postgresql"
	LogStreamPooler     = "pooler"
)

// LogSourceConfig describes one log store.
type LogSourceConfig struct {
	Type      string              `mapstructure:"type"`
	Addresses []string            `mapstructure:"addresses"`
	Auth      LogSourceAuthConfig `mapstructure:"auth"`
	TLS       LogSourceTLSConfig  `mapstructure:"tls"`
	// BatchSize is how many records one upstream request fetches; default 1000.
	BatchSize int `mapstructure:"batch_size"`
	// MaxBoundaryIDs caps the ids a cursor carries for one timestamp before the
	// source stops paginating and marks the result partial; default 10000.
	MaxBoundaryIDs int `mapstructure:"max_boundary_ids"`
	// RateLimit / AdminRateLimit override the global log search limits for
	// clusters served by this source.
	RateLimit      *RateLimitConfig           `mapstructure:"rate_limit"`
	AdminRateLimit *RateLimitConfig           `mapstructure:"admin_rate_limit"`
	Streams        map[string]LogStreamConfig `mapstructure:"streams"`
}

// Log source auth kinds.
const (
	LogAuthNone   = "none"
	LogAuthBasic  = "basic"
	LogAuthAPIKey = "api_key"
)

// LogSourceAuthConfig holds log store credentials. Prefer the *_from_env
// variants so secrets are injected at runtime instead of stored inline.
type LogSourceAuthConfig struct {
	Kind            string `mapstructure:"kind"` // none|basic|api_key
	User            string `mapstructure:"user"`
	Password        string `mapstructure:"password"`
	PasswordFromEnv string `mapstructure:"password_from_env"`
	APIKey          string `mapstructure:"api_key"`
	APIKeyFromEnv   string `mapstructure:"api_key_from_env"`
}

// LogSourceTLSConfig configures the transport to the log store.
type LogSourceTLSConfig struct {
	CAFile             string `mapstructure:"ca_file"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

// LogStreamConfig describes where one stream of one source lives. Index and
// selector values accept the {{ .Cluster }} and {{ .Host }} substitutions.
type LogStreamConfig struct {
	Index    string            `mapstructure:"index"`
	Selector map[string]string `mapstructure:"selector"`
	FieldMap LogFieldMapConfig `mapstructure:"field_map"`
}

// LogFieldMapConfig names the index fields carrying each role. A preset fills
// the roles of a known log format; every field overrides the preset.
type LogFieldMapConfig struct {
	Preset    string `mapstructure:"preset"`
	Timestamp string `mapstructure:"timestamp"`
	Severity  string `mapstructure:"severity"`
	Text      string `mapstructure:"text"`
	Host      string `mapstructure:"host"`
	Database  string `mapstructure:"database"`
	User      string `mapstructure:"user"`
	PID       string `mapstructure:"pid"`
	// Mask lists free-text fields sanitized before they leave the backend.
	Mask []string `mapstructure:"mask"`
	// KeywordFields maps a field to the field an exact-match filter must use
	// when the store indexes it as analyzed text, e.g.
	// error_severity: error_severity.keyword.
	KeywordFields map[string]string `mapstructure:"keyword_fields"`
	// Severities overrides the accepted severity values of the preset.
	Severities []string `mapstructure:"severities"`
	// HostMatch is exact or suffix; suffix matches a short host name against an
	// FQDN stored in the index.
	HostMatch string `mapstructure:"host_match"`
}

// Defaults for LogSearchConfig when values are unset (<= 0).
const (
	DefaultLogSearchMaxScan        = 5000
	DefaultLogSearchMaxPageSize    = 1000
	DefaultLogSearchTimeoutSeconds = 30
	DefaultLogSourceBatchSize      = 1000
	DefaultLogSourceMaxBoundaryIDs = 10000
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

	if len(c.Sources) > 0 {
		sources := make(map[string]LogSourceConfig, len(c.Sources))
		for name, src := range c.Sources {
			sources[name] = src.withDefaults(c)
		}

		c.Sources = sources
	}

	return c
}

func (s LogSourceConfig) withDefaults(parent LogSearchConfig) LogSourceConfig {
	if s.BatchSize <= 0 {
		s.BatchSize = DefaultLogSourceBatchSize
	}

	if s.MaxBoundaryIDs <= 0 {
		s.MaxBoundaryIDs = DefaultLogSourceMaxBoundaryIDs
	}

	if s.RateLimit == nil {
		rl := *parent.RateLimit
		s.RateLimit = &rl
	}

	if s.AdminRateLimit == nil {
		rl := *parent.AdminRateLimit
		s.AdminRateLimit = &rl
	}

	if s.Auth.Kind == "" {
		s.Auth.Kind = LogAuthNone
	}

	return s
}

// Validate checks the structure of the configured log sources and that every
// cluster referencing one names a source that exists. Credentials are read
// after the *_from_env variables have been resolved, so a missing secret fails
// here instead of as a 502 on the first search. Field maps are validated by the
// source registry, which owns the presets.
func (c LogSearchConfig) Validate(clusters []Cluster) error {
	if c.DefaultSource != "" {
		if _, ok := c.Sources[c.DefaultSource]; !ok {
			return fmt.Errorf("default_source %q is not defined in sources", c.DefaultSource)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(c.Sources)) {
		if err := validateLogSource(name, c.Sources[name]); err != nil {
			return err
		}
	}

	for _, cl := range clusters {
		if cl.LogSource == "" || cl.LogSource == SourceYandexMDB {
			continue
		}

		if _, ok := c.Sources[cl.LogSource]; !ok {
			return fmt.Errorf("clusters.%s: log_source %q is not defined in log_search.sources",
				cl.Name, cl.LogSource)
		}
	}

	return nil
}

func validateLogSource(name string, src LogSourceConfig) error {
	// The Yandex MDB source is built in, not configurable: a source under
	// that name would silently replace it in the registry.
	if name == SourceYandexMDB {
		return fmt.Errorf("sources.%s: name is reserved for the built-in source", name)
	}

	if src.Type != LogSourceTypeOpenSearch {
		return fmt.Errorf("sources.%s: unknown type %q", name, src.Type)
	}

	if len(src.Addresses) == 0 {
		return fmt.Errorf("sources.%s: addresses must not be empty", name)
	}

	if len(src.Streams) == 0 {
		return fmt.Errorf("sources.%s: at least one stream must be configured", name)
	}

	if err := src.Auth.validate(name); err != nil {
		return err
	}

	if src.Auth.Kind == LogAuthBasic || src.Auth.Kind == LogAuthAPIKey {
		for _, addr := range src.Addresses {
			if !strings.HasPrefix(strings.ToLower(addr), "https://") {
				return fmt.Errorf("sources.%s: address %q must use https with auth.kind %q", name, addr, src.Auth.Kind)
			}
		}
	}

	for _, stream := range slices.Sorted(maps.Keys(src.Streams)) {
		if stream != LogStreamPostgreSQL && stream != LogStreamPooler {
			return fmt.Errorf("sources.%s.streams.%s: unknown stream (want %s|%s)",
				name, stream, LogStreamPostgreSQL, LogStreamPooler)
		}

		if src.Streams[stream].Index == "" {
			return fmt.Errorf("sources.%s.streams.%s: index must not be empty", name, stream)
		}
	}

	return nil
}

func (a LogSourceAuthConfig) validate(name string) error {
	switch a.Kind {
	case LogAuthNone:
		return nil
	case LogAuthBasic:
		if a.User == "" {
			return fmt.Errorf("sources.%s: auth.kind %q requires auth.user", name, a.Kind)
		}

		if a.Password == "" {
			return fmt.Errorf("sources.%s: auth.kind %q requires auth.password (or a %s that is set)",
				name, a.Kind, cmp.Or(a.PasswordFromEnv, "password_from_env"))
		}
	case LogAuthAPIKey:
		if a.APIKey == "" {
			return fmt.Errorf("sources.%s: auth.kind %q requires auth.api_key (or a %s that is set)",
				name, a.Kind, cmp.Or(a.APIKeyFromEnv, "api_key_from_env"))
		}
	default:
		return fmt.Errorf("sources.%s: unknown auth.kind %q (want none|basic|api_key)", name, a.Kind)
	}

	return nil
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

	// SchemaLint tunes the schema checks: which run, which schemas to ignore,
	// sequence thresholds and how long a report stays cached.
	SchemaLint schemalint.Config `mapstructure:"schema_lint"`

	// IndexAdvisor tunes the index candidate report: how much of the
	// pg_stat_statements top is analyzed and what a candidate may look like.
	IndexAdvisor indexadvisor.Config `mapstructure:"index_advisor"`
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
	// UpdateSource replaces the clusters published by one discovery source,
	// keeping static ones and the other sources untouched. It returns the names
	// it refused to publish because another owner already holds them.
	UpdateSource(source string, discovered []Cluster) []string
}

// ClustersFromConfig stores static + discovery clusters with thread-safe access.
// Discovered clusters are kept per source (the discovery entry name), so one
// source's refresh cycle cannot drop what another source published.
type ClustersFromConfig struct {
	mu         sync.RWMutex
	static     []Cluster
	discovered map[string][]Cluster
}

func NewClustersFromConfig(cfg Config) Clusters {
	// Tag static clusters with source.
	for i := range cfg.Clusters {
		if cfg.Clusters[i].Source == "" {
			cfg.Clusters[i].Source = "static"
		}
	}

	return &ClustersFromConfig{ //nolint:exhaustruct
		static:     cfg.Clusters,
		discovered: make(map[string][]Cluster),
	}
}

func (c *ClustersFromConfig) Get(_ context.Context) ([]Cluster, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := len(c.static)
	for _, cls := range c.discovered {
		total += len(cls)
	}

	all := make([]Cluster, 0, total)
	all = append(all, c.static...)

	// Sources in sorted order: the cluster list reaches the API in this order,
	// so it must not depend on map iteration.
	for _, source := range slices.Sorted(maps.Keys(c.discovered)) {
		all = append(all, c.discovered[source]...)
	}

	return all, nil
}

// UpdateSource replaces the clusters published by source. Names already taken by
// a static cluster or by another source are dropped and returned: two clusters
// sharing a name would silently merge into one set of pools downstream.
func (c *ClustersFromConfig) UpdateSource(source string, discovered []Cluster) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	taken := make(map[ClusterName]bool, len(c.static))
	for i := range c.static {
		taken[c.static[i].Name] = true
	}

	for key, cls := range c.discovered {
		if key == source {
			continue
		}

		for i := range cls {
			taken[cls[i].Name] = true
		}
	}

	var rejected []string

	accepted := make([]Cluster, 0, len(discovered))

	for _, cl := range discovered {
		if taken[cl.Name] {
			rejected = append(rejected, cl.Name.String())

			continue
		}

		taken[cl.Name] = true

		accepted = append(accepted, cl)
	}

	if c.discovered == nil {
		c.discovered = make(map[string][]Cluster, 1)
	}

	c.discovered[source] = accepted

	return rejected
}
