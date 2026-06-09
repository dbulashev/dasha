package health

import (
	"errors"
	"math"
	"testing"
)

func TestDefaultWeights_SumsToOne(t *testing.T) {
	w := DefaultWeights()
	if math.Abs(w.Sum()-1.0) > 1e-9 {
		t.Fatalf("default weights sum = %v, want 1.0", w.Sum())
	}
}

func TestWeights_Validate(t *testing.T) {
	tests := []struct {
		name    string
		w       Weights
		wantErr bool
	}{
		{"default", DefaultWeights(), false},
		{"all zero", Weights{}, true},
		{"single positive", Weights{Connections: 1}, false},
		{"single new-category positive", Weights{Locks: 1}, false},
		{"negative", Weights{Connections: -0.1, Performance: 0.5}, true},
		{"negative on new category", Weights{Connections: 0.5, Horizon: -0.1}, true},
		{"NaN", Weights{Connections: math.NaN()}, true},
		{"NaN on new category", Weights{WalCheckpoint: math.NaN()}, true},
		{"+Inf", Weights{Connections: math.Inf(1)}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.w.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidWeights) {
				t.Fatalf("expected ErrInvalidWeights, got %v", err)
			}
		})
	}
}

func TestWeights_Normalize(t *testing.T) {
	tests := []struct {
		name string
		in   Weights
		want Weights
	}{
		{
			"already normalized (defaults)",
			DefaultWeights(),
			DefaultWeights(),
		},
		{
			"sum 2.0 → halved",
			Weights{
				Connections: 0.30, Performance: 0.30, Storage: 0.20,
				Replication: 0.30, Maintenance: 0.30,
				Horizon: 0.20, WalCheckpoint: 0.20, Locks: 0.20,
			},
			Weights{
				Connections: 0.15, Performance: 0.15, Storage: 0.10,
				Replication: 0.15, Maintenance: 0.15,
				Horizon: 0.10, WalCheckpoint: 0.10, Locks: 0.10,
			},
		},
		{
			"replica-tuned: replication dominates",
			Weights{
				Connections: 0.05, Performance: 0.05, Storage: 0.05,
				Replication: 0.60, Maintenance: 0.05,
				Horizon: 0.10, WalCheckpoint: 0.05, Locks: 0.05,
			},
			Weights{
				Connections: 0.05, Performance: 0.05, Storage: 0.05,
				Replication: 0.60, Maintenance: 0.05,
				Horizon: 0.10, WalCheckpoint: 0.05, Locks: 0.05,
			},
		},
		{
			"zero sum stays zero",
			Weights{},
			Weights{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			if !weightsClose(got, tc.want, 1e-9) {
				t.Fatalf("Normalize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWeights_ByCategory(t *testing.T) {
	w := Weights{
		Connections: 0.1, Performance: 0.2, Storage: 0.3, Replication: 0.4, Maintenance: 0.5,
		Horizon: 0.6, WalCheckpoint: 0.7, Locks: 0.8,
	}

	for name, want := range map[Category]float64{
		"connections":    0.1,
		"performance":    0.2,
		"storage":        0.3,
		"replication":    0.4,
		"maintenance":    0.5,
		"horizon":        0.6,
		"wal_checkpoint": 0.7,
		"locks":          0.8,
		"unknown":        0,
	} {
		if got := w.byCategory(name); got != want {
			t.Errorf("byCategory(%q) = %v, want %v", name, got, want)
		}
	}
}

func weightsClose(a, b Weights, eps float64) bool {
	return math.Abs(a.Connections-b.Connections) < eps &&
		math.Abs(a.Performance-b.Performance) < eps &&
		math.Abs(a.Storage-b.Storage) < eps &&
		math.Abs(a.Replication-b.Replication) < eps &&
		math.Abs(a.Maintenance-b.Maintenance) < eps &&
		math.Abs(a.Horizon-b.Horizon) < eps &&
		math.Abs(a.WalCheckpoint-b.WalCheckpoint) < eps &&
		math.Abs(a.Locks-b.Locks) < eps
}
