package piicatalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
