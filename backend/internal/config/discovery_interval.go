package config

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultDiscoveryInterval = 5 * time.Minute
	MinDiscoveryInterval     = time.Minute
)

var errBadMinutes = errors.New("refresh interval out of range")

var durationType = reflect.TypeFor[time.Duration]()

// minutesHook decodes a bare number into a time.Duration as minutes; a string
// with a unit ("5m", "90s") is left to StringToTimeDurationHookFunc.
func minutesHook(from, to reflect.Type, data any) (any, error) {
	if to != durationType || from == durationType {
		return data, nil
	}

	var minutes float64

	switch v := reflect.ValueOf(data); v.Kind() { //nolint:exhaustive
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		minutes = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		minutes = float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		minutes = v.Float()
	case reflect.String:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.String()), 64)
		if err != nil {
			return data, nil //nolint:nilerr // not a bare number: a duration string
		}

		minutes = f
	default:
		return data, nil
	}

	ns := minutes * float64(time.Minute)
	if math.IsNaN(ns) || math.Abs(ns) >= math.MaxInt64 {
		return nil, fmt.Errorf("%w: %v minutes", errBadMinutes, data)
	}

	return time.Duration(ns), nil
}

// DiscoveryInterval applies the default to a non-positive refresh interval and
// raises one below the minimum; raised reports the latter, for a warning.
func DiscoveryInterval(d time.Duration) (interval time.Duration, raised bool) {
	switch {
	case d <= 0:
		return DefaultDiscoveryInterval, false
	case d < MinDiscoveryInterval:
		return MinDiscoveryInterval, true
	default:
		return d, false
	}
}
