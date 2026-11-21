package logs

import (
	"slices"
	"testing"

	"github.com/dbulashev/dasha/internal/logs/source"
)

func TestNarrowedBy(t *testing.T) {
	t.Parallel()

	id := int64(7)
	f := source.Filter{
		Severities: []string{"LOG", "ERROR"},
		Host:       "db-1",
		QueryID:    &id,
		Words:      [][]string{{"duration"}, {"checkpoint", "restartpoint"}},
		Substrings: []string{"select", "from t"},
		Database:   "app",
		User:       "bob",
	}

	want := []string{
		"severity=LOG,ERROR", "host", "query_id", "text=duration", "text=checkpoint|restartpoint",
		"text~select", "text~from t", "database", "user",
	}

	if got := narrowedBy(f); !slices.Equal(got, want) {
		t.Errorf("narrowed_by = %v, want %v", got, want)
	}

	if got := narrowedBy(source.Filter{}); got != nil { //nolint:exhaustruct
		t.Errorf("narrowed_by of an empty filter = %v, want nil", got)
	}
}
