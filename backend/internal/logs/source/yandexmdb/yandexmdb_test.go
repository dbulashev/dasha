package yandexmdb

import (
	"testing"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

func TestMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster config.Cluster
		want    bool
	}{
		{
			name: "yandex cluster with provider id and folder",
			cluster: config.Cluster{
				Source:     config.SourceYandexMDB,
				ProviderID: "c9q123",
				Labels:     map[string]string{"folder_id": "b1g456"},
			},
			want: true,
		},
		{
			name: "static cluster",
			cluster: config.Cluster{
				Source:     "static",
				ProviderID: "c9q123",
				Labels:     map[string]string{"folder_id": "b1g456"},
			},
		},
		{
			name: "missing provider id",
			cluster: config.Cluster{
				Source: config.SourceYandexMDB,
				Labels: map[string]string{"folder_id": "b1g456"},
			},
		},
		{
			name: "missing folder_id label",
			cluster: config.Cluster{
				Source:     config.SourceYandexMDB,
				ProviderID: "c9q123",
				Labels:     map[string]string{},
			},
		},
		{
			name:    "zero value",
			cluster: config.Cluster{},
		},
	}

	p := New(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := p.Matches(tt.cluster); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFieldsPerStream(t *testing.T) {
	t.Parallel()

	p := New(nil)

	pg := p.Fields(source.StreamPostgreSQL)
	if pg.Text != "message" || pg.Host != hostField || pg.Database != "database_name" {
		t.Errorf("postgresql field map = %+v", pg)
	}

	pooler := p.Fields(source.StreamPooler)
	if pooler.Text != "text" || pooler.Severity != "level" {
		t.Errorf("pooler field map = %+v", pooler)
	}

	if got, ok := pooler.CanonicalSeverity("ERROR"); !ok || got != "error" {
		t.Errorf("pooler CanonicalSeverity(ERROR) = %q, %v; want error, true", got, ok)
	}

	if !p.Fields("syslog").Empty() {
		t.Error("Fields() returned a map for an unknown stream")
	}
}

func TestBuildFilterUsesAllowlistedValuesOnly(t *testing.T) {
	t.Parallel()

	p := New(nil)
	fm := p.Fields(source.StreamPostgreSQL)

	got := buildFilter(fm, source.Filter{Severities: []string{"ERROR", "FATAL"}, Host: "db-1"})
	want := `message.error_severity IN ("ERROR", "FATAL") AND message.hostname = "db-1"`

	if got != want {
		t.Errorf("buildFilter() = %q, want %q", got, want)
	}

	if got := buildFilter(fm, source.Filter{}); got != "" {
		t.Errorf("empty filter = %q, want empty", got)
	}
}
