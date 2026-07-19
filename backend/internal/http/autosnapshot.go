package http

import (
	"context"
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

	// Scheduled hot-objects captures are a separate mechanism from the
	// triggered pgss events shown next to this — surface their freshness
	// separately so new hot snapshots are not mistaken for trigger activity.
	if last, err := s.storage.LastHotSnapshotAt(ctx); err == nil {
		var newest time.Time

		for _, t := range last {
			if t.After(newest) {
				newest = t
			}
		}

		if !newest.IsZero() {
			resp.LastHotSnapshotAt = &newest
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

	if err := cfg.Validate(); err != nil {
		return serverhttp.PutAutosnapshotConfig400Response{}, nil
	}

	// Reject the change if the new defaults would invalidate a stored override:
	// EffectiveFor is lenient at read time, so an invalid merge would otherwise be
	// used silently by the daemon.
	overrides, err := s.storage.ListClusterOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("PutAutosnapshotConfig | overrides: %w", err)
	}

	for _, ov := range overrides {
		if err := cfg.ValidateEffective(ov); err != nil {
			return serverhttp.PutAutosnapshotConfig400Response{}, nil
		}
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

// ListAutosnapshotClusters returns overrides + effective config for every cluster
// in one call, so the cluster list view doesn't fan out a request per cluster.
func (s *Handlers) ListAutosnapshotClusters(
	ctx context.Context,
	_ serverhttp.ListAutosnapshotClustersRequestObject,
) (serverhttp.ListAutosnapshotClustersResponseObject, error) {
	if s.storage == nil {
		return serverhttp.ListAutosnapshotClusters501Response{}, nil
	}

	cfg, err := s.storage.GetAutosnapshotConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListAutosnapshotClusters | config: %w", err)
	}

	overrides, err := s.storage.ListClusterOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListAutosnapshotClusters | overrides: %w", err)
	}

	clusters, err := s.repo.Clusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListAutosnapshotClusters | clusters: %w", err)
	}

	out := make(serverhttp.ListAutosnapshotClusters200JSONResponse, 0, len(clusters))
	for _, cl := range clusters {
		name := cl.Name.String()
		ov := overrides[name]

		out = append(out, serverhttp.AutoSnapshotClusterOverride{
			ClusterName: name,
			Overrides:   ov,
			Effective:   triggerDefaultsToAPI(cfg.EffectiveFor(ov)),
		})
	}

	return out, nil
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

	cfg, err := s.storage.GetAutosnapshotConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("PutAutosnapshotCluster | config: %w", err)
	}

	if err := cfg.ValidateOverride(req.Body.Overrides); err != nil {
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

	items, err := s.storage.ListTriggerEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("GetAutosnapshotTriggerEvents | %w", err)
	}

	out := make([]serverhttp.TriggerEvent, 0, len(items))
	for _, e := range items {
		out = append(out, triggerEventToAPI(e))
	}

	return serverhttp.GetAutosnapshotTriggerEvents200JSONResponse(out), nil
}

