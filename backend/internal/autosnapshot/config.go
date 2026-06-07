package autosnapshot

import (
	"time"

	"github.com/google/uuid"
)

type Direction string

const (
	DirectionMasterToReplica Direction = "master_to_replica"
	DirectionReplicaToMaster Direction = "replica_to_master"
	DirectionBoth            Direction = "both"
)

type ActivitySpikeTrigger struct {
	Enabled            bool          `json:"enabled"`
	WindowSize         time.Duration `json:"window_size"`
	ActiveThresholdPct int           `json:"active_threshold_pct"`
	SpikeDuration      time.Duration `json:"spike_duration"`
	// RecoveryDuration: after a spike, take a snapshot once activity stays below
	// the threshold for this long (captures the spike's aftermath). 0 = disabled.
	RecoveryDuration time.Duration `json:"recovery_duration"`
	// DeferredInterval: after a spike snapshot, enqueue a follow-up snapshot this
	// long later (persisted queue, survives restarts). 0 = disabled.
	DeferredInterval time.Duration `json:"deferred_interval"`
}

type RoleChangeTrigger struct {
	Enabled   bool      `json:"enabled"`
	Direction Direction `json:"direction"`
}

type TriggerDefaults struct {
	ActivitySpike ActivitySpikeTrigger `json:"activity_spike"`
	RoleChange    RoleChangeTrigger    `json:"role_change"`
}

type Config struct {
	Enabled              bool
	PollInterval         time.Duration
	MaxSnapshotFrequency time.Duration
	RetentionBytes       int64
	RetentionMinDays     int
	MinBaselineActive    int
	CaptureLocks         bool
	LockProbeCount       int
	LockProbeInterval    time.Duration
	ResetQueryStats      bool // reset pg_stat_statements after each auto-snapshot
	Defaults             TriggerDefaults
	UpdatedAt            time.Time
	UpdatedBy            *string
}

// ClusterOverride holds the raw jsonb of overrides for a cluster plus the
// effective merged defaults returned alongside for the UI.
type ClusterOverride struct {
	ClusterName string
	Overrides   map[string]any
	UpdatedAt   time.Time
	UpdatedBy   *string
}

// EffectiveFor returns defaults deep-merged with per-cluster overrides.
func (c Config) EffectiveFor(override map[string]any) TriggerDefaults {
	res := c.Defaults

	if override == nil {
		return res
	}

	if spike, ok := override["activity_spike"].(map[string]any); ok {
		if v, ok := spike["enabled"].(bool); ok {
			res.ActivitySpike.Enabled = v
		}
		if v, ok := spike["window_size"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				res.ActivitySpike.WindowSize = d
			}
		}
		if v, ok := spike["active_threshold_pct"].(float64); ok {
			res.ActivitySpike.ActiveThresholdPct = int(v)
		}
		if v, ok := spike["spike_duration"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				res.ActivitySpike.SpikeDuration = d
			}
		}
		if v, ok := spike["recovery_duration"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				res.ActivitySpike.RecoveryDuration = d
			}
		}
		if v, ok := spike["deferred_interval"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				res.ActivitySpike.DeferredInterval = d
			}
		}
	}

	if role, ok := override["role_change"].(map[string]any); ok {
		if v, ok := role["enabled"].(bool); ok {
			res.RoleChange.Enabled = v
		}
		if v, ok := role["direction"].(string); ok {
			res.RoleChange.Direction = Direction(v)
		}
	}

	return res
}

type TriggerType string

const (
	TriggerActivitySpike TriggerType = "activity_spike"
	TriggerRoleChange    TriggerType = "role_change"
	TriggerActivityDrop  TriggerType = "activity_drop" // spike resolved (back below threshold)
	TriggerDeferred      TriggerType = "deferred"      // scheduled follow-up after a spike
)

type Outcome string

const (
	OutcomeSnapshotCreated       Outcome = "snapshot_created"
	OutcomeSkippedDebounce       Outcome = "skipped:debounce"
	OutcomeSkippedStorage        Outcome = "skipped:storage_unavailable"
	OutcomeSkippedBelowBaseline  Outcome = "skipped:below_baseline"
	OutcomeSkippedWrongDirection Outcome = "skipped:wrong_direction"
	OutcomeError                 Outcome = "error"
)

type TriggerEvent struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	ClusterName    string
	Instance       string
	Database       *string
	TriggerType    TriggerType
	Outcome        Outcome
	SnapshotID     *uuid.UUID
	TriggerContext map[string]any
	ErrorMessage   *string
}

type TriggerEventFilter struct {
	ClusterName string
	Outcome     string
	TriggerType string
	From        *time.Time
	To          *time.Time
	Limit       int
	Offset      int
}

// PendingSnapshot is a deferred (scheduled) snapshot waiting in the queue.
type PendingSnapshot struct {
	ClusterName string
	Instance    string
	Database    string
	Reason      string
}

// ClusterSummary aggregates trigger-event counts per cluster — powers the
// summary tab (where to look first: many spikes or many errors).
type ClusterSummary struct {
	ClusterName   string
	Snapshots     int // outcome = snapshot_created (total)
	ActivitySpike int // snapshot_created via activity_spike
	RoleChange    int // snapshot_created via role_change
	Errors        int // outcome = error
}

type LeaderInfo struct {
	InstanceID    *string
	LastHeartbeat *time.Time
	IsAlive       bool
}

// LeaderLivenessThreshold — if the last heartbeat is older than this,
// the leader is considered dead.
const LeaderLivenessThreshold = 30 * time.Second

// SnapshotOpts bundles optional fields for a snapshot insert.
// Lives here (not in storage) to keep autosnapshot free of storage imports.
type SnapshotOpts struct {
	PgssStatsReset *time.Time
	Reason         string
	TriggerContext map[string]any
	LocksData      *LockCapture
}

// PartitionSize is a single day-triple partition group with its combined size.
type PartitionSize struct {
	Day       time.Time
	TotalSize int64
}
