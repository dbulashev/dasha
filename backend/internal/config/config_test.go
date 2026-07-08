package config

import "testing"

func TestClusterSupportsLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster Cluster
		want    bool
	}{
		{
			name: "yandex cluster with provider id and folder",
			cluster: Cluster{ //nolint:exhaustruct
				Source:     SourceYandexMDB,
				ProviderID: "c9q123",
				Labels:     map[string]string{"folder_id": "b1g456"},
			},
			want: true,
		},
		{
			name: "static cluster",
			cluster: Cluster{ //nolint:exhaustruct
				Source:     "static",
				ProviderID: "c9q123",
				Labels:     map[string]string{"folder_id": "b1g456"},
			},
			want: false,
		},
		{
			name: "missing provider id",
			cluster: Cluster{ //nolint:exhaustruct
				Source: SourceYandexMDB,
				Labels: map[string]string{"folder_id": "b1g456"},
			},
			want: false,
		},
		{
			name: "missing folder_id label",
			cluster: Cluster{ //nolint:exhaustruct
				Source:     SourceYandexMDB,
				ProviderID: "c9q123",
				Labels:     map[string]string{},
			},
			want: false,
		},
		{
			name: "nil labels",
			cluster: Cluster{ //nolint:exhaustruct
				Source:     SourceYandexMDB,
				ProviderID: "c9q123",
			},
			want: false,
		},
		{
			name:    "zero value",
			cluster: Cluster{}, //nolint:exhaustruct
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.cluster.SupportsLogs(); got != tt.want {
				t.Errorf("SupportsLogs() = %v, want %v", got, tt.want)
			}
		})
	}
}
