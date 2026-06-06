package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/autosnapshot"
)

func userNameFromContext(ctx context.Context) string {
	if u := auth.UserFromContext(ctx); u != nil {
		return u.Name
	}

	return ""
}

// GetAutosnapshotStatus reports feature availability and the current leader.
func (s *Handlers) GetAutosnapshotStatus(
	ctx context.Context,
	_ serverhttp.GetAutosnapshotStatusRequestObject,
) (serverhttp.GetAutosnapshotStatusResponseObject, error) {
	resp := serverhttp.GetAutosnapshotStatus200JSONResponse{
		Available: s.storage != nil,
		Enabled:   false,
	}

	if s.storage == nil {
		return resp, nil
	}

	cfg, err := s.storage.GetAutosnapshotConfig(ctx)
	if err == nil {
		resp.Enabled = cfg.Enabled
	}

	leader, err := s.storage.GetLeaderInfo(ctx)
	if err == nil {
		resp.Leader = &serverhttp.AutoSnapshotLeaderInfo{
			InstanceId:    leader.InstanceID,
			LastHeartbeat: leader.LastHeartbeat,
			IsAlive:       leader.IsAlive,
		}
	}

	return resp, nil
}

// GetAutosnapshotConfig returns the global auto-snapshot config.
func (s *Handlers) GetAutosnapshotConfig(
	ctx context.Context,
	_ serverhttp.GetAutosnapshotConfigRequestObject,
) (serverhttp.GetAutosnapshotConfigResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetAutosnapshotConfig501Response{}, nil
	}

	cfg, err := s.storage.GetAutosnapshotConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetAutosnapshotConfig | %w", err)
	}

	return serverhttp.GetAutosnapshotConfig200JSONResponse(configToAPI(cfg)), nil
}

// PutAutosnapshotConfig replaces the global auto-snapshot config (admin only via Casbin).
func (s *Handlers) PutAutosnapshotConfig(
	ctx context.Context,
	req serverhttp.PutAutosnapshotConfigRequestObject,
) (serverhttp.PutAutosnapshotConfigResponseObject, error) {
	if s.storage == nil {
		return serverhttp.PutAutosnapshotConfig501Response{}, nil
	}

	if req.Body == nil {
		return serverhttp.PutAutosnapshotConfig400Response{}, nil
	}

	cfg, err := configFromAPI(*req.Body)
	if err != nil {
		return serverhttp.PutAutosnapshotConfig400Response{}, nil
	}

	if err := validateAutosnapshotConfig(cfg); err != nil {
		return serverhttp.PutAutosnapshotConfig400Response{}, nil
	}

	user := userNameFromContext(ctx)

	if err := s.storage.SetAutosnapshotConfig(ctx, cfg, user); err != nil {
		return nil, fmt.Errorf("PutAutosnapshotConfig | %w", err)
	}

	return serverhttp.PutAutosnapshotConfig204Response{}, nil
}

// GetAutosnapshotCluster returns per-cluster overrides plus the effective merged defaults.
func (s *Handlers) GetAutosnapshotCluster(
	ctx context.Context,
	req serverhttp.GetAutosnapshotClusterRequestObject,
) (serverhttp.GetAutosnapshotClusterResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetAutosnapshotCluster501Response{}, nil
	}

	cfg, err := s.storage.GetAutosnapshotConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetAutosnapshotCluster | config: %w", err)
	}

	override, err := s.storage.GetClusterOverride(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("GetAutosnapshotCluster | override: %w", err)
	}

	effective := cfg.EffectiveFor(override.Overrides)

	return serverhttp.GetAutosnapshotCluster200JSONResponse{
		ClusterName: override.ClusterName,
		Overrides:   override.Overrides,
		Effective:   triggerDefaultsToAPI(effective),
	}, nil
}

// PutAutosnapshotCluster upserts per-cluster overrides (empty Overrides map deletes the row).
func (s *Handlers) PutAutosnapshotCluster(
	ctx context.Context,
	req serverhttp.PutAutosnapshotClusterRequestObject,
) (serverhttp.PutAutosnapshotClusterResponseObject, error) {
	if s.storage == nil {
		return serverhttp.PutAutosnapshotCluster501Response{}, nil
	}

	if req.Body == nil {
		return serverhttp.PutAutosnapshotCluster400Response{}, nil
	}

	user := userNameFromContext(ctx)

	if err := s.storage.SetClusterOverride(ctx, req.Name, req.Body.Overrides, user); err != nil {
		return nil, fmt.Errorf("PutAutosnapshotCluster | %w", err)
	}

	return serverhttp.PutAutosnapshotCluster204Response{}, nil
}

// GetAutosnapshotTriggerEvents returns paginated event history with filters.
func (s *Handlers) GetAutosnapshotTriggerEvents(
	ctx context.Context,
	req serverhttp.GetAutosnapshotTriggerEventsRequestObject,
) (serverhttp.GetAutosnapshotTriggerEventsResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetAutosnapshotTriggerEvents501Response{}, nil
	}

	filter := autosnapshot.TriggerEventFilter{}
	if req.Params.ClusterName != nil {
		filter.ClusterName = *req.Params.ClusterName
	}

	if req.Params.Outcome != nil {
		filter.Outcome = *req.Params.Outcome
	}

	if req.Params.TriggerType != nil {
		filter.TriggerType = *req.Params.TriggerType
	}

	filter.From = req.Params.From
	filter.To = req.Params.To

	if req.Params.Limit != nil {
		filter.Limit = *req.Params.Limit
	}

	if req.Params.Offset != nil {
		filter.Offset = *req.Params.Offset
	}

	items, total, err := s.storage.ListTriggerEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("GetAutosnapshotTriggerEvents | %w", err)
	}

	out := make([]serverhttp.TriggerEvent, 0, len(items))
	for _, e := range items {
		out = append(out, triggerEventToAPI(e))
	}

	return serverhttp.GetAutosnapshotTriggerEvents200JSONResponse{
		Items: out,
		Total: total,
	}, nil
}

