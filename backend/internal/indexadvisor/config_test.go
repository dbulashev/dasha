package indexadvisor

import "testing"

func TestConfigWithDefaults(t *testing.T) {
	cfg := Config{}.WithDefaults() //nolint:exhaustruct // the point is that a zero config works

	if cfg.MaxQueries != DefaultMaxQueries {
		t.Errorf("MaxQueries = %d, want %d", cfg.MaxQueries, DefaultMaxQueries)
	}

	if cfg.MinTableRows != DefaultMinTableRows {
		t.Errorf("MinTableRows = %d, want %d", cfg.MinTableRows, DefaultMinTableRows)
	}

	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %s, want %s", cfg.Timeout, DefaultTimeout)
	}
}

// Configuring a wider index than the ceiling is capped rather than rejected: the
// number past four is unjustifiable in this step either way, and refusing to
// start over it would be a harsh answer to a tuning knob.
func TestConfigCapsIndexWidth(t *testing.T) {
	cfg := Config{MaxIndexColumns: 9}.WithDefaults() //nolint:exhaustruct

	if cfg.MaxIndexColumns != MaxIndexColumnsCeiling {
		t.Errorf("MaxIndexColumns = %d, want the ceiling %d", cfg.MaxIndexColumns, MaxIndexColumnsCeiling)
	}
}

func TestConfigEnabledDefaultsToOn(t *testing.T) {
	if !(Config{}).IsEnabled() { //nolint:exhaustruct
		t.Error("an absent enabled key must read as on")
	}

	off := false
	if (Config{Enabled: &off}).IsEnabled() { //nolint:exhaustruct
		t.Error("enabled: false must read as off")
	}
}
