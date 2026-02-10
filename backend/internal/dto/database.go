package dto

import "time"

type StatsResetTime struct {
	Time time.Time
}

type DatabaseSize struct {
	SizeBytes  int64
	SizePretty string
}
