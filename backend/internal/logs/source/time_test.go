package source

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
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
		json.Number(strconv.FormatInt(want.UnixMilli(), 10)),
		json.Number(strconv.FormatInt(want.UnixMilli(), 10) + ".0"),
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

	for _, in := range []any{
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
		float64(1 << 63),
		json.Number("NaN"),
		json.Number("1e300"),
		json.Number("-1e300"),
	} {
		if _, err := ParseTime(in); !errors.Is(err, ErrConfig) {
			t.Errorf("ParseTime(%v) error = %v, want ErrConfig", in, err)
		}
	}
}
