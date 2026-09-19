package opensearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// validIndex constrains an expanded index pattern to what may appear in a
// request path: no traversal, no query string, no host of its own.
var validIndex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.+*,\-]*$`)

func expandIndex(tmpl string, data source.TemplateData) (string, error) {
	index, err := source.Expand(tmpl, data)
	if err != nil {
		return "", err
	}

	if !validIndex.MatchString(index) {
		return "", fmt.Errorf("%w: index %q is not a valid index pattern", source.ErrConfig, index)
	}

	return index, nil
}

// searchRequest is the body of one _search call. Every user-supplied value
// enters it as a JSON literal, never as a fragment of a query expression.
type searchRequest struct {
	Size           int              `json:"size"`
	Sort           []map[string]any `json:"sort"`
	Query          map[string]any   `json:"query"`
	TrackTotalHits bool             `json:"track_total_hits"`
}

// buildSearch assembles the bounded query from the filter Narrow has already
// reduced to what the index can execute without dropping a matching record.
func buildSearch(
	fm source.FieldMap,
	selector map[string]string,
	f source.Filter,
	from, to time.Time,
	size int,
	trackTotal bool,
) searchRequest {
	filters := []map[string]any{
		{"range": map[string]any{fm.Timestamp: map[string]any{
			"gte":    from.UTC().Format(time.RFC3339Nano),
			"lte":    to.UTC().Format(time.RFC3339Nano),
			"format": "strict_date_optional_time",
		}}},
	}

	if len(f.Severities) > 0 {
		filters = append(filters, map[string]any{
			"terms": map[string]any{fm.Keyword(fm.Severity): f.Severities},
		})
	}

	if f.Host != "" {
		filters = append(filters, hostFilter(fm, f.Host))
	}

	if f.QueryID != nil {
		filters = append(filters, map[string]any{
			"term": map[string]any{fm.Keyword(fm.QueryID): *f.QueryID},
		})
	}

	for _, phrase := range f.Contains {
		filters = append(filters, map[string]any{
			"match_phrase": map[string]any{fm.Text: phrase},
		})
	}

	for _, k := range sortedKeys(selector) {
		filters = append(filters, map[string]any{
			"term": map[string]any{fm.Keyword(k): selector[k]},
		})
	}

	return searchRequest{
		Size:           size,
		Sort:           []map[string]any{{fm.Timestamp: map[string]any{"order": "asc"}}},
		Query:          map[string]any{"bool": map[string]any{"filter": filters}},
		TrackTotalHits: trackTotal,
	}
}

// hostFilter matches the cluster host name against the host field. In suffix
// mode the index holds an FQDN whose first label is the configured host.
func hostFilter(fm source.FieldMap, host string) map[string]any {
	field := fm.Keyword(fm.Host)

	if fm.HostMatch != source.HostMatchSuffix {
		return map[string]any{"term": map[string]any{field: host}}
	}

	return map[string]any{"bool": map[string]any{
		"should": []map[string]any{
			{"term": map[string]any{field: host}},
			{"prefix": map[string]any{field: host + "."}},
		},
		"minimum_should_match": 1,
	}}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// searchResponse is the part of the _search answer Dasha reads.
type searchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []hit `json:"hits"`
	} `json:"hits"`
}

type hit struct {
	ID     string    `json:"_id"`
	Source hitSource `json:"_source"`
}

// hitSource keeps numbers as json.Number: a float64 cannot hold an int64 query_id.
type hitSource map[string]any

func (s *hitSource) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()

	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return fmt.Errorf("decode _source: %w", err)
	}

	*s = m

	return nil
}

// fieldCapsResponse maps a field name to the types it has across the indices
// the pattern resolves to, unmappedType among them where an index does not hold
// the field.
type fieldCapsResponse struct {
	Fields map[string]map[string]struct {
		Type string `json:"type"`
	} `json:"fields"`
}

// flatten turns a decoded _source into the dotted-key string map the rest of
// the pipeline works with, so a field map may name a nested field as "host.name".
func flatten(prefix string, src map[string]any, out map[string]string) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		if nested, ok := v.(map[string]any); ok {
			flatten(key, nested, out)

			continue
		}

		out[key] = scalar(v)
	}
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}

		return string(b)
	}
}

// streamsFromConfig resolves every configured stream into an index pattern and
// a validated field map.
func streamsFromConfig(cfg config.LogSourceConfig) (map[string]streamDef, error) {
	out := make(map[string]streamDef, len(cfg.Streams))

	for name, sc := range cfg.Streams {
		fm, err := source.FieldMapFromConfig(sc.FieldMap)
		if err != nil {
			return nil, fmt.Errorf("streams.%s: %w", name, err)
		}

		probe := source.TemplateData{Cluster: source.ProbeCluster}

		if _, err := expandIndex(sc.Index, probe); err != nil {
			return nil, fmt.Errorf("streams.%s: %w", name, err)
		}

		if _, err := source.ExpandMap(sc.Selector, probe); err != nil {
			return nil, fmt.Errorf("streams.%s: %w", name, err)
		}

		out[name] = streamDef{
			index:    sc.Index,
			selector: sc.Selector,
			fields:   fm,
		}
	}

	return out, nil
}
