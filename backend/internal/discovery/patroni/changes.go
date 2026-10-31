package patroni

import (
	"slices"

	"go.uber.org/zap"
)

// maxListedScopes caps the scope list in the first-cycle log line.
const maxListedScopes = 50

// logChanges logs what next changes against the published state.
func (p *Provider) logChanges(next map[string]scopeState) {
	names := sortedKeys(next)

	if p.state == nil {
		fields := []zap.Field{zap.String("entry", p.entry), zap.Int("count", len(names))}
		if len(names) <= maxListedScopes {
			fields = append(fields, zap.Strings("scopes", names))
		}

		p.logger.Info("patroni scopes discovered", fields...)

		return
	}

	prev := sortedKeys(p.state)

	if added, removed := missingFrom(names, prev), missingFrom(prev, names); len(added) > 0 || len(removed) > 0 {
		p.logger.Info("patroni scopes changed",
			zap.String("entry", p.entry),
			zap.Strings("added", added),
			zap.Strings("removed", removed),
		)
	}

	for _, scope := range names {
		old, ok := p.state[scope]
		if !ok {
			continue
		}

		cur := next[scope]

		logDiff(p.logger, "patroni scope hosts changed", p.entry, scope, old.Hosts, cur.Hosts)
		logDiff(p.logger, "patroni scope databases changed", p.entry, scope, old.Published, cur.Published)
	}
}

func logDiff(logger *zap.Logger, msg, entry, scope string, old, cur []string) {
	added, removed := missingFrom(cur, old), missingFrom(old, cur)
	if len(added) == 0 && len(removed) == 0 {
		return
	}

	logger.Info(msg,
		zap.String("entry", entry),
		zap.String("scope", scope),
		zap.Strings("added", added),
		zap.Strings("removed", removed),
	)
}

func sortedKeys(m map[string]scopeState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// missingFrom returns the names of from that other does not contain.
func missingFrom(from, other []string) []string {
	var diff []string

	for _, name := range from {
		if !slices.Contains(other, name) {
			diff = append(diff, name)
		}
	}

	return diff
}
