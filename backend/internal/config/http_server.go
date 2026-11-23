package config

import (
	"fmt"
	"time"
)

const (
	DefaultHTTPServerReadTimeout       = 30 * time.Second
	DefaultHTTPServerReadHeaderTimeout = 10 * time.Second
	DefaultHTTPServerWriteTimeout      = 30 * time.Second
	DefaultHTTPServerIdleTimeout       = 30 * time.Second

	minHTTPServerTimeout = time.Second
	maxHTTPServerTimeout = 10 * time.Minute
)

// HTTPServerConfig bounds the API server's connections. A long page (a bloated
// pg_stat_statements) needs both read_timeout and write_timeout raised: when
// the read deadline passes, net/http cancels the request context.
type HTTPServerConfig struct {
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
}

// WithDefaults leaves negative values for Validate to reject.
func (c HTTPServerConfig) WithDefaults() HTTPServerConfig {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = DefaultHTTPServerReadTimeout
	}

	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = DefaultHTTPServerReadHeaderTimeout
	}

	if c.WriteTimeout == 0 {
		c.WriteTimeout = DefaultHTTPServerWriteTimeout
	}

	if c.IdleTimeout == 0 {
		c.IdleTimeout = DefaultHTTPServerIdleTimeout
	}

	return c
}

func (c HTTPServerConfig) Validate() error {
	for _, f := range []struct {
		name  string
		value time.Duration
	}{
		{"read_timeout", c.ReadTimeout},
		{"read_header_timeout", c.ReadHeaderTimeout},
		{"write_timeout", c.WriteTimeout},
		{"idle_timeout", c.IdleTimeout},
	} {
		if f.value < minHTTPServerTimeout || f.value > maxHTTPServerTimeout {
			return fmt.Errorf("http_server.%s must be between %s and %s, got %s",
				f.name, minHTTPServerTimeout, maxHTTPServerTimeout, f.value)
		}
	}

	return nil
}

// WriteCutsRead reports a write deadline that ends the response before the
// read deadline would cancel the request: the handler keeps running for a
// client that can no longer get the answer.
func (c HTTPServerConfig) WriteCutsRead() bool {
	return c.WriteTimeout < c.ReadTimeout
}
