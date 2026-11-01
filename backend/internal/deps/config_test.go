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
