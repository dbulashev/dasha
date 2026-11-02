package deps

import (
	"strings"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
)

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
