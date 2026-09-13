package source

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// timeLayouts are tried in order when the timestamp field is a string.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
}

// ParseTime reads the timestamp field of a record: a date string, or epoch
// milliseconds when the store keeps it as a number.
func ParseTime(v any) (time.Time, error) {
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

		return time.Time{}, fmt.Errorf("%w: timestamp %q has no recognized format", ErrConfig, t)
	case float64:
		return floatMillis(t)
	case json.Number:
		if ms, err := t.Int64(); err == nil {
			return time.UnixMilli(ms).UTC(), nil
		}

		ms, err := t.Float64()
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: timestamp %q is not a number", ErrConfig, t.String())
		}

		return floatMillis(ms)
	default:
		return time.Time{}, fmt.Errorf("%w: record has no usable timestamp", ErrConfig)
	}
}

func floatMillis(ms float64) (time.Time, error) {
	if math.IsNaN(ms) || ms < math.MinInt64 || ms >= math.MaxInt64 {
		return time.Time{}, fmt.Errorf("%w: timestamp %v is out of range", ErrConfig, ms)
	}

	return time.UnixMilli(int64(ms)).UTC(), nil
}
