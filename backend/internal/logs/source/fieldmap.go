package source

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dbulashev/dasha/internal/config"
)

// FieldMap binds the roles Dasha displays and filters on to the field names of
// one stream.
type FieldMap struct {
	// Timestamp and Host are never part of a preset: PostgreSQL writes neither
	// into the log, the delivery agent names them.
	Timestamp string
	Severity  string
	Text      string
	Host      string
	Database  string
	User      string
	PID       string
	// Mask lists the free-text fields whose values pass through sanitize.SQL()
	// before leaving the backend.
	Mask []string
	// Severities are the accepted values in the casing the source stores them.
	Severities []string
	// KeywordFields maps a field to the field an exact-match filter must use
	// when the store indexes the field as analyzed text.
	KeywordFields map[string]string
	// HostMatch selects how a cluster host name is matched against the host
	// field of the source.
	HostMatch string
}

// Host matching modes.
const (
	HostMatchExact  = "exact"
	HostMatchSuffix = "suffix"
)

// Empty reports whether the map is unset.
func (m FieldMap) Empty() bool {
	return m.Text == ""
}

// Keyword returns the field an exact-match filter must use for field. Without
// an override the field is assumed to be indexed as keyword already.
func (m FieldMap) Keyword(field string) string {
	if kw, ok := m.KeywordFields[field]; ok && kw != "" {
		return kw
	}

	return field
}

// CanonicalSeverity matches a user-supplied severity against the accepted
// values, ignoring case, and returns the spelling the source uses.
func (m FieldMap) CanonicalSeverity(raw string) (string, bool) {
	for _, v := range m.Severities {
		if strings.EqualFold(v, raw) {
			return v, true
		}
	}

	return "", false
}

var presets = map[string]FieldMap{
	PresetCSVLog: {
		Severity:   "error_severity",
		Text:       "message",
		Database:   "database_name",
		User:       "user_name",
		PID:        "process_id",
		Mask:       []string{"message", "query", "internal_query", "detail", "hint", "context"},
		Severities: pgSeverities,
	},
	PresetJSONLog: {
		Severity:   "error_severity",
		Text:       "message",
		Database:   "dbname",
		User:       "user",
		PID:        "pid",
		Mask:       []string{"message", "statement", "internal_query", "detail", "hint", "context"},
		Severities: pgSeverities,
	},
	PresetOdyssey: {
		Severity:   "level",
		Text:       "text",
		Database:   "db",
		User:       "user",
		PID:        "pid",
		Mask:       []string{"text"},
		Severities: []string{"debug", "info", "warning", "error", "fatal"},
	},
}

// Preset names of the supported log formats. PresetNone leaves every role to
// the configuration.
const (
	PresetCSVLog  = "csvlog"
	PresetJSONLog = "jsonlog"
	PresetOdyssey = "odyssey"
	PresetNone    = "none"
)

var pgSeverities = []string{
	"DEBUG", "LOG", "INFO", "NOTICE", "WARNING", "ERROR", "FATAL", "PANIC",
}

// Preset returns a copy of a named preset.
func Preset(name string) (FieldMap, bool) {
	fm, ok := presets[name]
	if !ok {
		return FieldMap{}, false
	}

	fm.Mask = slices.Clone(fm.Mask)
	fm.Severities = slices.Clone(fm.Severities)

	return fm, true
}

// Role names reported by a source check.
const (
	RoleTimestamp = "timestamp"
	RoleSeverity  = "severity"
	RoleText      = "text"
	RoleHost      = "host"
	RoleDatabase  = "database"
	RoleUser      = "user"
	RolePID       = "pid"
)

// Roles returns the configured role-to-field pairs, skipping unset roles.
func (m FieldMap) Roles() map[string]string {
	roles := make(map[string]string, 7)

	for role, field := range map[string]string{
		RoleTimestamp: m.Timestamp,
		RoleSeverity:  m.Severity,
		RoleText:      m.Text,
		RoleHost:      m.Host,
		RoleDatabase:  m.Database,
		RoleUser:      m.User,
		RolePID:       m.PID,
	} {
		if field != "" {
			roles[role] = field
		}
	}

	return roles
}

// FieldMapFromConfig applies the configured overrides on top of a preset and
// checks that every role Dasha needs is bound to a field.
func FieldMapFromConfig(c config.LogFieldMapConfig) (FieldMap, error) {
	fm := FieldMap{}

	if c.Preset != "" && c.Preset != PresetNone {
		p, ok := Preset(c.Preset)
		if !ok {
			return FieldMap{}, fmt.Errorf("%w: unknown field map preset %q", ErrConfig, c.Preset)
		}

		fm = p
	}

	for _, o := range []struct {
		dst *string
		src string
	}{
		{&fm.Timestamp, c.Timestamp},
		{&fm.Severity, c.Severity},
		{&fm.Text, c.Text},
		{&fm.Host, c.Host},
		{&fm.Database, c.Database},
		{&fm.User, c.User},
		{&fm.PID, c.PID},
		{&fm.HostMatch, c.HostMatch},
	} {
		if o.src != "" {
			*o.dst = o.src
		}
	}

	if len(c.Mask) > 0 {
		fm.Mask = slices.Clone(c.Mask)
	}

	if len(c.Severities) > 0 {
		fm.Severities = slices.Clone(c.Severities)
	}

	fm.KeywordFields = maps.Clone(c.KeywordFields)

	if fm.HostMatch == "" {
		fm.HostMatch = HostMatchExact
	}

	if fm.HostMatch != HostMatchExact && fm.HostMatch != HostMatchSuffix {
		return FieldMap{}, fmt.Errorf("%w: host_match %q (want exact|suffix)", ErrConfig, fm.HostMatch)
	}

	var missing []string

	for role, field := range map[string]string{
		RoleTimestamp: fm.Timestamp,
		RoleSeverity:  fm.Severity,
		RoleText:      fm.Text,
		RoleHost:      fm.Host,
	} {
		if field == "" {
			missing = append(missing, role)
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)

		return FieldMap{}, fmt.Errorf("%w: field map has no %s", ErrConfig, strings.Join(missing, ", "))
	}

	if len(fm.Severities) == 0 {
		return FieldMap{}, fmt.Errorf("%w: field map lists no severity values", ErrConfig)
	}

	fm.Mask = resolveMask(fm)

	return fm, nil
}

// resolveMask expands the configured mask onto the field names records really
// carry: the text field is always sanitized, and a bare entry also covers the
// same field nested beside the text field, so a text of "pg.message" masks
// "pg.detail" as well as "detail".
func resolveMask(fm FieldMap) []string {
	prefix := ""
	if i := strings.LastIndex(fm.Text, "."); i >= 0 {
		prefix = fm.Text[:i+1]
	}

	out := make([]string, 0, len(fm.Mask)+1)
	seen := make(map[string]struct{}, len(fm.Mask)+1)

	add := func(field string) {
		if _, dup := seen[field]; field == "" || dup {
			return
		}

		seen[field] = struct{}{}
		out = append(out, field)
	}

	add(fm.Text)

	for _, field := range fm.Mask {
		add(field)

		if prefix != "" && !strings.Contains(field, ".") {
			add(prefix + field)
		}
	}

	return out
}
