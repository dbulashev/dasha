// Package postgres discovers the databases of a PostgreSQL cluster by querying
// the cluster itself.
package postgres

import (
	"errors"
	"os"
	"time"
)

const (
	defaultPort            = "5432"
	defaultBootstrapDB     = "postgres"
	defaultRefreshInterval = 5 * time.Minute
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
	BootstrapDB     string  `mapstructure:"bootstrap_db"`
	RefreshInterval int     `mapstructure:"refresh_interval"` // minutes
	Db              *string `mapstructure:"db"`
	ExcludeDb       *string `mapstructure:"exclude_db"`
}

// withDefaults fills unset fields and resolves the password from the environment.
func (c Config) withDefaults() Config {
	if c.Port == "" {
		c.Port = defaultPort
	}

	if c.BootstrapDB == "" {
		c.BootstrapDB = defaultBootstrapDB
	}

	if c.PasswordFromEnv != "" {
		c.Password = os.Getenv(c.PasswordFromEnv)
	}

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

// interval returns how often the databases are re-listed. The unit is minutes,
// so any configured value is already at or above the sane lower bound.
func (c Config) interval() time.Duration {
	if c.RefreshInterval <= 0 {
		return defaultRefreshInterval
	}

	return time.Duration(c.RefreshInterval) * time.Minute
}
