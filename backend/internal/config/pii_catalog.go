package config

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/pkg/cronsched"
)

const (
	DefaultPIICatalogSchedule           = "0 3 * * *"
	DefaultPIICatalogRetentionDays      = 90
	DefaultPIICatalogAuditRetentionDays = 365
	DefaultPIICatalogStatementTimeout   = 30 * time.Second

	maxPIICatalogRetentionDays    = 3650
	minPIICatalogStatementTimeout = time.Second
	maxPIICatalogStatementTimeout = 5 * time.Minute
)

// PIICatalogConfig configures the catalog of (column name, column comment)
// pairs collected from monitored databases and served to an external PII
// classifier. Unlike the autosnapshot settings it lives in dasha.yaml only:
// who may read the catalog is a deployment decision, not a UI one.
type PIICatalogConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Schedule is a 5-field cron expression, UTC unless prefixed with CRON_TZ=.
	Schedule string `mapstructure:"schedule"`
	// RetentionDays bounds catalog versions, removed pairs, the horizon of the
	// changes feed and the data of databases that are no longer collected.
	RetentionDays int `mapstructure:"retention_days"`
	// AuditRetentionDays bounds the catalog access log.
	AuditRetentionDays int `mapstructure:"audit_retention_days"`
	// StatementTimeout bounds one catalog query on a monitored database.
	StatementTimeout time.Duration `mapstructure:"statement_timeout"`
	// ExcludeSchemas are LIKE patterns: % matches any string, _ any one
	// character, \ escapes a literal % or _. Matching is case-sensitive.
	ExcludeSchemas []string `mapstructure:"exclude_schemas"`
	// AllowedTokens names the static auth.tokens entries (by name, never by
	// secret) allowed to read the catalog. PATs and sessions never are.
	AllowedTokens []string `mapstructure:"allowed_tokens"`
}

// WithDefaults returns a copy with unset (<=0, empty) fields filled from defaults.
func (c PIICatalogConfig) WithDefaults() PIICatalogConfig {
	if c.Schedule == "" {
		c.Schedule = DefaultPIICatalogSchedule
	}

	if c.RetentionDays <= 0 {
		c.RetentionDays = DefaultPIICatalogRetentionDays
	}

	if c.AuditRetentionDays <= 0 {
		c.AuditRetentionDays = DefaultPIICatalogAuditRetentionDays
	}

	if c.StatementTimeout == 0 {
		c.StatementTimeout = DefaultPIICatalogStatementTimeout
	}

	return c
}

// Validate checks the section and its link to auth. It runs whether or not the
// catalog is enabled, so a broken section fails the deploy that introduces it
// rather than the one that turns the feature on. auth must already have its
// token_from_env secrets resolved.
//
// An allowed_tokens entry is never echoed back when it matches a token secret:
// the names are not secret, but a secret pasted in place of a name is.
func (c *PIICatalogConfig) Validate(auth AuthConfig) error {
	if _, err := cronsched.Parse(c.Schedule); err != nil {
		return fmt.Errorf("pii_catalog.schedule: invalid cron expression %q: %w", c.Schedule, err)
	}

	if c.RetentionDays < 1 || c.RetentionDays > maxPIICatalogRetentionDays {
		return fmt.Errorf("pii_catalog.retention_days must be between 1 and %d", maxPIICatalogRetentionDays)
	}

	if c.AuditRetentionDays < 1 || c.AuditRetentionDays > maxPIICatalogRetentionDays {
		return fmt.Errorf("pii_catalog.audit_retention_days must be between 1 and %d", maxPIICatalogRetentionDays)
	}

	if c.StatementTimeout < minPIICatalogStatementTimeout || c.StatementTimeout > maxPIICatalogStatementTimeout {
		return fmt.Errorf("pii_catalog.statement_timeout must be between %s and %s",
			minPIICatalogStatementTimeout, maxPIICatalogStatementTimeout)
	}

	for i, pattern := range c.ExcludeSchemas {
		if pattern == "" {
			return fmt.Errorf("pii_catalog.exclude_schemas[%d] must not be empty", i)
		}

		// PostgreSQL rejects a LIKE pattern ending in a lone escape character
		// at query time; catch it at startup instead of on every collection.
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

	for i, name := range c.AllowedTokens {
		if err := validateAllowedToken(i, name, auth.Tokens); err != nil {
			return err
		}
	}

	return nil
}

func validateAllowedToken(i int, name string, tokens []AuthToken) error {
	if name == "" {
		return fmt.Errorf("pii_catalog.allowed_tokens[%d] must not be empty", i)
	}

	var (
		found   *AuthToken
		matches int
	)

	for j := range tokens {
		if tokens[j].Name == name {
			found = &tokens[j]
			matches++
		}
	}

	switch {
	case matches == 0 && looksLikeTokenSecret(name, tokens):
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: looks like a token secret, use its name", i)
	case matches == 0:
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: no auth.tokens entry named %q", i, name)
	case matches > 1:
		// Two secrets behind one name: the gate could not tell which is allowed.
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: auth.tokens has %d entries named %q", i, matches, name)
	case found.Token == "":
		return fmt.Errorf("pii_catalog.allowed_tokens[%d]: token %q has no secret (token / token_from_env)", i, name)
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

// DuplicateAllowedTokens returns the names listed in allowed_tokens more than
// once, each once, in first-seen order. Harmless, so a warning, not an error.
func (c PIICatalogConfig) DuplicateAllowedTokens() []string {
	seen := make(map[string]int, len(c.AllowedTokens))

	var dups []string

	for _, name := range c.AllowedTokens {
		seen[name]++
		if seen[name] == 2 { //nolint:mnd // second sighting = first duplicate
			dups = append(dups, name)
		}
	}

	return dups
}
