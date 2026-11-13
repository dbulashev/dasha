package config

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	for input, want := range map[string]string{"": "", "/": "", "/dasha": "/dasha", "/dasha/": "/dasha", "/tools/dasha-1.2": "/tools/dasha-1.2"} {
		got, err := NormalizeBasePath(input)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"//", "dasha", "//host", "/a//b", "/a/../b", "/./a", "/a%2fb", "/a?b", "/a#b", "/a\\b", "/a;\nreturn 200;"} {
		if _, err := NormalizeBasePath(input); err == nil {
			t.Errorf("Принят небезопасный префикс %q", input)
		}
	}
}
