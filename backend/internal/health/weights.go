package health

import (
	"errors"
	"math"
)

// Weights holds per-category weights for health score aggregation.
// Sum must be > 0; values are typically normalized to sum to 1.0.
type Weights struct {
	Connections float64 `json:"connections"`
	Performance float64 `json:"performance"`
	Storage     float64 `json:"storage"`
	Replication float64 `json:"replication"`
	Maintenance float64 `json:"maintenance"`
}

// ErrInvalidWeights is returned when weight values are out of range
// or sum to zero.
var ErrInvalidWeights = errors.New("invalid weights")

// DefaultWeights returns the built-in default weights used when no
// per-cluster override is configured.
func DefaultWeights() Weights {
	return Weights{
		Connections: weightConnections,
		Performance: weightPerformance,
		Storage:     weightStorage,
		Replication: weightReplication,
		Maintenance: weightMaintenance,
	}
}

// Sum returns the sum of all weight values.
func (w Weights) Sum() float64 {
	return w.Connections + w.Performance + w.Storage + w.Replication + w.Maintenance
}

// Validate ensures all weights are non-negative and at least one is > 0.
func (w Weights) Validate() error {
	for _, v := range [...]float64{w.Connections, w.Performance, w.Storage, w.Replication, w.Maintenance} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return ErrInvalidWeights
		}
	}

	if w.Sum() <= 0 {
		return ErrInvalidWeights
	}

	return nil
}

// Normalize scales weights so that the sum equals 1.0.
// Returns the receiver unchanged when sum is already 1.0 within float tolerance.
func (w Weights) Normalize() Weights {
	sum := w.Sum()
	if sum <= 0 {
		return w
	}

	if math.Abs(sum-1.0) < 1e-9 {
		return w
	}

	return Weights{
		Connections: w.Connections / sum,
		Performance: w.Performance / sum,
		Storage:     w.Storage / sum,
		Replication: w.Replication / sum,
		Maintenance: w.Maintenance / sum,
	}
}

// byCategory returns the weight for a category name; zero if unknown.
func (w Weights) byCategory(name string) float64 {
	switch name {
	case "connections":
		return w.Connections
	case "performance":
		return w.Performance
	case "storage":
		return w.Storage
	case "replication":
		return w.Replication
	case "maintenance":
		return w.Maintenance
	default:
		return 0
	}
}
