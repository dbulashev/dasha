package autosnapshot

import (
	"encoding/json"
	"fmt"
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
	WindowSize         time.Duration `json:"window_size"          validate:"gt=0"`
	ActiveThresholdPct int           `json:"active_threshold_pct" validate:"gt=0,lte=10000"`
	SpikeDuration      time.Duration `json:"spike_duration"       validate:"gt=0"`
	// RecoveryDuration: after a spike, take a snapshot once activity stays below
	// the threshold for this long (captures the spike's aftermath). 0 = disabled.
	RecoveryDuration time.Duration `json:"recovery_duration" validate:"gte=0"`
	// DeferredInterval: after a spike snapshot, enqueue a follow-up snapshot this
	// long later (persisted queue, survives restarts). 0 = disabled.
	DeferredInterval time.Duration `json:"deferred_interval" validate:"gte=0"`
}

type RoleChangeTrigger struct {
	Enabled   bool      `json:"enabled"`
	Direction Direction `json:"direction" validate:"oneof=master_to_replica replica_to_master both"`
}

type TriggerDefaults struct {
	ActivitySpike ActivitySpikeTrigger `json:"activity_spike"`
	RoleChange    RoleChangeTrigger    `json:"role_change"`
}

type Config struct {
	Enabled              bool
	PollInterval         time.Duration `validate:"gte=5s"`
	MaxSnapshotFrequency time.Duration `validate:"gtefield=PollInterval"`
	RetentionBytes       int64         `validate:"gte=0"`
	RetentionMinDays     int           `validate:"gte=0"`
	MinBaselineActive    int           `validate:"gte=0"`
	CaptureLocks         bool
	LockProbeCount       int           `validate:"gte=1,lte=20"`
	LockProbeInterval    time.Duration `validate:"gte=100ms,lte=5s"`
	ResetQueryStats      bool          // reset pg_stat_statements after each auto-snapshot
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

// Duration wraps time.Duration so override JSON (stored as jsonb) carries Go
// duration strings ("30s") instead of raw nanoseconds, which encoding/json
// cannot decode back into a time.Duration.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string | %w", err)
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q | %w", s, err)
	}

	*d = Duration(parsed)

	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String()) //nolint:wrapcheck
}

// OverrideInput is the typed, partial per-cluster override. Pointer fields
// distinguish "absent" (inherit the default) from "set to zero"; durations
// arrive as strings via Duration. It drives both merge and validation.
type OverrideInput struct {
	ActivitySpike *ActivitySpikeOverride `json:"activity_spike,omitempty"`
	RoleChange    *RoleChangeOverride    `json:"role_change,omitempty"`
}

type ActivitySpikeOverride struct {
	Enabled            *bool     `json:"enabled,omitempty"`
	WindowSize         *Duration `json:"window_size,omitempty"`
	ActiveThresholdPct *int      `json:"active_threshold_pct,omitempty"`
	SpikeDuration      *Duration `json:"spike_duration,omitempty"`
	RecoveryDuration   *Duration `json:"recovery_duration,omitempty"`
	DeferredInterval   *Duration `json:"deferred_interval,omitempty"`
}

type RoleChangeOverride struct {
	Enabled   *bool      `json:"enabled,omitempty"`
	Direction *Direction `json:"direction,omitempty"`
}

// applyTo overwrites only the fields the override sets, leaving the rest at the
// inherited default — the deep-merge semantics.
func (in OverrideInput) applyTo(d *TriggerDefaults) {
	if s := in.ActivitySpike; s != nil {
		if s.Enabled != nil {
			d.ActivitySpike.Enabled = *s.Enabled
		}
		if s.WindowSize != nil {
			d.ActivitySpike.WindowSize = time.Duration(*s.WindowSize)
		}
		if s.ActiveThresholdPct != nil {
			d.ActivitySpike.ActiveThresholdPct = *s.ActiveThresholdPct
		}
		if s.SpikeDuration != nil {
			d.ActivitySpike.SpikeDuration = time.Duration(*s.SpikeDuration)
		}
		if s.RecoveryDuration != nil {
			d.ActivitySpike.RecoveryDuration = time.Duration(*s.RecoveryDuration)
		}
		if s.DeferredInterval != nil {
			d.ActivitySpike.DeferredInterval = time.Duration(*s.DeferredInterval)
		}
	}

	if r := in.RoleChange; r != nil {
		if r.Enabled != nil {
			d.RoleChange.Enabled = *r.Enabled
		}
		if r.Direction != nil {
			d.RoleChange.Direction = *r.Direction
		}
	}
}

// EffectiveFor returns defaults deep-merged with per-cluster overrides. It is
// lenient: malformed stored overrides are ignored (write-time validation guards
// the data), so a read never fails.
func (c Config) EffectiveFor(override map[string]any) TriggerDefaults {
	res := c.Defaults

	if len(override) == 0 {
		return res
	}

	raw, err := json.Marshal(override)
	if err != nil {
		return res
	}

	var in OverrideInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return res
	}

	in.applyTo(&res)

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
