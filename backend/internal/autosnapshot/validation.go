package autosnapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
)

// validate is shared and safe for concurrent use (it caches struct metadata).
var validate = validator.New()

// Validate checks the full global config. Field-level bounds live in struct
// `validate` tags; only the cross-field arithmetic rule is hand-written.
func (c Config) Validate() error {
	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("invalid config | %w", err)
	}

	return validateSpikeCross(c.Defaults.ActivitySpike)
}

// ValidateOverride checks a per-cluster override by parsing it strictly, then
// validating the *effective* result (override merged onto the current
// defaults). Absent fields inherit valid defaults; present fields must keep the
// merged config valid — including the cross-field rule.
func (c Config) ValidateOverride(raw map[string]any) error {
	if len(raw) == 0 {
		return nil // empty override deletes the row
	}

	in, err := parseOverrideStrict(raw)
	if err != nil {
		return err
	}

	merged := c.Defaults
	in.applyTo(&merged)

	if err := validate.Struct(merged); err != nil {
		return fmt.Errorf("invalid override | %w", err)
	}

	return validateSpikeCross(merged.ActivitySpike)
}

func validateSpikeCross(s ActivitySpikeTrigger) error {
	if s.SpikeDuration > 2*s.WindowSize {
		return errors.New("spike_duration must be <= 2 * window_size")
	}

	return nil
}

// parseOverrideStrict surfaces malformed input that the lenient read path
// (EffectiveFor) silently ignores: unknown keys, wrong types, bad durations.
func parseOverrideStrict(raw map[string]any) (OverrideInput, error) {
	var in OverrideInput

	b, err := json.Marshal(raw)
	if err != nil {
		return in, fmt.Errorf("marshal override | %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf("decode override | %w", err)
	}

	return in, nil
}
