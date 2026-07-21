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

	return validateMergedDefaults(merged)
}

// ValidateEffective re-checks an already-stored override against these defaults:
// it merges leniently (the raw was validated when written) and verifies the
// effective result still holds. Used when defaults change, so a tighter default
// can't silently invalidate a persisted override.
func (c Config) ValidateEffective(raw map[string]any) error {
	return validateMergedDefaults(c.EffectiveFor(raw))
}

// validateMergedDefaults checks a fully-merged TriggerDefaults: field bounds via
// tags plus the cross-field rule.
func validateMergedDefaults(td TriggerDefaults) error {
	if err := validate.Struct(td); err != nil {
		return fmt.Errorf("invalid override | %w", err)
	}

	return validateSpikeCross(td.ActivitySpike)
}

// validateSpikeCross keeps the baseline window long enough for the spike to
// stand out against it. The baseline is a moving average over window_size, so a
// spike that must hold for a comparable stretch inflates its own baseline and
// the crossing dies out before spike_duration elapses — the rule becomes
// unreachable. Requiring the window to be at least twice the duration leaves
// the majority of the baseline made of pre-spike samples.
func validateSpikeCross(s ActivitySpikeTrigger) error {
	if 2*s.SpikeDuration > s.WindowSize {
		return errors.New("spike_duration must be <= window_size / 2")
	}

	return nil
}

// parseOverrideStrict surfaces malformed input that the lenient read path
// (EffectiveFor) silently ignores: unknown keys, wrong types, bad durations.
func parseOverrideStrict(raw map[string]any) (OverrideInput, error) {
	var in OverrideInput

	if err := rejectNullFields(raw, ""); err != nil {
		return in, err
	}

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

// rejectNullFields fails on any explicit JSON null in the override payload. A
// null decodes into a nil pointer exactly like an absent field, so without this
// an admin could "set" a field to null and have it stored as a silent no-op.
func rejectNullFields(v any, path string) error {
	switch t := v.(type) {
	case nil:
		if path == "" {
			path = "override"
		}

		return fmt.Errorf("override field %q must not be null", path)
	case map[string]any:
		for k, child := range t {
			name := k
			if path != "" {
				name = path + "." + k
			}

			if err := rejectNullFields(child, name); err != nil {
				return err
			}
		}
	}

	return nil
}
