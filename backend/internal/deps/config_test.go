package deps

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
)

func TestBasePathEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "override", value: "/tools/dasha", want: "/tools/dasha"},
		{name: "empty preserves config", value: "", want: "/other"},
		{name: "explicit root", value: "/", want: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DASHA_BASE_PATH", tc.value)
			v := newConfigViper()
			v.SetConfigType("yaml")
			if err := v.ReadConfig(strings.NewReader("base_path: /other\n")); err != nil {
				t.Fatal(err)
			}
			var c config.Config
			if err := v.Unmarshal(&c); err != nil {
				t.Fatal(err)
			}
			if c.BasePath != tc.want {
				t.Fatalf("base_path = %q, want %q", c.BasePath, tc.want)
			}
		})
	}
}

func TestConfigViperKeepsDottedMapKeys(t *testing.T) {
	t.Parallel()

	const yml = `
log_search:
  sources:
    os:
      streams:
        postgresql:
          selector:
            "kubernetes.labels.app": pg
          field_map:
            host: host.name
            keyword_fields:
              "host.name": "host.name.keyword"
              "postgresql.severity": "postgresql.severity.keyword"
`

	v := newConfigViper()
	v.SetConfigType("yaml")

	if err := v.ReadConfig(strings.NewReader(yml)); err != nil {
		t.Fatalf("read: %v", err)
	}

	var c config.Config
	if err := v.Unmarshal(&c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	stream := c.LogSearch.Sources["os"].Streams["postgresql"]

	want := map[string]string{
		"host.name":           "host.name.keyword",
		"postgresql.severity": "postgresql.severity.keyword",
	}
	for k, w := range want {
		if got := stream.FieldMap.KeywordFields[k]; got != w {
			t.Errorf("keyword_fields[%q] = %q, want %q", k, got, w)
		}
	}

	if got := stream.Selector["kubernetes.labels.app"]; got != "pg" {
		t.Errorf("selector = %v", stream.Selector)
	}

	if got := stream.FieldMap.Host; got != "host.name" {
		t.Errorf("field_map.host = %q", got)
	}
}

func TestRoleMappingKeepsKeyCase(t *testing.T) {
	t.Parallel()

	const yml = `
auth:
  mode: oidc
  oidc:
    role_mapping:
      "DB.Admin": admin
      "dev.team": viewer
`

	v := newConfigViper()
	v.SetConfigType("yaml")

	if err := v.ReadConfig(strings.NewReader(yml)); err != nil {
		t.Fatalf("read: %v", err)
	}

	var c config.Config
	if err := v.Unmarshal(&c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if err := restoreRoleMappingCase(&c, []byte(yml)); err != nil {
		t.Fatalf("restore: %v", err)
	}

	m := c.Auth.OIDC.RoleMapping

	if got, ok := m["DB.Admin"]; !ok || got != config.RoleAdmin {
		t.Errorf(`role_mapping["DB.Admin"] = %q, %v; want admin`, got, ok)
	}

	for _, claim := range []string{"db.admin", "DB.ADMIN", "Db.Admin"} {
		if got, ok := m[claim]; ok {
			t.Errorf("role_mapping[%q] = %q, want no match", claim, got)
		}
	}

	if len(m) != 2 {
		t.Errorf("role_mapping = %v, want 2 entries", m)
	}
}

func TestRoleMappingKeepsKeyCaseUnderCapitalizedSections(t *testing.T) {
	t.Parallel()

	const yml = `
Auth:
  mode: oidc
  OIDC:
    Role_Mapping:
      "DB.Admin": admin
`

	v := newConfigViper()
	v.SetConfigType("yaml")

	if err := v.ReadConfig(strings.NewReader(yml)); err != nil {
		t.Fatalf("read: %v", err)
	}

	var c config.Config
	if err := v.Unmarshal(&c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if err := restoreRoleMappingCase(&c, []byte(yml)); err != nil {
		t.Fatalf("restore: %v", err)
	}

	m := c.Auth.OIDC.RoleMapping

	if got, ok := m["DB.Admin"]; !ok || got != config.RoleAdmin {
		t.Errorf(`role_mapping["DB.Admin"] = %q, %v; want admin`, got, ok)
	}

	if _, ok := m["db.admin"]; ok {
		t.Errorf("role_mapping = %v, lower-cased key survived", m)
	}
}

func TestPIICatalogConfigDecode(t *testing.T) {
	const yml = `
auth:
  mode: token
  tokens:
    - name: pii-classifier
      token_from_env: DASHA_TEST_PII_TOKEN
pii_catalog:
  enabled: true
  schedule: "CRON_TZ=Europe/Moscow 0 3 * * *"
  retention_days: 30
  statement_timeout: 45s
  exclude_schemas:
    - "archive%"
    - 'tmp\_%'
  allowed_tokens:
    - pii-classifier
`

	t.Setenv("DASHA_TEST_PII_TOKEN", "secret-value")
	t.Chdir(t.TempDir())

	if err := os.WriteFile("dasha.yaml", []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	c, warnings, err := provideConfig()
	if err != nil {
		t.Fatalf("provideConfig: %v", err)
	}

	got := c.PIICatalog
	want := config.PIICatalogConfig{
		Enabled:            true,
		Schedule:           "CRON_TZ=Europe/Moscow 0 3 * * *",
		RetentionDays:      30,
		AuditRetentionDays: config.DefaultPIICatalogAuditRetentionDays,
		StatementTimeout:   45 * time.Second,
		ExcludeSchemas:     []string{"archive%", `tmp\_%`},
		AllowedTokens:      []string{"pii-classifier"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pii_catalog = %+v, want %+v", got, want)
	}

	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}
}

func TestPIICatalogInvalidScheduleFailsConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile("dasha.yaml", []byte("pii_catalog:\n  schedule: bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := provideConfig()
	if err == nil || !strings.Contains(err.Error(), `pii_catalog.schedule: invalid cron expression "bad"`) {
		t.Fatalf("provideConfig error = %v, want invalid pii_catalog.schedule", err)
	}
}

func TestPIICatalogAllowedTokenSecretNotRequiredToLoad(t *testing.T) {
	const yml = `
auth:
  mode: token
  tokens:
    - name: pii-classifier
      token_from_env: DASHA_TEST_PII_TOKEN_UNSET
pii_catalog:
  allowed_tokens: [pii-classifier, pii-classifier]
`

	t.Setenv("DASHA_TEST_PII_TOKEN_UNSET", "")
	t.Chdir(t.TempDir())

	if err := os.WriteFile("dasha.yaml", []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	c, warnings, err := provideConfig()
	if err != nil {
		t.Fatalf("provideConfig: %v", err)
	}

	if len(warnings) != 1 || warnings[0].msg != "pii_catalog.allowed_tokens lists a token more than once" {
		t.Fatalf("warnings = %+v, want one duplicate warning", warnings)
	}

	err = c.PIICatalog.ValidateSecrets(c.Auth)
	if err == nil || !strings.Contains(err.Error(), `token "pii-classifier" has no secret`) {
		t.Fatalf("ValidateSecrets error = %v, want missing secret", err)
	}
}
