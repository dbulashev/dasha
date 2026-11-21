package autosnapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/dto"
)

// configStore runs the Update* callbacks synchronously, the way storage does
// inside its transaction, and records what would have been written.
type configStore struct {
	cfg       Config
	overrides map[string]map[string]any

	savedCfg  *Config
	savedBy   string
	savedOv   map[string]any
	ovWritten bool
}

func (f *configStore) GetAutosnapshotConfig(context.Context) (Config, error) { return f.cfg, nil }

func (f *configStore) GetClusterOverride(_ context.Context, cluster string) (ClusterOverride, error) {
	ov := f.overrides[cluster]
	if ov == nil {
		ov = map[string]any{}
	}

	return ClusterOverride{ClusterName: cluster, Overrides: ov}, nil //nolint:exhaustruct
}

func (f *configStore) ListClusterOverrides(context.Context) (map[string]map[string]any, error) {
	return f.overrides, nil
}

func (f *configStore) UpdateConfig(
	_ context.Context, user string, fn func(Config, map[string]map[string]any) (Config, error),
) error {
	next, err := fn(f.cfg, f.overrides)
	if err != nil {
		return err
	}

	f.savedCfg, f.savedBy = &next, user

	return nil
}

func (f *configStore) UpdateClusterOverride(
	_ context.Context, _, user string, fn func(Config) (map[string]any, error),
) error {
	ov, err := fn(f.cfg)
	if err != nil {
		return err
	}

	f.savedOv, f.savedBy, f.ovWritten = ov, user, true

	return nil
}

type clusterList []string

func (l clusterList) Clusters(context.Context) ([]dto.ClusterInfo, error) {
	out := make([]dto.ClusterInfo, 0, len(l))
	for _, n := range l {
		out = append(out, dto.ClusterInfo{Name: config.ClusterName(n)}) //nolint:exhaustruct
	}

	return out, nil
}

// spikeOverride is valid against validDefaults (window 30s) but not against a
// window shorter than 30s.
var spikeOverride = map[string]any{"activity_spike": map[string]any{"spike_duration": "15s"}}

func TestPutConfigRejectsConfigBreakingOverride(t *testing.T) {
	t.Parallel()

	st := &configStore{cfg: validConfig(), overrides: map[string]map[string]any{"c1": spikeOverride}} //nolint:exhaustruct
	svc := NewConfigService(st, nil)

	in := validConfig()
	in.Defaults.ActivitySpike.WindowSize = 20 * time.Second
	in.Defaults.ActivitySpike.SpikeDuration = 10 * time.Second

	err := svc.PutConfig(t.Context(), in, nil, "admin")
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}

	if st.savedCfg != nil {
		t.Fatal("config must not be written")
	}
}

func TestPutConfigInvalidConfig(t *testing.T) {
	t.Parallel()

	st := &configStore{cfg: validConfig()} //nolint:exhaustruct
	in := validConfig()
	in.HotSchedule = "nope"

	err := NewConfigService(st, nil).PutConfig(t.Context(), in, nil, "admin")
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestPutConfigColdWindow(t *testing.T) {
	t.Parallel()

	explicit := 30

	tests := []struct {
		name string
		in   *int
		want int
	}{
		{name: "absent keeps the stored value", in: nil, want: 12},
		{name: "explicit wins", in: &explicit, want: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cur := validConfig()
			cur.HotColdWindowDays = 12
			st := &configStore{cfg: cur} //nolint:exhaustruct

			in := validConfig()
			in.HotColdWindowDays = 0

			if err := NewConfigService(st, nil).PutConfig(t.Context(), in, tt.in, "admin"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if st.savedCfg == nil || st.savedCfg.HotColdWindowDays != tt.want {
				t.Fatalf("saved = %+v, want hot_cold_window_days %d", st.savedCfg, tt.want)
			}

			if st.savedBy != "admin" {
				t.Fatalf("savedBy = %q", st.savedBy)
			}
		})
	}
}

func TestPutCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr bool
	}{
		{name: "valid", raw: spikeOverride},
		{name: "empty deletes", raw: map[string]any{}},
		{name: "breaks cross rule", raw: map[string]any{"activity_spike": map[string]any{"spike_duration": "16s"}}, wantErr: true},
		{name: "unknown key", raw: map[string]any{"nope": true}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := &configStore{cfg: validConfig()} //nolint:exhaustruct

			err := NewConfigService(st, nil).PutCluster(t.Context(), "c1", tt.raw, "admin")
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want ErrInvalidConfig", err)
				}

				if st.ovWritten {
					t.Fatal("override must not be written")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !st.ovWritten || len(st.savedOv) != len(tt.raw) {
				t.Fatalf("written = %v, saved = %v", st.ovWritten, st.savedOv)
			}
		})
	}
}

func TestClustersMergesOverrides(t *testing.T) {
	t.Parallel()

	st := &configStore{cfg: validConfig(), overrides: map[string]map[string]any{"c1": spikeOverride}} //nolint:exhaustruct

	got, err := NewConfigService(st, clusterList{"c1", "c2"}).Clusters(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d clusters, want 2", len(got))
	}

	if d := got[0].Effective.ActivitySpike.SpikeDuration; got[0].Name != "c1" || d != 15*time.Second {
		t.Fatalf("c1: name %q, spike_duration %v", got[0].Name, d)
	}

	if d := got[1].Effective.ActivitySpike.SpikeDuration; got[1].Name != "c2" || got[1].Overrides != nil || d != 10*time.Second {
		t.Fatalf("c2: %+v", got[1])
	}
}
