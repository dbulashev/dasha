package mcpserver

import (
	"cmp"
	"context"
	"slices"

	"golang.org/x/sync/errgroup"
)

// defaultFleetLimit caps how many worst instances fleet_health returns by default.
const defaultFleetLimit = 5

// fleetHealthConcurrency bounds the parallel per-instance health-score fetches so
// a large fleet is scored quickly without flooding Dasha with requests at once.
const fleetHealthConcurrency = 12

// fleetEntry is one instance's health in the fleet ranking. Score is a pointer so
// a legitimate worst-possible score of 0 is still emitted (omitempty on a value
// float64 would drop it, hiding the most critical instance); it is nil only when
// the score could not be read, which the Error field then explains.
type fleetEntry struct {
	Cluster    string   `json:"cluster"`
	Instance   string   `json:"instance"`
	Score      *float64 `json:"score,omitempty"`
	Source     string   `json:"source,omitempty"`
	InRecovery bool     `json:"in_recovery,omitempty"`
	Error      string   `json:"error,omitempty"` // set when this instance's score could not be read
}

// fleetHealth ranks every instance by health score ascending (worst first),
// tolerating per-instance failures so one bad host does not sink the whole scan.
func fleetHealth(ctx context.Context, c *DashaClient, limit int) (any, error) {
	if limit <= 0 {
		limit = defaultFleetLimit
	}

	clusters, err := c.Clusters(ctx)
	if err != nil {
		return nil, err
	}

	// Collect every target first, then score them concurrently: a large fleet
	// otherwise serializes one HTTP round-trip per instance.
	rows := make([]fleetEntry, 0)

	for _, cl := range clusters {
		name := deref(cl.Name)
		if cl.Instances == nil {
			continue
		}

		for _, inst := range *cl.Instances {
			rows = append(rows, fleetEntry{Cluster: name, Instance: deref(inst.HostName)}) //nolint:exhaustruct
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fleetHealthConcurrency)

	for i := range rows {
		g.Go(func() error {
			hs, herr := c.HealthScore(gctx, rows[i].Cluster, rows[i].Instance)
			if herr != nil {
				rows[i].Error = herr.Error()

				return nil // tolerate per-instance failures
			}

			score := hs.Score
			rows[i].Score = &score
			rows[i].Source = deref(hs.Source)
			rows[i].InRecovery = hs.InRecovery

			return nil
		})
	}

	_ = g.Wait() // no goroutine returns a non-nil error; failures are per-row

	// Scored instances first (ascending), unreadable ones last.
	slices.SortStableFunc(rows, func(a, b fleetEntry) int {
		if (a.Error == "") != (b.Error == "") {
			if a.Error == "" {
				return -1
			}

			return 1
		}

		return cmp.Compare(deref(a.Score), deref(b.Score))
	})

	if len(rows) > limit {
		rows = rows[:limit]
	}

	return map[string]any{"limit": limit, "worst": rows}, nil
}
