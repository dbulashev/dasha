package victorialogs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

// streamDef is one configured stream; expand resolves its templates for a
// cluster and returns the same shape.
type streamDef struct {
	streamSelector map[string]string
	selector       map[string]string
	query          string
	fields         source.FieldMap
}

func (d streamDef) expand(data source.TemplateData) (streamDef, error) {
	streamSelector, err := source.ExpandMap(d.streamSelector, data)
	if err != nil {
		return streamDef{}, err //nolint:exhaustruct
	}

	selector, err := source.ExpandMap(d.selector, data)
	if err != nil {
		return streamDef{}, err //nolint:exhaustruct
	}

	query, err := source.Expand(d.query, data)
	if err != nil {
		return streamDef{}, err //nolint:exhaustruct
	}

	return streamDef{
		streamSelector: streamSelector,
		selector:       selector,
		query:          query,
		fields:         d.fields,
	}, nil
}

// target is the expression identifying the stream, without the bounds of a
// single read.
func (d streamDef) target() string {
	var parts []string

	if s := streamFilter(d.streamSelector); s != "" {
		parts = append(parts, s)
	}

	for _, k := range sortedKeys(d.selector) {
		parts = append(parts, quote(k)+":="+quote(d.selector[k]))
	}

	if d.query != "" {
		parts = append(parts, "("+d.query+")")
	}

	return strings.Join(parts, " ")
}

// logsQL assembles the expression of one read. Only filters that cannot drop a
// matching record are pushed down: the time range, severity and host. User
// input reaches the expression as a quoted value and only after validation —
// severities come from the field map, hosts from the cluster.
func (d streamDef) logsQL(f source.Filter, from, to time.Time) string {
	var parts []string

	if s := streamFilter(d.streamSelector); s != "" {
		parts = append(parts, s)
	}

	parts = append(parts, fmt.Sprintf("_time:[%s, %s]",
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)))

	if len(f.Severities) > 0 {
		values := make([]string, 0, len(f.Severities))
		for _, v := range f.Severities {
			values = append(values, quote(v))
		}

		parts = append(parts, quote(d.fields.Severity)+":in("+strings.Join(values, ",")+")")
	}

	if f.Host != "" {
		parts = append(parts, hostFilter(d.fields, f.Host))
	}

	for _, k := range sortedKeys(d.selector) {
		parts = append(parts, quote(k)+":="+quote(d.selector[k]))
	}

	if d.query != "" {
		parts = append(parts, "("+d.query+")")
	}

	return strings.Join(parts, " ")
}

// hostFilter matches the cluster host name against the host field. In suffix
// mode the store holds an FQDN whose first label is the configured host.
func hostFilter(fm source.FieldMap, host string) string {
	field := quote(fm.Host)
	exact := field + ":=" + quote(host)

	if fm.HostMatch != source.HostMatchSuffix {
		return exact
	}

	return "(" + exact + " OR " + field + ":=" + quote(host+".") + "*)"
}

// streamFilter is the log stream selector, the cheapest filter VictoriaLogs
// has: it picks whole streams before any record is read.
func streamFilter(sel map[string]string) string {
	if len(sel) == 0 {
		return ""
	}

	pairs := make([]string, 0, len(sel))
	for _, k := range sortedKeys(sel) {
		pairs = append(pairs, streamField(k)+"="+quote(sel[k]))
	}

	return "{" + strings.Join(pairs, ",") + "}"
}

// streamField quotes a stream field name only when it is not a bare identifier.
func streamField(name string) string {
	if bareIdent.MatchString(name) {
		return name
	}

	return quote(name)
}

var bareIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// quote renders a value as a LogsQL string literal.
func quote(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// recordCap bounds the slice preallocated for one batch.
const recordCap = 1024

// readRecords decodes the NDJSON answer of a query. VictoriaLogs stores every
// field as a string, so a record is a flat string map.
func readRecords(r io.Reader, limit int) ([]map[string]string, error) {
	dec := json.NewDecoder(r)
	out := make([]map[string]string, 0, min(limit, recordCap))

	for {
		var rec map[string]string

		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			return out, nil
		}

		if err != nil {
			// The answer is streamed, so a failure upstream arrives as a
			// truncated body rather than as an error status.
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("log store closed the response after %d records: %w", len(out), err)
			}

			return nil, fmt.Errorf("decode response: %w", err)
		}

		out = append(out, rec)

		if limit > 0 && len(out) == limit {
			return out, nil
		}
	}
}

// fieldNamesResponse lists the field names present in the queried range.
type fieldNamesResponse struct {
	Values []struct {
		Value string `json:"value"`
	} `json:"values"`
}

// hitsResponse counts records per step; one step covers the whole window.
type hitsResponse struct {
	Hits []struct {
		Values []int `json:"values"`
	} `json:"hits"`
}

// streamsFromConfig resolves every configured stream into a filter and a
// validated field map.
func streamsFromConfig(cfg config.LogSourceConfig) (map[string]streamDef, error) {
	out := make(map[string]streamDef, len(cfg.Streams))

	for name, sc := range cfg.Streams {
		fm, err := source.FieldMapFromConfig(sc.FieldMap)
		if err != nil {
			return nil, fmt.Errorf("streams.%s: %w", name, err)
		}

		def := streamDef{
			streamSelector: sc.StreamSelector,
			selector:       sc.Selector,
			query:          sc.Query,
			fields:         fm,
		}

		if _, err := def.expand(source.TemplateData{Cluster: source.ProbeCluster}); err != nil {
			return nil, fmt.Errorf("streams.%s: %w", name, err)
		}

		out[name] = def
	}

	return out, nil
}
