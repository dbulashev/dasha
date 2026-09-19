package opensearch

import (
	"context"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/dbulashev/dasha/internal/logs/source"
)

// fieldTypeTTL bounds how long the mapping of an index pattern is trusted: a
// new index rolling into the pattern may carry a field the old ones did not.
const fieldTypeTTL = 15 * time.Minute

type fieldTypeEntry struct {
	types map[string][]string
	at    time.Time
}

// Narrow keeps the filter parts the index can execute. Severity and host match
// keyword fields and always push down; query_id and a phrase depend on how the
// field is indexed, and a mapping that cannot be read narrows nothing: a phrase
// sent to a keyword field matches no record at all, and a silently empty answer
// is worse than a large scan.
func (p *Provider) Narrow(ctx context.Context, sp source.StreamParams) source.Filter {
	out := source.Filter{ //nolint:exhaustruct
		Severities: sp.Filter.Severities,
		Host:       sp.Filter.Host,
	}

	def, ok := p.streams[sp.Stream]
	if !ok || (sp.Filter.QueryID == nil && len(sp.Filter.Contains) == 0) {
		return out
	}

	index, err := expandIndex(def.index, source.TemplateData{Cluster: sp.Cluster.Name.String()})
	if err != nil {
		return out
	}

	types := p.fieldTypes(ctx, index)
	if types == nil {
		return out
	}

	fm := def.fields

	if sp.Filter.QueryID != nil && fm.QueryID != "" && exact(types, fm.Keyword(fm.QueryID)) {
		out.QueryID = sp.Filter.QueryID
	}

	if len(sp.Filter.Contains) > 0 && analyzed(types, fm.Text) {
		out.Contains = sp.Filter.Contains
	}

	return out
}

// analyzed reports whether the store tokenizes the field, which a phrase needs.
func analyzed(types map[string][]string, field string) bool {
	for _, t := range types[field] {
		if t == "text" || t == "match_only_text" {
			return true
		}
	}

	return false
}

// exact reports whether a term matches the field as written: it is mapped, and
// mapped as something the store does not tokenize. A field the index does not
// hold at all matches nothing, which is the outcome push-down must never cause.
func exact(types map[string][]string, field string) bool {
	return len(types[field]) > 0 && !analyzed(types, field)
}

// fieldTypes reads the mapping of an index pattern, cached for fieldTypeTTL. A
// store that will not answer returns nil.
func (p *Provider) fieldTypes(ctx context.Context, index string) map[string][]string {
	if v, ok := p.fieldTypeCache.Load(index); ok {
		if e, valid := v.(fieldTypeEntry); valid && time.Since(e.at) < fieldTypeTTL {
			return e.types
		}
	}

	caps, err := p.fieldCaps(ctx, index)
	if err != nil {
		p.logger.Warn("opensearch: field mapping unavailable, scan not narrowed",
			zap.String("index", index), zap.Error(err))

		return nil
	}

	types := make(map[string][]string, len(caps.Fields))

	for field, byType := range caps.Fields {
		names := make([]string, 0, len(byType))
		for name := range byType {
			names = append(names, name)
		}

		types[field] = names
	}

	p.fieldTypeCache.Store(index, fieldTypeEntry{types: types, at: time.Now()})

	return types
}

func (p *Provider) fieldCaps(ctx context.Context, index string) (fieldCapsResponse, error) {
	var caps fieldCapsResponse

	err := p.client.JSON(ctx, "GET", "/"+url.PathEscape(index)+"/_field_caps?fields=*", nil, &caps)

	return caps, err
}
