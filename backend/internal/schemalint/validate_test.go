package schemalint

import "testing"

func TestValidate_AcceptsAnEmptyAndAFullSection(t *testing.T) {
	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("an absent section must be valid: %v", err)
	}

	cfg := Config{
		DisabledChecks:     []string{CodeUUIDInNonUUIDType},
		EnabledChecks:      []string{CodeRelationWithoutFk},
		IgnoreSchemas:      []string{"_timescaledb*", "cron"},
		SequenceThresholds: map[string]float64{"error": 3, "warning": 8, "notice": 15},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("a well-formed section must be valid: %v", err)
	}
}

func TestValidate_RejectsUnknownCheckNames(t *testing.T) {
	// The whole point: a typo that silently does nothing is how an operator ends
	// up thinking a noisy check is off.
	if err := (Config{DisabledChecks: []string{"sequence_exhausted"}}).Validate(); err == nil {
		t.Error("a misspelled code in disabled_checks must be rejected")
	}

	if err := (Config{EnabledChecks: []string{"nope"}}).Validate(); err == nil {
		t.Error("a misspelled code in enabled_checks must be rejected")
	}
}

func TestValidate_RejectsBrokenThresholds(t *testing.T) {
	tests := map[string]map[string]float64{
		"out of order":     {"error": 20, "warning": 10, "notice": 5},
		"above 100":        {"notice": 120},
		"zero":             {"error": 0},
		"negative":         {"warning": -1},
		"unknown level":    {"critical": 5},
		"warning below er": {"error": 10, "warning": 5},
	}

	for name, thresholds := range tests {
		if err := (Config{SequenceThresholds: thresholds}).Validate(); err == nil {
			t.Errorf("%s: expected a rejection, got none", name)
		}
	}
}

func TestValidate_RejectsBrokenSchemaMask(t *testing.T) {
	if err := (Config{IgnoreSchemas: []string{"[unclosed"}}).Validate(); err == nil {
		t.Error("a malformed glob must be rejected rather than silently matching nothing")
	}
}
