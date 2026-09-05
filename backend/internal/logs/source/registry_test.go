package source_test

import (
	"context"
	"testing"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// stubProvider serves one stream and claims clusters whose source name matches.
type stubProvider struct {
	claims string
}

func (p stubProvider) Streams() []string { return []string{source.StreamPostgreSQL} }

func (p stubProvider) Fields(stream string) source.FieldMap {
	if stream != source.StreamPostgreSQL {
		return source.FieldMap{}
	}

	fm, _ := source.Preset(source.PresetCSVLog)
	fm.Timestamp = "ts"
	fm.Host = "host"

	return fm
}

func (p stubProvider) Stream(context.Context, source.StreamParams, func(source.Record) bool) error {
	return nil
}

func (p stubProvider) Check(context.Context, config.Cluster, string) (source.CheckResult, error) {
	return source.CheckResult{}, nil
}

func (p stubProvider) Matches(cluster config.Cluster) bool {
	return p.claims != "" && cluster.Source == p.claims
}

func TestRegistryResolutionOrder(t *testing.T) {
	t.Parallel()

	named := stubProvider{claims: ""}
	implicit := stubProvider{claims: config.SourceYandexMDB}

	tests := []struct {
		name    string
		cluster config.Cluster
		def     string
		want    string
	}{
		{
			name:    "cluster names its source",
			cluster: config.Cluster{Name: "a", LogSource: "main"},
			want:    "main",
		},
		{
			name:    "default source serves the rest",
			cluster: config.Cluster{Name: "a", Source: "static"},
			def:     "main",
			want:    "main",
		},
		{
			name:    "cluster source wins over the default",
			cluster: config.Cluster{Name: "a", Source: "static", LogSource: "implicit"},
			def:     "main",
			want:    "implicit",
		},
		{
			name:    "implicit binding by cluster shape",
			cluster: config.Cluster{Name: "a", Source: config.SourceYandexMDB},
			want:    "implicit",
		},
		{
			// The default is a fleet-wide fallback; a cluster whose logs live
			// somewhere a provider claims must not be re-routed into it.
			name:    "cluster shape wins over the default",
			cluster: config.Cluster{Name: "a", Source: config.SourceYandexMDB},
			def:     "main",
			want:    "implicit",
		},
		{
			name:    "no source at all",
			cluster: config.Cluster{Name: "a", Source: "static"},
			want:    "",
		},
		{
			name:    "unknown source name",
			cluster: config.Cluster{Name: "a", LogSource: "gone"},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg := source.NewRegistry()
			reg.Register("implicit", implicit)
			reg.Register("main", named)
			reg.SetDefault(tt.def)

			_, name, ok := reg.For(tt.cluster)
			if tt.want == "" {
				if ok {
					t.Fatalf("For() resolved to %q, want no source", name)
				}

				if reg.Supports(tt.cluster) {
					t.Error("Supports() = true, want false")
				}

				return
			}

			if !ok || name != tt.want {
				t.Fatalf("For() = %q, %v; want %q, true", name, ok, tt.want)
			}

			if got := reg.Streams(tt.cluster); len(got) != 1 || got[0] != source.StreamPostgreSQL {
				t.Errorf("Streams() = %v", got)
			}
		})
	}
}
