package piicatalog

import (
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("piicatalog: database has no catalog status")
	ErrNotCollected = errors.New("piicatalog: database has not been collected yet")
)

// Catalog is the current set of one database, ordered by (name, comment).
type Catalog struct {
	SetHash   []byte
	ChangedAt time.Time
	CheckedAt time.Time
	Pairs     []Pair
}
