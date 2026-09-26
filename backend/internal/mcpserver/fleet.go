package mcpserver

import (
	"context"
	"time"
)

const (
	defaultFleetLimit = 5
	maxFleetLimit     = 50
)

// fleetEntry.Score is nil only for an unscored instance; Error says why.
type fleetEntry struct {
	Cluster    string   `json:"cluster"`
	Instance   string   `json:"instance"`
	Score      *float64 `json:"score,omitempty"`
	Source     string   `json:"source,omitempty"`
	InRecovery bool     `json:"in_recovery,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type fleetMeta struct {
	Total              int       `json:"total"`
	Scored             int       `json:"scored"`
	Uncomputed         int       `json:"uncomputed"`
	Incomplete         bool      `json:"incomplete"`
	MetricsUnavailable bool      `json:"metrics_unavailable"`
	ComputedAt         time.Time `json:"computed_at"`
}

type fleetResult struct {
	Limit int          `json:"limit"`
	Worst []fleetEntry `json:"worst"`
	Fleet fleetMeta    `json:"fleet"`
}

func fleetHealth(ctx context.Context, c *DashaClient, limit int) (any, error) {
	if limit <= 0 {
		limit = defaultFleetLimit
	}

	limit = min(limit, maxFleetLimit)

	f, err := c.FleetHealth(ctx, limit)
	if err != nil {
		return nil, err
	}

	rows := make([]fleetEntry, 0, len(f.Items))

	for _, it := range f.Items {
		rows = append(rows, fleetEntry{
			Cluster:    it.ClusterName,
			Instance:   it.Instance,
			Score:      it.Score,
			Source:     string(it.Source),
			InRecovery: it.InRecovery,
			Error:      deref(it.Error),
		})
	}

	return fleetResult{
		Limit: limit,
		Worst: rows,
		Fleet: fleetMeta{
			Total:              f.InstancesTotal,
			Scored:             f.InstancesScored,
			Uncomputed:         f.Uncomputed,
			Incomplete:         f.Incomplete,
			MetricsUnavailable: f.MetricsUnavailable,
			ComputedAt:         f.ComputedAt,
		},
	}, nil
}
