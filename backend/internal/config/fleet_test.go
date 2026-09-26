package config

import (
	"testing"
	"time"
)

func TestFleetConfigValidate(t *testing.T) {
	zero, negative := 0, -1

	tests := []struct {
		name    string
		cfg     FleetConfig
		wantErr bool
	}{
		{"defaults", FleetConfig{}, false},
		{"zero margin", FleetConfig{CandidateMargin: &zero}, false},
		{"negative margin", FleetConfig{CandidateMargin: &negative}, true},
		{"budget not above instance timeout", FleetConfig{Budget: 5 * time.Second, InstanceTimeout: 5 * time.Second}, true},
		{"budget above max", FleetConfig{Budget: MaxFleetBudget + time.Second}, true},
		{"limit above api max", FleetConfig{DefaultLimit: MaxFleetLimit + 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFleetConfigKeepsZeroMargin(t *testing.T) {
	zero := 0

	if got := (FleetConfig{CandidateMargin: &zero}).WithDefaults(); *got.CandidateMargin != 0 {
		t.Errorf("candidate_margin %d, want 0 kept", *got.CandidateMargin)
	}
}
