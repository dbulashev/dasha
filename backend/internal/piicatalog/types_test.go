package piicatalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(s string) *string { return &s }

func TestPairHash_NullVsEmptyComment(t *testing.T) {
	assert.NotEqual(t, PairHash(Pair{Name: "snils"}), PairHash(Pair{Name: "snils", Comment: ptr("")}))
}

func TestPairHash_NoBoundaryAmbiguity(t *testing.T) {
	assert.NotEqual(t,
		PairHash(Pair{Name: "a", Comment: ptr("b")}),
		PairHash(Pair{Name: "a\x00\x01b"}))
}

func TestNewSnapshot_OrderIndependentAndDeduplicated(t *testing.T) {
	at := time.Now()
	a := NewSnapshot([]Pair{{Name: "fio", Comment: ptr("ФИО")}, {Name: "id"}}, "h", 170000, at)
	b := NewSnapshot([]Pair{{Name: "id"}, {Name: "fio", Comment: ptr("ФИО")}, {Name: "id"}}, "h", 170000, at)

	assert.Equal(t, a.SetHash, b.SetHash)
	assert.Len(t, b.Pairs, 2)
}

func TestNewSnapshot_EmptySetHasStableHash(t *testing.T) {
	a := NewSnapshot(nil, "h", 0, time.Now())
	b := NewSnapshot([]Pair{}, "h", 0, time.Now())

	assert.Equal(t, a.SetHash, b.SetHash)
	assert.NotEqual(t, a.SetHash, NewSnapshot([]Pair{{Name: "id"}}, "h", 0, time.Now()).SetHash)
}

func TestDiff(t *testing.T) {
	old := NewSnapshot([]Pair{{Name: "id"}, {Name: "snils"}}, "h", 0, time.Now())
	cur := make([][]byte, 0, len(old.Pairs))

	for _, p := range old.Pairs {
		cur = append(cur, p.Hash)
	}

	snap := NewSnapshot([]Pair{{Name: "id"}, {Name: "snils", Comment: ptr("СНИЛС")}}, "h", 0, time.Now())

	added, removed := Diff(cur, snap)
	require.Len(t, added, 1)
	assert.Equal(t, "snils", added[0].Name)
	assert.Equal(t, "СНИЛС", *added[0].Comment)
	require.Len(t, removed, 1)
	assert.Equal(t, PairHash(Pair{Name: "snils"}), removed[0])

	added, removed = Diff(cur, old)
	assert.Empty(t, added)
	assert.Empty(t, removed)
}

func TestTruncate_KeepsValidUTF8(t *testing.T) {
	s := truncate("абв", 3)
	assert.Equal(t, "а", s)
	assert.Equal(t, "abc", truncate("abc", 5))
}

func TestTruncate_ReplacesInvalidUTF8(t *testing.T) {
	cp1251 := "FATAL: \xcf\xee\xeb\xfc\xe7\xee\xe2\xe0\xf2\xe5\xeb\xfc"
	assert.Equal(t, "FATAL: ?", truncate(cp1251, 100))
	assert.Equal(t, "a?b", truncate("a\xffb", 2+len("?")))
}
