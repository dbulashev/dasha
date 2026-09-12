package source

import (
	"bytes"
	"fmt"
	"text/template"
)

// ProbeCluster stands in for a real cluster name while templates are expanded
// once at startup, so a template naming an unknown substitution fails there
// rather than on the first search.
const ProbeCluster = "probe"

// TemplateData holds the substitutions allowed in the patterns a source
// resolves per cluster. The host is deliberately absent: a search without a
// host filter and every Check have none, so a host-dependent pattern would
// resolve to nothing instead of failing.
type TemplateData struct {
	Cluster string
}

// Expand resolves one pattern.
func Expand(tmpl string, data TemplateData) (string, error) {
	t, err := template.New("t").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("%w: template %q: %w", ErrConfig, tmpl, err)
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", fmt.Errorf("%w: template %q: %w", ErrConfig, tmpl, err)
	}

	return out.String(), nil
}

// ExpandMap resolves the values of a selector, keeping the keys as configured.
func ExpandMap(m map[string]string, data TemplateData) (map[string]string, error) {
	if len(m) == 0 {
		return nil, nil
	}

	out := make(map[string]string, len(m))

	for k, v := range m {
		value, err := Expand(v, data)
		if err != nil {
			return nil, err
		}

		out[k] = value
	}

	return out, nil
}
