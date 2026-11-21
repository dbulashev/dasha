// Package piicatalog collects the columns of every configured database with
// their schema, table and comment, and keeps the current set in storage.
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

type Column struct {
	Schema string
	Table  string
	Name   string
	// nil: the column has no comment, distinct from an empty one.
	Comment *string
}

// HashedColumn carries the column_hash so it is computed once per collection.
type HashedColumn struct {
	Column
	Hash []byte
}

type Snapshot struct {
	// Sorted by Hash, without duplicates.
	Columns        []HashedColumn
	SetHash        []byte
	SourceInstance string
	ServerVersion  int
	AttemptAt      time.Time
}

type Status struct {
	// nil until the first successful collection.
	SetHash        []byte
	ColumnCount    int
	LastAttemptAt  *time.Time
	LastSuccessAt  *time.Time
	LastChangedAt  *time.Time
	LastError      *string
	SourceInstance *string
}

// Bump together with the canonical form below; a bump rewrites every stored set once.
const hashVersion = "v1"

// ColumnHash = sha256("v1" 0x00 schema 0x00 table 0x00 name 0x00 (0x00 | 0x01 comment)).
// Identifiers cannot contain NUL, so the separators are unambiguous.
func ColumnHash(p Column) []byte {
	h := sha256.New()
	h.Write([]byte(hashVersion))
	h.Write([]byte{0})

	for _, ident := range []string{p.Schema, p.Table, p.Name} {
		h.Write([]byte(ident))
		h.Write([]byte{0})
	}

	if p.Comment == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write([]byte(*p.Comment))
	}

	return h.Sum(nil)
}

// NewSnapshot hashes, sorts and deduplicates columns. The set hash is computed
// over sorted column hashes, so it does not depend on the order or collation of
// the source query.
func NewSnapshot(cols []Column, sourceInstance string, serverVersion int, attemptAt time.Time) Snapshot {
	hashed := make([]HashedColumn, 0, len(cols))
	for _, p := range cols {
		hashed = append(hashed, HashedColumn{Column: p, Hash: ColumnHash(p)})
	}

	slices.SortFunc(hashed, func(a, b HashedColumn) int { return bytes.Compare(a.Hash, b.Hash) })
	hashed = slices.CompactFunc(hashed, func(a, b HashedColumn) bool { return bytes.Equal(a.Hash, b.Hash) })

	h := sha256.New()
	h.Write([]byte(hashVersion))
	h.Write([]byte{0})

	for _, p := range hashed {
		h.Write(p.Hash)
	}

	return Snapshot{
		Columns:        hashed,
		SetHash:        h.Sum(nil),
		SourceInstance: sourceInstance,
		ServerVersion:  serverVersion,
		AttemptAt:      attemptAt,
	}
}
