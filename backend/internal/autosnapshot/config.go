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
}

// PartitionSize is a single day-triple partition group with its combined size.
type PartitionSize struct {
	Day       time.Time
	TotalSize int64
}
