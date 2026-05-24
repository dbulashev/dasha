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
		{"negative", Weights{Connections: -0.1, Performance: 0.5}, true},
		{"NaN", Weights{Connections: math.NaN()}, true},
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
			"already normalized",
			DefaultWeights(),
			DefaultWeights(),
		},
		{
			"sum 2.0 → halved",
			Weights{Connections: 0.4, Performance: 0.5, Storage: 0.4, Replication: 0.3, Maintenance: 0.4},
			Weights{Connections: 0.2, Performance: 0.25, Storage: 0.2, Replication: 0.15, Maintenance: 0.2},
		},
		{
			"replica-tuned: replication dominates",
			Weights{Connections: 0.1, Performance: 0.1, Storage: 0.1, Replication: 0.6, Maintenance: 0.1},
			Weights{Connections: 0.1, Performance: 0.1, Storage: 0.1, Replication: 0.6, Maintenance: 0.1},
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
	w := Weights{Connections: 0.1, Performance: 0.2, Storage: 0.3, Replication: 0.4, Maintenance: 0.5}

	for name, want := range map[string]float64{
		"connections": 0.1,
		"performance": 0.2,
		"storage":     0.3,
		"replication": 0.4,
		"maintenance": 0.5,
		"unknown":     0,
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
		math.Abs(a.Maintenance-b.Maintenance) < eps
}
