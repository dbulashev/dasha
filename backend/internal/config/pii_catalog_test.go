package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPIICatalogConfig_WithDefaults(t *testing.T) {
	got := PIICatalogConfig{}.WithDefaults() //nolint:exhaustruct

	assert.Equal(t, DefaultPIICatalogSchedule, got.Schedule)
	assert.Equal(t, DefaultPIICatalogRetentionDays, got.RetentionDays)
	assert.Equal(t, DefaultPIICatalogStatementTimeout, got.StatementTimeout)

	got = PIICatalogConfig{ //nolint:exhaustruct
		Schedule: "*/5 * * * *", RetentionDays: 7, StatementTimeout: time.Minute,
	}.WithDefaults()
	assert.Equal(t, "*/5 * * * *", got.Schedule)
	assert.Equal(t, 7, got.RetentionDays)
	assert.Equal(t, time.Minute, got.StatementTimeout)

	neg := PIICatalogConfig{RetentionDays: -5}.WithDefaults() //nolint:exhaustruct
	assert.Equal(t, -5, neg.RetentionDays)

	// An absent section must not fail startup.
	zero := PIICatalogConfig{}.WithDefaults()       //nolint:exhaustruct
	require.NoError(t, zero.Validate(AuthConfig{})) //nolint:exhaustruct
}

func TestPIICatalogConfig_Validate(t *testing.T) {
	const secret = "s3cr3t-classifier-token"

	tokenAuth := AuthConfig{ //nolint:exhaustruct
		Mode: AuthModeToken,
		Tokens: []AuthToken{
			{Name: "pii-classifier", Token: secret, Role: RoleViewer},                //nolint:exhaustruct
			{Name: "ops", Token: "ops-secret", Role: RoleAdmin},                      //nolint:exhaustruct
			{Name: "dup", Token: "a", Role: RoleViewer},                              //nolint:exhaustruct
			{Name: "dup", Token: "b", Role: RoleViewer},                              //nolint:exhaustruct
			{Name: "unset", TokenFromEnv: "DASHA_UNSET_PII_TOKEN", Role: RoleViewer}, //nolint:exhaustruct
		},
	}

	oidcAuth := tokenAuth
	oidcAuth.Mode = AuthModeOIDC

	noSecretAuth := AuthConfig{ //nolint:exhaustruct
		Mode: AuthModeToken,
		Tokens: []AuthToken{
			{Name: "pii-classifier", TokenFromEnv: "DASHA_PII_TOKEN", Role: RoleViewer}, //nolint:exhaustruct
		},
	}

	base := PIICatalogConfig{}.WithDefaults() //nolint:exhaustruct

	tests := []struct {
		name    string
		mutate  func(c *PIICatalogConfig)
		auth    AuthConfig
		wantErr string
	}{
		{name: "defaults", mutate: func(*PIICatalogConfig) {}, auth: tokenAuth},
		{
			name:   "allowed static token, token mode",
			mutate: func(c *PIICatalogConfig) { c.AllowedTokens = []string{"pii-classifier"} },
			auth:   tokenAuth,
		},
		{
			name:   "allowed static token, oidc mode",
			mutate: func(c *PIICatalogConfig) { c.AllowedTokens = []string{"pii-classifier", "ops"} },
			auth:   oidcAuth,
		},
		{
			name:   "duplicate allowed name is not an error",
			mutate: func(c *PIICatalogConfig) { c.AllowedTokens = []string{"ops", "ops"} },
			auth:   tokenAuth,
		},
		{
			name:    "bad cron",
			mutate:  func(c *PIICatalogConfig) { c.Schedule = "bad" },
			auth:    tokenAuth,
			wantErr: `pii_catalog.schedule: invalid cron expression "bad"`,
		},
		{
			name:    "four-field cron",
			mutate:  func(c *PIICatalogConfig) { c.Schedule = "0 3 * *" },
			auth:    tokenAuth,
			wantErr: "pii_catalog.schedule",
		},
		{
			name:    "schedule that never fires",
			mutate:  func(c *PIICatalogConfig) { c.Schedule = "0 0 31 2 *" },
			auth:    tokenAuth,
			wantErr: `pii_catalog.schedule "0 0 31 2 *" never fires`,
		},
		{
			name:    "monthly schedule outlives retention",
			mutate:  func(c *PIICatalogConfig) { c.Schedule = "0 3 1 * *"; c.RetentionDays = 14 },
			auth:    tokenAuth,
			wantErr: `pii_catalog.schedule "0 3 1 * *" must run more often than every retention_days (14)`,
		},
		{
			name:    "daily schedule with one-day retention",
			mutate:  func(c *PIICatalogConfig) { c.RetentionDays = 1 },
			auth:    tokenAuth,
			wantErr: "must run more often than every retention_days (1)",
		},
		{
			name:   "weekly schedule within retention",
			mutate: func(c *PIICatalogConfig) { c.Schedule = "0 3 * * 0"; c.RetentionDays = 8 },
			auth:   tokenAuth,
		},
		{
			name:    "retention too long",
			mutate:  func(c *PIICatalogConfig) { c.RetentionDays = 3651 },
			auth:    tokenAuth,
			wantErr: "pii_catalog.retention_days must be between 1 and 3650",
		},
		{
			name:    "negative retention",
			mutate:  func(c *PIICatalogConfig) { c.RetentionDays = -5 },
			auth:    tokenAuth,
			wantErr: "pii_catalog.retention_days must be between 1 and 3650",
		},
		{
			name:    "statement timeout too short",
			mutate:  func(c *PIICatalogConfig) { c.StatementTimeout = 500 * time.Millisecond },
			auth:    tokenAuth,
			wantErr: "pii_catalog.statement_timeout must be between 1s and 5m0s",
		},
		{
			name:    "statement timeout too long",
			mutate:  func(c *PIICatalogConfig) { c.StatementTimeout = 6 * time.Minute },
			auth:    tokenAuth,
			wantErr: "pii_catalog.statement_timeout must be between",
		},
		{
			name:    "negative statement timeout",
			mutate:  func(c *PIICatalogConfig) { c.StatementTimeout = -time.Second },
			auth:    tokenAuth,
			wantErr: "pii_catalog.statement_timeout must be between",
		},
		{
			name:   "exclude patterns",
			mutate: func(c *PIICatalogConfig) { c.ExcludeSchemas = []string{"archive%", `tmp\_%`, `odd\\`} },
			auth:   tokenAuth,
		},
		{
			name:    "empty exclude pattern",
			mutate:  func(c *PIICatalogConfig) { c.ExcludeSchemas = []string{"archive%", ""} },
			auth:    tokenAuth,
			wantErr: "pii_catalog.exclude_schemas[1] must not be empty",
		},
		{
			name:    "exclude pattern ending in escape",
			mutate:  func(c *PIICatalogConfig) { c.ExcludeSchemas = []string{`tmp\`} },
			auth:    tokenAuth,
			wantErr: `pii_catalog.exclude_schemas[0] "tmp\\" must not end with an unescaped "\"`,
		},
		{
			name:    "allowed tokens in mode none",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{"pii-classifier"} },
			auth:    AuthConfig{Tokens: tokenAuth.Tokens}, //nolint:exhaustruct
			wantErr: "pii_catalog.allowed_tokens requires auth.mode token or oidc",
		},
		{
			name:    "allowed tokens in explicit mode none",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{"pii-classifier"} },
			auth:    AuthConfig{Mode: AuthModeNone, Tokens: tokenAuth.Tokens}, //nolint:exhaustruct
			wantErr: "pii_catalog.allowed_tokens requires auth.mode token or oidc",
		},
		{
			name:    "unknown name",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{"ops", "pii-clasifier"} },
			auth:    tokenAuth,
			wantErr: "pii_catalog.allowed_tokens[1]: no auth.tokens entry with this name",
		},
		{
			name:    "empty name",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{""} },
			auth:    tokenAuth,
			wantErr: "pii_catalog.allowed_tokens[0] must not be empty",
		},
		{
			name:    "duplicate auth.tokens name",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{"dup"} },
			auth:    tokenAuth,
			wantErr: `pii_catalog.allowed_tokens[0]: auth.tokens has 2 entries named "dup"`,
		},
		{
			// autosnapshot/migrate have no secret and must still start.
			name:   "token without secret here",
			mutate: func(c *PIICatalogConfig) { c.AllowedTokens = []string{"unset"} },
			auth:   tokenAuth,
		},
		{
			name:    "secret in place of name",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{secret} },
			auth:    tokenAuth,
			wantErr: "pii_catalog.allowed_tokens[0]: looks like a token secret, use its name",
		},
		{
			name:    "secret in place of name, secret not resolved here",
			mutate:  func(c *PIICatalogConfig) { c.AllowedTokens = []string{secret} },
			auth:    noSecretAuth,
			wantErr: "pii_catalog.allowed_tokens[0]: no auth.tokens entry with this name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			c.ExcludeSchemas = nil
			c.AllowedTokens = nil
			tt.mutate(&c)

			err := c.Validate(tt.auth)
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, err.Error(), secret, "a token secret must never reach the error text")
			assert.NotContains(t, err.Error(), "pii-clasifier", "an unmatched entry must never reach the error text")
		})
	}
}

