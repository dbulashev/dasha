package piicatalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

func snapAt(minutes int, pairs ...Pair) Snapshot {
	return NewSnapshot(pairs, "h1", 170000, at(minutes))
}

func TestApplySuccess_First(t *testing.T) {
	t.Parallel()

	snap := snapAt(0, Pair{Name: "id"})

	next, changed, ok := ApplySuccess(Status{}, snap) //nolint:exhaustruct
	require.True(t, ok)
	assert.True(t, changed)
	assert.Equal(t, snap.SetHash, next.SetHash)
	assert.Equal(t, 1, next.PairCount)
	assert.Equal(t, at(0), *next.LastAttemptAt)
	assert.Equal(t, at(0), *next.LastSuccessAt)
	assert.Equal(t, at(0), *next.LastChangedAt)
	assert.Equal(t, "h1", *next.SourceInstance)
	assert.Nil(t, next.LastError)
}

func TestApplySuccess_SameSetKeepsChangedAt(t *testing.T) {
	t.Parallel()

	cur, _, _ := ApplySuccess(Status{}, snapAt(0, Pair{Name: "id"})) //nolint:exhaustruct

	next, changed, ok := ApplySuccess(cur, snapAt(10, Pair{Name: "id"}))
	require.True(t, ok)
	assert.False(t, changed)
	assert.Equal(t, at(0), *next.LastChangedAt)
	assert.Equal(t, at(10), *next.LastSuccessAt)
}

func TestApplySuccess_StaleIsIgnored(t *testing.T) {
	t.Parallel()

	cur, _, _ := ApplySuccess(Status{}, snapAt(10, Pair{Name: "snils"})) //nolint:exhaustruct

	for _, m := range []int{5, 10} {
		_, _, ok := ApplySuccess(cur, snapAt(m, Pair{Name: "id"}))
		assert.False(t, ok, "attempt at +%dm", m)
	}
}

// An older success arriving after a newer failure must not replace a set from
// an even newer success.
func TestApplySuccess_StaleAfterNewerFailure(t *testing.T) {
	t.Parallel()

	cur, _, _ := ApplySuccess(Status{}, snapAt(10, Pair{Name: "snils"})) //nolint:exhaustruct
	cur, _ = ApplyFailure(cur, at(20), "h2: timeout")

	_, _, ok := ApplySuccess(cur, snapAt(5, Pair{Name: "id"}))
	assert.False(t, ok)
}

func TestApplySuccess_OlderThanFailureKeepsError(t *testing.T) {
	t.Parallel()

	cur, _ := ApplyFailure(Status{}, at(10), "h2: timeout") //nolint:exhaustruct

	next, changed, ok := ApplySuccess(cur, snapAt(5, Pair{Name: "id"}))
	require.True(t, ok)
	assert.True(t, changed)
	assert.Equal(t, at(10), *next.LastAttemptAt, "attempt time must not move back")
	assert.Equal(t, "h2: timeout", *next.LastError)
	assert.Equal(t, at(5), *next.LastSuccessAt)
}

func TestApplySuccess_NewerThanFailureClearsError(t *testing.T) {
	t.Parallel()

	cur, _ := ApplyFailure(Status{}, at(5), "h2: timeout") //nolint:exhaustruct

	next, _, ok := ApplySuccess(cur, snapAt(10, Pair{Name: "id"}))
	require.True(t, ok)
	assert.Equal(t, at(10), *next.LastAttemptAt)
	assert.Nil(t, next.LastError)
}

func TestApplyFailure(t *testing.T) {
	t.Parallel()

	cur, _, _ := ApplySuccess(Status{}, snapAt(10, Pair{Name: "id"})) //nolint:exhaustruct

	_, ok := ApplyFailure(cur, at(5), "old")
	assert.False(t, ok, "an older failure is ignored")

	next, ok := ApplyFailure(cur, at(10), "same attempt")
	require.True(t, ok)
	assert.Equal(t, "same attempt", *next.LastError)

	next, ok = ApplyFailure(cur, at(20), "boom")
	require.True(t, ok)
	assert.Equal(t, at(20), *next.LastAttemptAt)
	assert.Equal(t, "boom", *next.LastError)
	assert.Equal(t, cur.SetHash, next.SetHash, "a failure keeps the set")
	assert.Equal(t, at(10), *next.LastSuccessAt)
}
