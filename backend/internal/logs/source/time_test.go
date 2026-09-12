package source

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

	tests := []any{
		"2026-09-05T10:00:00Z",
		"2026-09-05T10:00:00.000Z",
		"2026-09-05 10:00:00+00:00",
		float64(want.UnixMilli()),
	}

	for _, in := range tests {
		got, err := ParseTime(in)
		if err != nil {
			t.Fatalf("ParseTime(%v): %v", in, err)
		}

		if !got.Equal(want) {
			t.Errorf("ParseTime(%v) = %v, want %v", in, got, want)
		}
	}

	if _, err := ParseTime("yesterday"); err == nil {
		t.Error("ParseTime accepted an unparseable value")
	}

	if _, err := ParseTime(nil); err == nil {
		t.Error("ParseTime accepted a missing value")
	}
}
