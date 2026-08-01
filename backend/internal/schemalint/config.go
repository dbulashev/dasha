package schemalint

import (
	"path/filepath"
	"slices"
	"time"
)

// Default sequence thresholds, in percent of values still free.
const (
	DefaultSequenceErrorPct   = 5.0
	DefaultSequenceWarningPct = 10.0
	DefaultSequenceNoticePct  = 20.0
)

// DefaultCacheTTL is how long a report stays reusable. Schema defects change on
// deploys, not per second; the page's refresh button covers the impatient case.
const DefaultCacheTTL = 5 * time.Minute

// Config is the global schema_lint section. Global rather than per-cluster, as
// with the other analysis settings.
type Config struct {
	DisabledChecks     []string           `mapstructure:"disabled_checks"`
	EnabledChecks      []string           `mapstructure:"enabled_checks"`
	IgnoreSchemas      []string           `mapstructure:"ignore_schemas"`
	SequenceThresholds map[string]float64 `mapstructure:"sequence_thresholds"`
	CacheTTL           time.Duration      `mapstructure:"cache_ttl"`
}

// enabled reports whether a check should run: the registry default, overridden
// by either config list. disabled_checks wins, so an operator silencing a noisy
// check cannot be surprised by it reappearing.
func (c Config) enabled(chk Check) bool {
	if slices.Contains(c.DisabledChecks, chk.Code) {
		return false
	}

	if slices.Contains(c.EnabledChecks, chk.Code) {
		return true
	}

	return chk.DefaultEnabled
}

// ignoredSchema matches a schema against the configured masks. Matching happens
// here rather than in SQL so the rule is one implementation for every check and
// is covered by a unit test. System schemas are already excluded in SQL.
func (c Config) ignoredSchema(schema string) bool {
	for _, mask := range c.IgnoreSchemas {
		if ok, err := filepath.Match(mask, schema); err == nil && ok {
			return true
		}
	}

	return false
}

// sequenceThreshold returns the configured percentage for a level, or its default.
func (c Config) sequenceThreshold(l Level) float64 {
	if v, ok := c.SequenceThresholds[string(l)]; ok {
		return v
	}

	switch l {
	case LevelError:
		return DefaultSequenceErrorPct
	case LevelWarning:
		return DefaultSequenceWarningPct
	case LevelNotice:
		return DefaultSequenceNoticePct
	default:
		return DefaultSequenceNoticePct
	}
}

// LevelForFreePct is sequenceLevel for callers outside this package — the
// health-score rule, so that the page and the recommendation cannot disagree on
// where a threshold lies, including at the exact boundary. A nil map means the
// defaults.
func LevelForFreePct(freePct float64, thresholds map[string]float64) (Level, bool) {
	return Config{SequenceThresholds: thresholds}.sequenceLevel(freePct) //nolint:exhaustruct
}

// sequenceLevel maps free headroom to a level. The second result is false when
// the sequence has enough room to not be worth reporting.
func (c Config) sequenceLevel(freePct float64) (Level, bool) {
	switch {
	case freePct < c.sequenceThreshold(LevelError):
		return LevelError, true
	case freePct < c.sequenceThreshold(LevelWarning):
		return LevelWarning, true
	case freePct < c.sequenceThreshold(LevelNotice):
		return LevelNotice, true
	default:
		return "", false
	}
}

// CacheTTLOrDefault keeps a zero config usable.
func (c Config) CacheTTLOrDefault() time.Duration {
	if c.CacheTTL <= 0 {
		return DefaultCacheTTL
	}

	return c.CacheTTL
}
