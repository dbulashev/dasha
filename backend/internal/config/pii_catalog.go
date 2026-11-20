package config

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

const (
	DefaultPIICatalogSchedule         = "0 3 * * *"
	DefaultPIICatalogRetentionDays    = 90
	DefaultPIICatalogStatementTimeout = 30 * time.Second

	maxPIICatalogRetentionDays    = 3650
	minPIICatalogStatementTimeout = time.Second
	maxPIICatalogStatementTimeout = 5 * time.Minute
)

// PIICatalogConfig lives in dasha.yaml only, not in the UI: who may read the
// catalog is a deployment decision.
type PIICatalogConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// UTC unless prefixed with CRON_TZ=.
	Schedule         string        `mapstructure:"schedule"`
	RetentionDays    int           `mapstructure:"retention_days"`
	StatementTimeout time.Duration `mapstructure:"statement_timeout"`
	// LIKE patterns, case-sensitive.
	ExcludeSchemas []string `mapstructure:"exclude_schemas"`
	// auth.tokens names; PATs and sessions are never allowed.
	AllowedTokens []string `mapstructure:"allowed_tokens"`
}

// WithDefaults leaves negative values for Validate to reject.
func (c PIICatalogConfig) WithDefaults() PIICatalogConfig {
	if c.Schedule == "" {
		c.Schedule = DefaultPIICatalogSchedule
	}

	if c.RetentionDays == 0 {
		c.RetentionDays = DefaultPIICatalogRetentionDays
	}

	if c.StatementTimeout == 0 {
		c.StatementTimeout = DefaultPIICatalogStatementTimeout
	}

	return c
}

// Validate runs in every process (server, autosnapshot, migrate), even when
// disabled. Token secrets are checked by ValidateSecrets: only the server has them.
//
// An unknown allowed_tokens entry is never echoed: it may be a pasted secret.
func (c *PIICatalogConfig) Validate(auth AuthConfig) error {
	sched, err := cronsched.Parse(c.Schedule)
	if err != nil {
		return fmt.Errorf("pii_catalog.schedule: invalid cron expression %q: %w", c.Schedule, err)
	}

	// HasGap reports no gap for a schedule that never fires.
	if sched.Next(time.Now()).IsZero() {
		return fmt.Errorf("pii_catalog.schedule %q never fires", c.Schedule)
	}

	if c.RetentionDays < 1 || c.RetentionDays > maxPIICatalogRetentionDays {
		return fmt.Errorf("pii_catalog.retention_days must be between 1 and %d", maxPIICatalogRetentionDays)
	}

	// Retention treats a database not attempted for retention_days as gone from
	// the config and deletes its catalog.
	if cronsched.HasGap(sched, time.Duration(c.RetentionDays)*24*time.Hour) {
		return fmt.Errorf("pii_catalog.schedule %q must run more often than every retention_days (%d)",
			c.Schedule, c.RetentionDays)
	}

	if c.StatementTimeout < minPIICatalogStatementTimeout || c.StatementTimeout > maxPIICatalogStatementTimeout {
		return fmt.Errorf("pii_catalog.statement_timeout must be between %s and %s",
			minPIICatalogStatementTimeout, maxPIICatalogStatementTimeout)
	}

	for i, pattern := range c.ExcludeSchemas {
		if pattern == "" {
			return fmt.Errorf("pii_catalog.exclude_schemas[%d] must not be empty", i)
		}

		// PostgreSQL rejects it only at query time.
		if trailing := len(pattern) - len(strings.TrimRight(pattern, `\`)); trailing%2 == 1 {
			return fmt.Errorf(`pii_catalog.exclude_schemas[%d] %q must not end with an unescaped "\"`, i, pattern)
		}
	}

	if len(c.AllowedTokens) == 0 {
		return nil
	}

	if auth.Mode == AuthModeNone || auth.Mode == "" {
		return fmt.Errorf("pii_catalog.allowed_tokens requires auth.mode token or oidc")
	}

	seen := make(map[string]struct{}, len(c.AllowedTokens))

	for i, name := range c.AllowedTokens {
		if err := validateAllowedToken(i, name, auth.Tokens); err != nil {
			return err
		}

		// After validateAllowedToken: by now name is a token name, safe to echo.
		// A repeat is usually a copy-paste that left another token out.
		if _, dup := seen[name]; dup {
			return fmt.Errorf("pii_catalog.allowed_tokens[%d]: %q is already listed", i, name)
		}

		seen[name] = struct{}{}
	}

	return nil
}

// ValidateSecrets is for the API server only, after Validate.
func (c *PIICatalogConfig) ValidateSecrets(auth AuthConfig) error {
	for i, name := range c.AllowedTokens {
		for _, t := range auth.Tokens {
			if t.Name == name && t.Token == "" {
				return fmt.Errorf("pii_catalog.allowed_tokens[%d]: token %q has no secret (token / token_from_env)", i, name)
			}
		}
	}

	return nil
}

func validateAllowedToken(i int, name string, tokens []AuthToken) error {
	if name == "" {
		return fmt.Errorf("pii_catalog.allowed_tokens[%d] must not be empty", i)
	}

	matches := 0

	for _, t := range tokens {
		if t.Name == name {
			matches++
		}
	}

	switch {
	case matches == 0 && looksLikeTokenSecret(name, tokens):
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: looks like a token secret, use its name", i)
	case matches == 0:
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: no auth.tokens entry with this name", i)
	case matches > 1:
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: auth.tokens has %d entries named %q", i, matches, name)
	}

	return nil
}

func looksLikeTokenSecret(value string, tokens []AuthToken) bool {
	hit := 0

	for _, t := range tokens {
		if t.Token != "" {
			hit |= subtle.ConstantTimeCompare([]byte(value), []byte(t.Token))
		}
	}

	return hit == 1
}
