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

// unmappedType is what _field_caps names the indices of the pattern that do not
// hold the field at all.
const unmappedType = "unmapped"

// unsearchableType stands for a type mapped with index: false: a query on such
// a field fails instead of matching nothing.
const unsearchableType = "unsearchable"

type fieldTypeEntry struct {
	types map[string][]string
	at    time.Time
}

// Narrow keeps the filter parts the index can execute. Severity and host match
// keyword fields and always push down; query_id and words depend on how the
// field is indexed, and a mapping that cannot be read narrows nothing: a phrase
// sent to a keyword field matches no record at all, and a silently empty answer
// is worse than a large scan. Terms go as a whole or not at all. Substrings,
// database and user are not sent.
func (p *Provider) Narrow(ctx context.Context, sp source.StreamParams) source.Narrowed {
	out := source.Narrowed{ //nolint:exhaustruct
		Filter: source.Filter{ //nolint:exhaustruct
			Severities: sp.Filter.Severities,
			Host:       sp.Filter.Host,
		},
		TextSkip: source.SkipText(sp.Filter, source.TextSkipUnsupported),
	}

	if len(sp.Filter.Terms) > 0 {
		out.TextSkip = source.TextSkipMapping
	}

	def, ok := p.streams[sp.Stream]
	if !ok || (sp.Filter.QueryID == nil && len(sp.Filter.Words) == 0 && len(sp.Filter.Terms) == 0) {
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

	if !fullyAnalyzed(types, fm.Text) {
		return out
	}

	out.Words = sp.Filter.Words

	if len(sp.Filter.Terms) > 0 {
		out.Terms = sp.Filter.Terms
		out.TextSkip = source.TextSkipNone
	}

	return out
}

// fullyAnalyzed reports whether every index behind the pattern tokenizes the
// field: a phrase sent to one that maps it as a keyword matches nothing there.
func fullyAnalyzed(types map[string][]string, field string) bool {
	if len(types[field]) == 0 {
		return false
	}

	for _, t := range types[field] {
		if !analyzedType(t) {
			return false
		}
	}

	return true
}

func analyzedType(t string) bool {
	return t == "text" || t == "match_only_text"
}

// exact reports whether a term matches the field as written in every index
// behind the pattern: mapped there, and mapped as something the store does not
// tokenize. An index rolled over from a template that does not hold the field
// answers nothing for it, which is the outcome push-down must never cause —
// hence include_unmapped on the mapping read.
func exact(types map[string][]string, field string) bool {
	if len(types[field]) == 0 {
		return false
	}

	for _, t := range types[field] {
		if t == unmappedType || t == unsearchableType || analyzedType(t) {
			return false
		}
	}

	return true
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

	types := fieldTypesOf(caps)
	p.fieldTypeCache.Store(index, fieldTypeEntry{types: types, at: time.Now()})

	return types
}

// fieldTypesOf lists the types of every field across the indices, a type mapped
// with index: false as unsearchableType.
func fieldTypesOf(caps fieldCapsResponse) map[string][]string {
	types := make(map[string][]string, len(caps.Fields))

	for field, byType := range caps.Fields {
		names := make([]string, 0, len(byType))
		for name, c := range byType {
			if !c.Searchable && name != unmappedType {
				name = unsearchableType
			}

			names = append(names, name)
		}

		types[field] = names
	}

	return types
}

func (p *Provider) fieldCaps(ctx context.Context, index string) (fieldCapsResponse, error) {
	var caps fieldCapsResponse

	err := p.client.JSON(ctx, "GET",
		"/"+url.PathEscape(index)+"/_field_caps?fields=*&include_unmapped=true", nil, &caps)

	return caps, err
}
