package config

import (
	"context"
	"sync"
)

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
	Name      ClusterName
	UserName  string
	Password  string
	Port      string
	Databases []Database
	Hosts     []Host

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
