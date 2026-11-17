// Package piicatalog collects the set of distinct (column name, column comment)
// pairs of every configured database and keeps its change history in storage.
package piicatalog

import (
	"bytes"
	"crypto/sha256"
	"slices"
	"time"
)

type Key struct {
	Cluster  string
	Database string
}

type Pair struct {
	Name string
	// nil: the column has no comment, distinct from an empty one.
	Comment *string
}

// HashedPair carries the pair_hash so it is computed once per collection.
type HashedPair struct {
	Pair
	Hash []byte
}

type Snapshot struct {
	// Sorted by Hash, without duplicates.
	Pairs          []HashedPair
	SetHash        []byte
	SourceInstance string
	ServerVersion  int
	AttemptAt      time.Time
}

type Status struct {
	CurrentVersionID *int64
	SetHash          []byte
	PairCount        int
	LastAttemptAt    *time.Time
	LastSuccessAt    *time.Time
	LastChangedAt    *time.Time
	LastError        *string
	SourceInstance   *string
}

type ApplyResult struct {
	// Zero when the set did not change and no version was created.
	VersionID int64
	Added     int
	Removed   int
}

type RetentionStats struct {
	Pairs           int64
	Versions        int64
	AccessLog       int64
	OrphanDatabases int64
}

// Bump together with the canonical form below; a version-only change creates
// no catalog version (see Diff).
const hashVersion = "v1"

// PairHash = sha256("v1" 0x00 name 0x00 (0x00 | 0x01 comment)).
func PairHash(p Pair) []byte {
	h := sha256.New()
	h.Write([]byte(hashVersion))
	h.Write([]byte{0})
	h.Write([]byte(p.Name))
	h.Write([]byte{0})

	if p.Comment == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write([]byte(*p.Comment))
	}

	return h.Sum(nil)
}

// NewSnapshot hashes, sorts and deduplicates pairs. The set hash is computed
// over sorted pair hashes, so it does not depend on the order or collation of
// the source query.
func NewSnapshot(pairs []Pair, sourceInstance string, serverVersion int, attemptAt time.Time) Snapshot {
	hashed := make([]HashedPair, 0, len(pairs))
	for _, p := range pairs {
		hashed = append(hashed, HashedPair{Pair: p, Hash: PairHash(p)})
	}

	slices.SortFunc(hashed, func(a, b HashedPair) int { return bytes.Compare(a.Hash, b.Hash) })
	hashed = slices.CompactFunc(hashed, func(a, b HashedPair) bool { return bytes.Equal(a.Hash, b.Hash) })

	h := sha256.New()
	h.Write([]byte(hashVersion))
	h.Write([]byte{0})

	for _, p := range hashed {
		h.Write(p.Hash)
	}

	return Snapshot{
		Pairs:          hashed,
		SetHash:        h.Sum(nil),
		SourceInstance: sourceInstance,
		ServerVersion:  serverVersion,
		AttemptAt:      attemptAt,
	}
}

// Diff compares the stored current pair hashes with a snapshot.
func Diff(current [][]byte, snap Snapshot) (added []HashedPair, removed [][]byte) {
	have := make(map[string]struct{}, len(current))
	for _, h := range current {
		have[string(h)] = struct{}{}
	}

	for _, p := range snap.Pairs {
		if _, ok := have[string(p.Hash)]; ok {
			delete(have, string(p.Hash))

			continue
		}

		added = append(added, p)
	}

	for h := range have {
		removed = append(removed, []byte(h))
	}

	slices.SortFunc(removed, bytes.Compare)

	return added, removed
}
