// Package postgres discovers the databases of a PostgreSQL cluster by querying
// the cluster itself.
package postgres

import (
	"errors"
	"time"

	"github.com/dbulashev/dasha/internal/config"
)

const (
	defaultPort        = "5432"
	defaultBootstrapDB = "postgres"
)

var (
	errHostsRequired = errors.New("hosts is required")
	errUserRequired  = errors.New("user is required")
	errEmptyHost     = errors.New("hosts contains an empty entry")
)

// Config holds the settings of one `type: postgres` discovery entry.
type Config struct {
	Hosts           []string `mapstructure:"hosts"`
	Port            string   `mapstructure:"port"`
	User            string   `mapstructure:"user"`
	Password        string   `mapstructure:"password"`
	PasswordFromEnv string   `mapstructure:"password_from_env"`
	// BootstrapDB is the database the discovery query connects to; it need not
	// be one of the databases Dasha ends up monitoring.
	BootstrapDB     string        `mapstructure:"bootstrap_db"`
	RefreshInterval time.Duration `mapstructure:"refresh_interval"` // bare number: minutes
	Db              *string       `mapstructure:"db"`
	ExcludeDb       *string       `mapstructure:"exclude_db"`
}

// withDefaults fills unset fields and resolves the password from the environment.
func (c Config) withDefaults() Config {
	if c.Port == "" {
		c.Port = defaultPort
	}

	if c.BootstrapDB == "" {
		c.BootstrapDB = defaultBootstrapDB
	}

	c.Password, _ = config.ResolveSecret(c.Password, c.PasswordFromEnv)

	return c
}

func (c Config) validate() error {
	if len(c.Hosts) == 0 {
		return errHostsRequired
	}

	for _, h := range c.Hosts {
		if h == "" {
			return errEmptyHost
		}
	}

	if c.User == "" {
		return errUserRequired
	}

	return nil
}
