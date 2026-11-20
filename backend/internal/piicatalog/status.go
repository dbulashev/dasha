package piicatalog

import (
	"bytes"
	"time"
)

// Status transitions. Storage locks the row, passes the current status here and
// writes back what is returned, so the rules hold under concurrent writers
// (several daemons without leader election).
//
// Every timestamp is an attempt time of the collecting instance: due-ness,
// ordering and the API all read the same clock. Without leader election the
// instances' clocks are compared, so skew decides between near-simultaneous runs.

// ApplySuccess returns the status after a successful collection and whether
// the stored set must be replaced. ok is false when a set from a newer attempt
// is already stored; a failure, older or newer, never blocks a success.
func ApplySuccess(cur Status, snap Snapshot) (next Status, changed, ok bool) {
	at := snap.AttemptAt

	if cur.LastSuccessAt != nil && !at.After(*cur.LastSuccessAt) {
		return cur, false, false
	}

	changed = !bytes.Equal(cur.SetHash, snap.SetHash)

	next = cur
	next.SetHash = snap.SetHash
	next.ColumnCount = len(snap.Columns)
	next.LastSuccessAt = &at
	next.SourceInstance = &snap.SourceInstance

	if changed {
		next.LastChangedAt = &at
	}

	// A newer failure stays visible: the last attempt did fail.
	if cur.LastAttemptAt == nil || !at.Before(*cur.LastAttemptAt) {
		next.LastAttemptAt = &at
		next.LastError = nil
	}

	return next, changed, true
}

// ApplyFailure returns the status after a failed collection; ok is false when
// a newer attempt is already recorded. The stored set is kept.
func ApplyFailure(cur Status, attemptAt time.Time, msg string) (next Status, ok bool) {
	if cur.LastAttemptAt != nil && attemptAt.Before(*cur.LastAttemptAt) {
		return cur, false
	}

	next = cur
	next.LastAttemptAt = &attemptAt
	next.LastError = &msg

	return next, true
}
