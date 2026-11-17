package sanitize

import "testing"

func TestTruncateUTF8(t *testing.T) {
	cp1251 := "FATAL: \xcf\xee\xeb\xfc\xe7\xee\xe2\xe0\xf2\xe5\xeb\xfc"

	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"абв", 3, "а"},
		{"ааа", 5, "аа"},
		{"abc", 5, "abc"},
		{cp1251, 100, "FATAL: ?"},
		{"a\xffb", 3, "a?b"},
	} {
		if got := TruncateUTF8(tc.in, tc.n); got != tc.want {
			t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
