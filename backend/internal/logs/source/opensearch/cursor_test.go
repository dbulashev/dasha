package opensearch

import (
	"errors"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/logs/source"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	want := cursor{
		TS:  time.Date(2026, 9, 5, 10, 30, 0, 123000000, time.UTC),
		IDs: []string{"a", "b"},
	}

	token, err := encodeCursor(want)
	if err != nil {
		t.Fatalf("encodeCursor: %v", err)
	}

	got, err := decodeCursor(token)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}

	if !got.TS.Equal(want.TS) || len(got.IDs) != 2 || got.IDs[0] != "a" || got.IDs[1] != "b" {
		t.Errorf("decodeCursor() = %+v, want %+v", got, want)
	}
}

func TestDecodeCursorEmptyAndMalformed(t *testing.T) {
	t.Parallel()

	empty, err := decodeCursor("")
	if err != nil || !empty.TS.IsZero() || empty.IDs != nil {
		t.Errorf("decodeCursor(\"\") = %+v, %v", empty, err)
	}

	if _, err := decodeCursor("not-base64!!"); !errors.Is(err, source.ErrInvalidToken) {
		t.Errorf("decodeCursor(garbage) error = %v, want ErrInvalidToken", err)
	}

	if _, err := decodeCursor("bm90LWpzb24"); !errors.Is(err, source.ErrInvalidToken) {
		t.Errorf("decodeCursor(non-json) error = %v, want ErrInvalidToken", err)
	}
}

func TestBoundarySkipsDeliveredRecords(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	b := newBoundary(cursor{TS: ts, IDs: []string{"1", "2"}})

	if !b.seen(ts, "1") {
		t.Error("cursor id not recognized as already delivered")
	}

	if b.seen(ts.Add(time.Second), "1") {
		t.Error("id at a different timestamp treated as delivered")
	}

	b.add(ts, "3")

	if len(b.ids) != 3 {
		t.Errorf("ids = %v, want three entries", b.ids)
	}

	next := ts.Add(time.Second)
	b.add(next, "4")

	if len(b.ids) != 1 || b.ids[0] != "4" || !b.ts.Equal(next) {
		t.Errorf("boundary not reset on a new timestamp: %v at %v", b.ids, b.ts)
	}

	if b.seen(ts, "1") {
		t.Error("stale id still treated as delivered after the timestamp moved")
	}
}
