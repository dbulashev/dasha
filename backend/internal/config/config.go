package config

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	IssuerURL           string   `mapstructure:"issuer_url"`
	ClientID            string   `mapstructure:"client_id"`
	ClientSecret        string   `mapstructure:"client_secret"`
	ClientSecretFromEnv string   `mapstructure:"client_secret_from_env"`
	Scopes              []string `mapstructure:"scopes"`
	RedirectURL         string   `mapstructure:"redirect_url"`
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

// Config is the top-level application configuration.
type Config struct {
	Debug     bool                      `mapstructure:"debug"`
	Clusters  []Cluster                 `mapstructure:"clusters"`
	Discovery map[string]DiscoveryEntry `mapstructure:"discovery"`
	Auth      AuthConfig                `mapstructure:"auth"`

	// PgStatsView is an optional custom view name to use instead of pg_catalog.pg_stats.
	// Useful when the connecting user lacks privileges to read pg_catalog.pg_stats
	// but a DBA has created an accessible view (e.g. "monitoring.pg_stats").
	// If empty, pg_catalog.pg_stats is used by default.
	PgStatsView string `mapstructure:"pg_stats_view"`

	// EnableQueryStatsReset allows resetting pg_stat_statements statistics via the UI.
	// Disabled by default for safety.
	EnableQueryStatsReset bool `mapstructure:"enable_query_stats_reset"`
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
