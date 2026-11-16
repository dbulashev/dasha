package yandexmdb

import (
	"context"
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

	if pg.QueryID != "query_id" {
		t.Errorf("postgresql QueryID = %q, want query_id", pg.QueryID)
	}

	pooler := p.Fields(source.StreamPooler)
	if pooler.Text != "text" || pooler.Severity != "level" {
		t.Errorf("pooler field map = %+v", pooler)
	}

	if pooler.QueryID != "" {
		t.Errorf("pooler QueryID = %q, want none", pooler.QueryID)
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

func TestNarrowDropsTextDatabaseAndUser(t *testing.T) {
	t.Parallel()

	p := New(nil)
	id := int64(7)
	in := source.Filter{
		Severities: []string{"LOG"},
		Host:       "db-1",
		QueryID:    &id,
		Words:      []string{"duration"},
		Substrings: []string{"select"},
		Database:   "app",
		User:       "bob",
	}

	got := p.Narrow(context.Background(), source.StreamParams{Filter: in}) //nolint:exhaustruct

	want := source.Filter{Severities: []string{"LOG"}, Host: "db-1"} //nolint:exhaustruct
	if got.Host != want.Host || len(got.Severities) != 1 || got.QueryID != nil ||
		got.Words != nil || got.Substrings != nil || got.Database != "" || got.User != "" {
		t.Errorf("narrowed = %+v, want %+v", got.Filter, want)
	}

	if got.TextSkip != source.TextSkipUnsupported {
		t.Errorf("text skip = %q, want unsupported", got.TextSkip)
	}

	in.Substrings = nil
	in.Terms = []string{"deadlock"}

	got = p.Narrow(context.Background(), source.StreamParams{Filter: in}) //nolint:exhaustruct
	if got.Terms != nil || got.TextSkip != source.TextSkipUnsupported {
		t.Errorf("narrowed = %+v, want terms dropped as unsupported", got)
	}

	in.Terms = nil
	if got := p.Narrow(context.Background(), source.StreamParams{Filter: in}); got.TextSkip != source.TextSkipNone { //nolint:exhaustruct
		t.Errorf("text skip without substrings = %q, want none", got.TextSkip)
	}
}