// GetAutosnapshotSummary returns per-cluster snapshot/error counts for the summary tab.
func (s *Handlers) GetAutosnapshotSummary(
	ctx context.Context,
	_ serverhttp.GetAutosnapshotSummaryRequestObject,
) (serverhttp.GetAutosnapshotSummaryResponseObject, error) {
	if s.storage == nil {
		return serverhttp.GetAutosnapshotSummary501Response{}, nil
	}

	items, err := s.storage.SummarizeTriggerEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetAutosnapshotSummary | %w", err)
	}

	out := make([]serverhttp.ClusterSnapshotSummary, 0, len(items))
	for _, c := range items {
		out = append(out, serverhttp.ClusterSnapshotSummary{
			ClusterName:   c.ClusterName,
			Snapshots:     c.Snapshots,
			ActivitySpike: c.ActivitySpike,
			RoleChange:    c.RoleChange,
			Errors:        c.Errors,
		})
	}

	return serverhttp.GetAutosnapshotSummary200JSONResponse(out), nil
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
		CaptureLocks:         cfg.CaptureLocks,
		LockProbeCount:       cfg.LockProbeCount,
		LockProbeInterval:    cfg.LockProbeInterval.String(),
		ResetQueryStats:      cfg.ResetQueryStats,
		HotEnabled:           cfg.HotEnabled,
		HotSchedule:          cfg.HotSchedule,
		HotTopN:              cfg.HotTopN,
		HotRetentionDays:     cfg.HotRetentionDays,
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
			RecoveryDuration:   t.ActivitySpike.RecoveryDuration.String(),
			DeferredInterval:   t.ActivitySpike.DeferredInterval.String(),
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

	lockInterval, err := time.ParseDuration(api.LockProbeInterval)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("lock_probe_interval: %w", err)
	}

	recoveryDur, err := parseOptionalDuration(api.Defaults.ActivitySpike.RecoveryDuration)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("recovery_duration: %w", err)
	}

	deferredInterval, err := parseOptionalDuration(api.Defaults.ActivitySpike.DeferredInterval)
	if err != nil {
		return autosnapshot.Config{}, fmt.Errorf("deferred_interval: %w", err)
	}

	// The struct validator cannot judge a cron expression; parse it here so an
	// invalid schedule is a 400 instead of a daemon-side daily fallback.
	if _, err := autosnapshot.ParseHotSchedule(api.HotSchedule); err != nil {
		return autosnapshot.Config{}, fmt.Errorf("hot_schedule: %w", err)
	}

	return autosnapshot.Config{
		Enabled:              api.Enabled,
		PollInterval:         poll,
		MaxSnapshotFrequency: maxFreq,
		RetentionBytes:       api.RetentionBytes,
		RetentionMinDays:     api.RetentionMinDays,
		MinBaselineActive:    api.MinBaselineActive,
		CaptureLocks:         api.CaptureLocks,
		LockProbeCount:       api.LockProbeCount,
		LockProbeInterval:    lockInterval,
		ResetQueryStats:      api.ResetQueryStats,
		HotEnabled:           api.HotEnabled,
		HotSchedule:          api.HotSchedule,
		HotTopN:              api.HotTopN,
		HotRetentionDays:     api.HotRetentionDays,
		Defaults: autosnapshot.TriggerDefaults{
			ActivitySpike: autosnapshot.ActivitySpikeTrigger{
				Enabled:            api.Defaults.ActivitySpike.Enabled,
				WindowSize:         windowSize,
				ActiveThresholdPct: api.Defaults.ActivitySpike.ActiveThresholdPct,
				SpikeDuration:      spikeDur,
				RecoveryDuration:   recoveryDur,
				DeferredInterval:   deferredInterval,
			},
			RoleChange: autosnapshot.RoleChangeTrigger{
				Enabled:   api.Defaults.RoleChange.Enabled,
				Direction: autosnapshot.Direction(api.Defaults.RoleChange.Direction),
			},
		},
	}, nil
}

// parseOptionalDuration parses a Go duration string; empty means 0 (disabled).
func parseOptionalDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	return time.ParseDuration(s)
}

func triggerEventToAPI(e autosnapshot.TriggerEvent) serverhttp.TriggerEvent {
	out := serverhttp.TriggerEvent{
		Id:           openapi_types.UUID(e.ID),
		CreatedAt:    e.CreatedAt,
		ClusterName:  e.ClusterName,
		Instance:     e.Instance,
		Database:     e.Database,
		TriggerType:  string(e.TriggerType),
		Outcome:      string(e.Outcome),
		ErrorMessage: e.ErrorMessage,
	}

	// Only set the pointer when there is context, otherwise it serializes as null.
	if len(e.TriggerContext) > 0 {
		tc := e.TriggerContext
		out.TriggerContext = &tc
	}

	if e.SnapshotID != nil {
		id := openapi_types.UUID(*e.SnapshotID)
		out.SnapshotId = &id
	}

	return out
}
