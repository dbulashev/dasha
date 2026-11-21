package config

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	for input, want := range map[string]string{
		"": "", "/": "", "/dasha": "/dasha", "/dasha/": "/dasha", "/tools/dasha-1.2": "/tools/dasha-1.2",
		"/apiary": "/apiary", "/authentication": "/authentication", "/mcp-tools": "/mcp-tools", "/tools/api": "/tools/api",
	} {
		got, err := NormalizeBasePath(input)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{
		"//", "dasha", "//host", "/a//b", "/a/../b", "/./a", "/a%2fb", "/a?b", "/a#b", "/a\\b", "/a;\nreturn 200;",
		"/api", "/api/", "/auth", "/auth/", "/mcp", "/mcp/", "/api/auth", "/api/autosnapshot/clusters", "/auth/tools", "/mcp/tools",
	} {
		if _, err := NormalizeBasePath(input); err == nil {
			t.Errorf("accepted an invalid prefix %q", input)
		}
	}
}