func TestPIICatalogConfig_ValidateSecrets(t *testing.T) {
	auth := AuthConfig{ //nolint:exhaustruct
		Mode: AuthModeToken,
		Tokens: []AuthToken{
			{Name: "pii-classifier", Token: "s3cr3t", Role: RoleViewer},              //nolint:exhaustruct
			{Name: "unset", TokenFromEnv: "DASHA_UNSET_PII_TOKEN", Role: RoleViewer}, //nolint:exhaustruct
		},
	}

	c := PIICatalogConfig{AllowedTokens: []string{"pii-classifier"}} //nolint:exhaustruct
	require.NoError(t, c.ValidateSecrets(auth))

	c.AllowedTokens = []string{"pii-classifier", "unset"}
	require.EqualError(t, c.ValidateSecrets(auth),
		`pii_catalog.allowed_tokens[1]: token "unset" has no secret (token / token_from_env)`)

	c.AllowedTokens = []string{"pii-classifier"}
	require.NoError(t, c.ValidateSecrets(auth))
}

func TestPIICatalogConfig_DuplicateAllowedTokens(t *testing.T) {
	c := PIICatalogConfig{AllowedTokens: []string{"a", "b", "a", "c", "a", "b"}} //nolint:exhaustruct
	assert.Equal(t, []string{"a", "b"}, c.DuplicateAllowedTokens())

	c.AllowedTokens = []string{"a", "b"}
	assert.Empty(t, c.DuplicateAllowedTokens())
}
