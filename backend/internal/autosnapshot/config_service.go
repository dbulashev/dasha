package autosnapshot

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/dbulashev/dasha/internal/dto"
)

// ConfigStore persists the global config and per-cluster overrides. The
// Update* callbacks run inside a transaction and must not touch the network;
// an error from fn rolls it back and is returned unchanged.
type ConfigStore interface {
	GetAutosnapshotConfig(ctx context.Context) (Config, error)
	GetClusterOverride(ctx context.Context, cluster string) (ClusterOverride, error)
	ListClusterOverrides(ctx context.Context) (map[string]map[string]any, error)

	// fn runs with the config row locked FOR UPDATE.
	UpdateConfig(ctx context.Context, user string,
		fn func(cur Config, overrides map[string]map[string]any) (Config, error)) error

	// fn runs with the config row locked FOR SHARE. An empty result deletes the override.
	UpdateClusterOverride(ctx context.Context, cluster, user string,
		fn func(cfg Config) (map[string]any, error)) error
}

type ClusterLister interface {
	Clusters(ctx context.Context) ([]dto.ClusterInfo, error)
}

var ErrInvalidConfig = errors.New("autosnapshot: invalid config")

type ClusterView struct {
	Name      string
	Overrides map[string]any
	Effective TriggerDefaults
}

type ConfigService struct {
	store    ConfigStore
	clusters ClusterLister
}

func NewConfigService(store ConfigStore, clusters ClusterLister) *ConfigService {
	return &ConfigService{store: store, clusters: clusters}
}

func (s *ConfigService) Config(ctx context.Context) (Config, error) {
	cfg, err := s.store.GetAutosnapshotConfig(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
}

// PutConfig replaces the global config. coldWindowDays == nil keeps the stored
// value. The new defaults are rejected if they would invalidate a stored
// override: EffectiveFor is lenient at read time, so the daemon would otherwise
// run on an invalid merge without anyone noticing.
func (s *ConfigService) PutConfig(ctx context.Context, in Config, coldWindowDays *int, user string) error {
	return s.store.UpdateConfig(ctx, user, func(cur Config, overrides map[string]map[string]any) (Config, error) {
		next := in
		next.HotColdWindowDays = cur.HotColdWindowDays

		if coldWindowDays != nil {
			next.HotColdWindowDays = *coldWindowDays
		}

		if err := next.Validate(); err != nil {
			return Config{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}

		for _, name := range slices.Sorted(maps.Keys(overrides)) {
			if err := next.ValidateEffective(overrides[name]); err != nil {
				return Config{}, fmt.Errorf("%w: override of cluster %q: %w", ErrInvalidConfig, name, err)
			}
		}

		return next, nil
	})
}

func (s *ConfigService) Cluster(ctx context.Context, name string) (ClusterView, error) {
	cfg, err := s.store.GetAutosnapshotConfig(ctx)
	if err != nil {
		return ClusterView{}, fmt.Errorf("config: %w", err)
	}

	ov, err := s.store.GetClusterOverride(ctx, name)
	if err != nil {
		return ClusterView{}, fmt.Errorf("override: %w", err)
	}

	return ClusterView{Name: ov.ClusterName, Overrides: ov.Overrides, Effective: cfg.EffectiveFor(ov.Overrides)}, nil
}

// Clusters lists every configured cluster, with or without an override.
func (s *ConfigService) Clusters(ctx context.Context) ([]ClusterView, error) {
	cfg, err := s.store.GetAutosnapshotConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	overrides, err := s.store.ListClusterOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("overrides: %w", err)
	}

	clusters, err := s.clusters.Clusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("clusters: %w", err)
	}

	out := make([]ClusterView, 0, len(clusters))

	for _, cl := range clusters {
		name := cl.Name.String()
		ov := overrides[name]

		out = append(out, ClusterView{Name: name, Overrides: ov, Effective: cfg.EffectiveFor(ov)})
	}

	return out, nil
}

// PutCluster replaces the cluster's override; an empty raw deletes it.
func (s *ConfigService) PutCluster(ctx context.Context, name string, raw map[string]any, user string) error {
	return s.store.UpdateClusterOverride(ctx, name, user, func(cfg Config) (map[string]any, error) {
		if err := cfg.ValidateOverride(raw); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}

		return raw, nil
	})
}
