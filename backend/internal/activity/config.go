package activity

import (
	"fmt"
	"time"
)

const (
	DefaultWindowMinutes = 60
	DefaultSampleTimeout = time.Second
	DefaultMaxSessions   = 2000

	minWindowMinutes = 5
	maxWindowMinutes = 240
	minSampleTimeout = 200 * time.Millisecond
	maxSampleTimeout = 10 * time.Second
	minMaxSessions   = 100
	maxMaxSessions   = 20000
)

type Config struct {
	// WindowMinutes is how much history the page keeps in the browser.
	WindowMinutes int           `mapstructure:"window_minutes"`
	SampleTimeout time.Duration `mapstructure:"sample_timeout"`
	MaxSessions   int           `mapstructure:"max_sessions"`
}

func (c Config) WithDefaults() Config {
	if c.WindowMinutes == 0 {
		c.WindowMinutes = DefaultWindowMinutes
	}

	if c.SampleTimeout == 0 {
		c.SampleTimeout = DefaultSampleTimeout
	}

	if c.MaxSessions == 0 {
		c.MaxSessions = DefaultMaxSessions
	}

	return c
}

func (c Config) Validate() error {
	if c.WindowMinutes < minWindowMinutes || c.WindowMinutes > maxWindowMinutes {
		return fmt.Errorf("window_minutes must be between %d and %d, got %d",
			minWindowMinutes, maxWindowMinutes, c.WindowMinutes)
	}

	if c.SampleTimeout < minSampleTimeout || c.SampleTimeout > maxSampleTimeout {
		return fmt.Errorf("sample_timeout must be between %s and %s, got %s",
			minSampleTimeout, maxSampleTimeout, c.SampleTimeout)
	}

	if c.MaxSessions < minMaxSessions || c.MaxSessions > maxMaxSessions {
		return fmt.Errorf("max_sessions must be between %d and %d, got %d",
			minMaxSessions, maxMaxSessions, c.MaxSessions)
	}

	return nil
}
