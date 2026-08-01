package schemalint

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Validate rejects a schema_lint section that cannot do what it says. A typo in
// a check name is the case that matters: silently doing nothing is exactly how
// an operator ends up believing a noisy check was switched off.
func (c Config) Validate() error {
	known := make([]string, 0, len(registry))
	for _, chk := range registry {
		known = append(known, chk.Code)
	}

	for _, list := range []struct {
		name  string
		codes []string
	}{
		{"disabled_checks", c.DisabledChecks},
		{"enabled_checks", c.EnabledChecks},
	} {
		for _, code := range list.codes {
			if !slices.Contains(known, code) {
				return fmt.Errorf("%s: unknown check %q (known: %s)", list.name, code, strings.Join(known, ", "))
			}
		}
	}

	for _, mask := range c.IgnoreSchemas {
		if _, err := filepath.Match(mask, ""); err != nil {
			return fmt.Errorf("ignore_schemas: %q is not a valid pattern | %w", mask, err)
		}
	}

	return c.validateSequenceThresholds()
}

// validateSequenceThresholds keeps the three levels in the order the classifier
// assumes: a warning threshold below the error one would make the warning band
// unreachable, and the rule would silently never fire.
func (c Config) validateSequenceThresholds() error {
	for name := range c.SequenceThresholds {
		switch Level(name) {
		case LevelError, LevelWarning, LevelNotice:
		default:
			return fmt.Errorf("sequence_thresholds: unknown level %q (expected error, warning, notice)", name)
		}
	}

	var (
		errorPct   = c.sequenceThreshold(LevelError)
		warningPct = c.sequenceThreshold(LevelWarning)
		noticePct  = c.sequenceThreshold(LevelNotice)
	)

	for name, v := range map[string]float64{"error": errorPct, "warning": warningPct, "notice": noticePct} {
		if v <= 0 || v > 100 {
			return fmt.Errorf("sequence_thresholds.%s: %v is outside (0, 100]", name, v)
		}
	}

	if errorPct > warningPct || warningPct > noticePct {
		return fmt.Errorf(
			"sequence_thresholds: expected error <= warning <= notice (percent of values still free), got %v / %v / %v",
			errorPct, warningPct, noticePct)
	}

	return nil
}
