package explain

import (
	"os"
	"path/filepath"
	"testing"
)

// versions are the PostgreSQL releases the probe corpus covers.
var versions = []string{"pg14", "pg15", "pg16", "pg17", "pg18"}

func fixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return string(body)
}

func parseFixture(t *testing.T, name string, src Source) Plan {
	t.Helper()

	p, err := Parse(fixture(t, name), src)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	return p
}

func findNode(p *Plan, nodeType string) *Node {
	var found *Node

	p.Walk(func(_ []int, n *Node) bool {
		if found == nil && n.Type == nodeType {
			found = n
		}

		return true
	})

	return found
}

func codes(findings []Finding) map[string]Finding {
	out := make(map[string]Finding, len(findings))
	for _, f := range findings {
		if _, ok := out[f.Code]; !ok {
			out[f.Code] = f
		}
	}

	return out
}
