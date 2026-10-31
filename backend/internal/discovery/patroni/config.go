// Package patroni discovers PostgreSQL clusters from the keys Patroni keeps in
// its DCS (etcd v3 or Consul) and lists each cluster's databases.
package patroni

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/discovery/filter"
)

const (
	dcsEtcd3    = "etcd3"
	dcsConsul   = "consul"
	defaultNS   = "/service"
	defaultPGDB = "postgres"
	matchAll    = ".*"
)

var (
	errDCSRequired       = errors.New(`dcs is required: "etcd3" or "consul"`)
	errDCSUnknown        = errors.New(`unknown dcs, want "etcd3" or "consul"`)
	errEndpointsRequired = errors.New("etcd3.endpoints is required")
	errEmptyEndpoint     = errors.New("etcd3.endpoints contains an empty entry")
	errAddressRequired   = errors.New("consul.address is required")
	errUserRequired      = errors.New("user is required")
)

// Config holds the settings of one `type: patroni` discovery entry.
type Config struct {
	DCS             string        `mapstructure:"dcs"`
	Namespace       string        `mapstructure:"namespace"`
	Scope           string        `mapstructure:"scope"`
	ExcludeScope    *string       `mapstructure:"exclude_scope"`
	RefreshInterval time.Duration `mapstructure:"refresh_interval"` // bare number: minutes

	Etcd3  EtcdConfig   `mapstructure:"etcd3"`
	Consul ConsulConfig `mapstructure:"consul"`

	PGSettings `mapstructure:",squash"`

	Overrides map[string]PGOverride `mapstructure:"overrides"` // key: scope name
}

// PGSettings are the PostgreSQL settings shared by every scope of the entry.
type PGSettings struct {
	User            string  `mapstructure:"user"`
	Password        string  `mapstructure:"password"`
	PasswordFromEnv string  `mapstructure:"password_from_env"`
	Port            string  `mapstructure:"port"` // empty: port from conn_url
	BootstrapDB     string  `mapstructure:"bootstrap_db"`
	Db              *string `mapstructure:"db"`
	ExcludeDb       *string `mapstructure:"exclude_db"`
}

// PGOverride replaces individual PGSettings fields for one scope; nil keeps the entry value.
type PGOverride struct {
	User            *string `mapstructure:"user"`
	Password        *string `mapstructure:"password"`
	PasswordFromEnv *string `mapstructure:"password_from_env"`
	Port            *string `mapstructure:"port"`
	BootstrapDB     *string `mapstructure:"bootstrap_db"`
	Db              *string `mapstructure:"db"`
	ExcludeDb       *string `mapstructure:"exclude_db"`
}

// EtcdConfig is the etcd v3 connection.
type EtcdConfig struct {
	Endpoints       []string  `mapstructure:"endpoints"`
	Username        string    `mapstructure:"username"`
	Password        string    `mapstructure:"password"`
	PasswordFromEnv string    `mapstructure:"password_from_env"`
	TLS             TLSConfig `mapstructure:"tls"`
}

// ConsulConfig is the Consul HTTP API connection.
type ConsulConfig struct {
	Address      string    `mapstructure:"address"`    // http(s)://host:port
	Datacenter   string    `mapstructure:"datacenter"` // empty: the agent's DC
	Token        string    `mapstructure:"token"`
	TokenFromEnv string    `mapstructure:"token_from_env"`
	TLS          TLSConfig `mapstructure:"tls"`
}

func (c Config) withDefaults() Config {
	c.DCS = strings.ToLower(strings.TrimSpace(c.DCS))

	if c.Namespace == "" {
		c.Namespace = defaultNS
	}

	if c.Scope == "" {
		c.Scope = matchAll
	}

	return c
}

func (c Config) validate() error {
	switch c.DCS {
	case "":
		return errDCSRequired
	case dcsEtcd3:
		if len(c.Etcd3.Endpoints) == 0 {
			return errEndpointsRequired
		}

		for _, e := range c.Etcd3.Endpoints {
			if strings.TrimSpace(e) == "" {
				return errEmptyEndpoint
			}
		}
	case dcsConsul:
		if c.Consul.Address == "" {
			return errAddressRequired
		}
	default:
		return fmt.Errorf("%w: %q", errDCSUnknown, c.DCS)
	}

	if c.User == "" {
		return errUserRequired
	}

	return nil
}

// scopePG is the resolved PostgreSQL side of one scope.
type scopePG struct {
	User        string
	Password    string
	Port        string // empty: port from conn_url
	BootstrapDB string
	filter      *filter.Filter // db / exclude_db only
}

// merge applies an override field by field; password and password_from_env
// are one setting, an override of either replaces both.
func (s PGSettings) merge(o PGOverride) PGSettings {
	if o.User != nil {
		s.User = *o.User
	}

	if o.Password != nil || o.PasswordFromEnv != nil {
		s.Password = deref(o.Password)
		s.PasswordFromEnv = deref(o.PasswordFromEnv)
	}

	if o.Port != nil {
		s.Port = *o.Port
	}

	if o.BootstrapDB != nil {
		s.BootstrapDB = *o.BootstrapDB
	}

	if o.Db != nil {
		s.Db = o.Db
	}

	if o.ExcludeDb != nil {
		s.ExcludeDb = o.ExcludeDb
	}

	return s
}

// resolve reads the password from the environment and compiles the db filters.
// scope is empty for the entry-level settings.
func (s PGSettings) resolve(entry, scope string, logger *zap.Logger) (scopePG, error) {
	if s.User == "" {
		return scopePG{}, errUserRequired //nolint:exhaustruct
	}

	password := s.Password
	if s.PasswordFromEnv != "" {
		password = os.Getenv(s.PasswordFromEnv)

		if password == "" {
			logger.Warn("password_from_env is set but the variable is empty",
				zap.String("entry", entry),
				zap.String("scope", scope),
				zap.String("variable", s.PasswordFromEnv),
			)
		}
	}

	f, err := filter.New(matchAll, s.Db, nil, s.ExcludeDb)
	if err != nil {
		return scopePG{}, fmt.Errorf("compile filter: %w", err) //nolint:exhaustruct
	}

	bootstrap := s.BootstrapDB
	if bootstrap == "" {
		bootstrap = defaultPGDB
	}

	return scopePG{
		User:        s.User,
		Password:    password,
		Port:        s.Port,
		BootstrapDB: bootstrap,
		filter:      f,
	}, nil
}

// resolvePG builds the entry settings and one scopePG per override, keyed by
// lower-cased scope name: viper lower-cases map keys when it reads the file.
func resolvePG(entry string, cfg Config, logger *zap.Logger) (scopePG, map[string]scopePG, error) {
	base, err := cfg.PGSettings.resolve(entry, "", logger)
	if err != nil {
		return scopePG{}, nil, err //nolint:exhaustruct
	}

	overrides := make(map[string]scopePG, len(cfg.Overrides))

	for scope, o := range cfg.Overrides {
		pg, err := cfg.PGSettings.merge(o).resolve(entry, scope, logger)
		if err != nil {
			return scopePG{}, nil, fmt.Errorf("overrides.%s: %w", scope, err) //nolint:exhaustruct
		}

		overrides[strings.ToLower(scope)] = pg
	}

	return base, overrides, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// secret returns the value of the variable when env is set, else plain.
func secret(plain, env string) string {
	if env != "" {
		return os.Getenv(env)
	}

	return plain
}
