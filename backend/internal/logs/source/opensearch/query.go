package opensearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// validIndex constrains an expanded index pattern to what may appear in a
// request path: no traversal, no query string, no host of its own.
var validIndex = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.+*,\-]*$`)

// templateData holds the substitutions allowed in index patterns and selector
// values.
type templateData struct {
	Cluster string
	Host    string
}

func expand(tmpl string, data templateData) (string, error) {
	t, err := template.New("t").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("%w: template %q: %w", source.ErrConfig, tmpl, err)
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", fmt.Errorf("%w: template %q: %w", source.ErrConfig, tmpl, err)
	}

	return out.String(), nil
}

func expandIndex(tmpl string, data templateData) (string, error) {
	index, err := expand(tmpl, data)
	if err != nil {
		return "", err
	}

	if !validIndex.MatchString(index) {
		return "", fmt.Errorf("%w: index %q is not a valid index pattern", source.ErrConfig, index)
	}

	return index, nil
}

func expandSelector(selector map[string]string, data templateData) (map[string]string, error) {
	if len(selector) == 0 {
		return nil, nil
	}

	out := make(map[string]string, len(selector))

	for k, v := range selector {
		value, err := expand(v, data)
		if err != nil {
			return nil, err
		}

		out[k] = value
	}

	return out, nil
}

// searchRequest is the body of one _search call. Every user-supplied value
// enters it as a JSON literal, never as a fragment of a query expression.
type searchRequest struct {
	Size           int              `json:"size"`
	Sort           []map[string]any `json:"sort"`
	Query          map[string]any   `json:"query"`
	TrackTotalHits bool             `json:"track_total_hits"`
}

// buildSearch assembles the bounded query. Only filters that cannot drop a
// matching record are pushed down: the time range, severity and host.
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
			"terms": map[string]any{fm.Severity: f.Severities},
		})
	}

	if f.Host != "" {
		filters = append(filters, hostFilter(fm, f.Host))
	}

	for _, k := range sortedKeys(selector) {
		filters = append(filters, map[string]any{
			"term": map[string]any{k: selector[k]},
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
	if fm.HostMatch != source.HostMatchSuffix {
		return map[string]any{"term": map[string]any{fm.Host: host}}
	}

	return map[string]any{"bool": map[string]any{
		"should": []map[string]any{
			{"term": map[string]any{fm.Host: host}},
			{"prefix": map[string]any{fm.Host: host + "."}},
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
	ID     string         `json:"_id"`
	Source map[string]any `json:"_source"`
}

// fieldCapsResponse maps a field name to the types it has across the indices
// the pattern resolves to.
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

// timeLayouts are tried in order when the timestamp field is a string.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
}

// parseTime reads the timestamp field of a record: a date string, or epoch
// milliseconds when the index stores it as a number.
func parseTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case string:
		trimmed := strings.TrimSpace(t)

		for _, layout := range timeLayouts {
			if ts, err := time.Parse(layout, trimmed); err == nil {
				return ts.UTC(), nil
			}
		}

		if ms, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return time.UnixMilli(ms).UTC(), nil
		}

		return time.Time{}, fmt.Errorf("%w: timestamp %q has no recognized format", source.ErrConfig, t)
	case float64:
		return time.UnixMilli(int64(t)).UTC(), nil
	case json.Number:
		ms, err := t.Int64()
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: timestamp %q is not a number", source.ErrConfig, t.String())
		}

		return time.UnixMilli(ms).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("%w: record has no usable timestamp", source.ErrConfig)
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

		out[name] = streamDef{
			index:    sc.Index,
			selector: sc.Selector,
			fields:   fm,
		}
	}

	return out, nil
}