// --- helpers -------------------------------------------------------------

func configToAPI(cfg autosnapshot.Config) serverhttp.AutoSnapshotConfig {
	return serverhttp.AutoSnapshotConfig{
		Enabled:              cfg.Enabled,
		PollInterval:         cfg.PollInterval.String(),
		MaxSnapshotFrequency: cfg.MaxSnapshotFrequency.String(),
		RetentionBytes:       cfg.RetentionBytes,
		RetentionMinDays:     cfg.RetentionMinDays,
		MinBaselineActive:    cfg.MinBaselineActive,
		Defaults:             triggerDefaultsToAPI(cfg.Defaults),
	}
}

func triggerDefaultsToAPI(t autosnapshot.TriggerDefaults) serverhttp.AutoSnapshotTriggerDefaults {
	return serverhttp.AutoSnapshotTriggerDefaults{
		ActivitySpike: serverhttp.ActivitySpikeTrigger{
			Enabled:            t.ActivitySpike.Enabled,
			WindowSize:         t.ActivitySpike.WindowSize.String(),
			ActiveThresholdPct: t.ActivitySpike.ActiveThresholdPct,
			SpikeDuration:      t.ActivitySpike.SpikeDuration.String(),
		},
		RoleChange: serverhttp.RoleChangeTrigger{
			Enabled:   t.RoleChange.Enabled,
			Direction: serverhttp.RoleChangeTriggerDirection(t.RoleChange.Direction),
		},
	}
}

func configFromAPI(api serverhttp.AutoSnapshotConfig) (autosnapshot.Config, error) {
	poll, err := time.ParseDuration(api.PollInterval)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("poll_interval: %w", err)
	}

	maxFreq, err := time.ParseDuration(api.MaxSnapshotFrequency)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("max_snapshot_frequency: %w", err)
	}

	windowSize, err := time.ParseDuration(api.Defaults.ActivitySpike.WindowSize)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("window_size: %w", err)
	}

	spikeDur, err := time.ParseDuration(api.Defaults.ActivitySpike.SpikeDuration)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("spike_duration: %w", err)
	}

	return autosnapshot.Config{
		Enabled:              api.Enabled,
		PollInterval:         poll,
		MaxSnapshotFrequency: maxFreq,
		RetentionBytes:       api.RetentionBytes,
		RetentionMinDays:     api.RetentionMinDays,
		MinBaselineActive:    api.MinBaselineActive,
		Defaults: autosnapshot.TriggerDefaults{
			ActivitySpike: autosnapshot.ActivitySpikeTrigger{
				Enabled:            api.Defaults.ActivitySpike.Enabled,
				WindowSize:         windowSize,
				ActiveThresholdPct: api.Defaults.ActivitySpike.ActiveThresholdPct,
				SpikeDuration:      spikeDur,
			},
			RoleChange: autosnapshot.RoleChangeTrigger{
				Enabled:   api.Defaults.RoleChange.Enabled,
				Direction: autosnapshot.Direction(api.Defaults.RoleChange.Direction),
			},
		},
	}, nil
}

func validateAutosnapshotConfig(cfg autosnapshot.Config) error {
	if cfg.PollInterval < 5*time.Second {
		return errors.New("poll_interval must be >= 5s")
	}

	if cfg.MaxSnapshotFrequency < cfg.PollInterval {
		return errors.New("max_snapshot_frequency must be >= poll_interval")
	}

	if cfg.RetentionBytes < 0 {
		return errors.New("retention_bytes must be >= 0")
	}

	if cfg.RetentionMinDays < 0 {
		return errors.New("retention_min_days must be >= 0")
	}

	if cfg.MinBaselineActive < 0 {
		return errors.New("min_baseline_active must be >= 0")
	}

	spike := cfg.Defaults.ActivitySpike
	if spike.WindowSize <= 0 {
		return errors.New("window_size must be > 0")
	}

	if spike.ActiveThresholdPct <= 0 || spike.ActiveThresholdPct > 10000 {
		return errors.New("active_threshold_pct must be in (0, 10000]")
	}

	if spike.SpikeDuration <= 0 {
		return errors.New("spike_duration must be > 0")
	}

	if spike.SpikeDuration > 2*spike.WindowSize {
		return errors.New("spike_duration must be <= 2 * window_size")
	}

	switch cfg.Defaults.RoleChange.Direction {
	case autosnapshot.DirectionMasterToReplica,
		autosnapshot.DirectionReplicaToMaster,
		autosnapshot.DirectionBoth:
	default:
		return fmt.Errorf("invalid direction: %q", cfg.Defaults.RoleChange.Direction)
	}

	return nil
}

func triggerEventToAPI(e autosnapshot.TriggerEvent) serverhttp.TriggerEvent {
	out := serverhttp.TriggerEvent{
		Id:             openapi_types.UUID(e.ID),
		CreatedAt:      e.CreatedAt,
		ClusterName:    e.ClusterName,
		Instance:       e.Instance,
		Database:       e.Database,
		TriggerType:    string(e.TriggerType),
		Outcome:        string(e.Outcome),
		TriggerContext: &e.TriggerContext,
		ErrorMessage:   e.ErrorMessage,
	}

	if e.SnapshotID != nil {
		id := openapi_types.UUID(*e.SnapshotID)
		out.SnapshotId = &id
	}

	return out
}
