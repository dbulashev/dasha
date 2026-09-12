package victorialogs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dbulashev/dasha/internal/config"
	"github.com/dbulashev/dasha/internal/logs/source"
)

var cursorTS = time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

func testFieldMap(t *testing.T) source.FieldMap {
	t.Helper()

	fm, err := source.FieldMapFromConfig(config.LogFieldMapConfig{ //nolint:exhaustruct
		Preset:    source.PresetJSONLog,
		Timestamp: "_time",
		Text:      "_msg",
		Host:      "host",
	})
	if err != nil {
		t.Fatalf("field map: %v", err)
	}

	return fm
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	want := cursor{TS: cursorTS, Hashes: []string{"aaaa", "bbbb", "aaaa"}}

	token, err := encodeCursor(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, err := decodeCursor(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !got.TS.Equal(want.TS) || strings.Join(got.Hashes, ",") != strings.Join(want.Hashes, ",") {
		t.Errorf("cursor round trip = %+v, want %+v", got, want)
	}
}

func TestDecodeCursorEmptyAndMalformed(t *testing.T) {
	t.Parallel()

	if c, err := decodeCursor(""); err != nil || !c.TS.IsZero() {
		t.Errorf("empty token = %+v, %v", c, err)
	}

	for _, token := range []string{"!!", "YWJj"} {
		if _, err := decodeCursor(token); !errors.Is(err, source.ErrInvalidToken) {
			t.Errorf("decodeCursor(%q) error = %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestHashRecordIgnoresFieldOrderAndSeparatesContent(t *testing.T) {
	t.Parallel()

	a := hashRecord(map[string]string{"_msg": "a", "host": "db-1"})
	b := hashRecord(map[string]string{"host": "db-1", "_msg": "a"})

	if a != b {
		t.Errorf("hash depends on field order: %s vs %s", a, b)
	}

	// Without a separator "ab"+"c" and "a"+"bc" would collide.
	if hashRecord(map[string]string{"ab": "c"}) == hashRecord(map[string]string{"a": "bc"}) {
		t.Error("field boundaries do not enter the hash")
	}
}

// TestHashRecordSeparatesContentHoldingTheSeparator: a log line may carry any
// byte, so field boundaries cannot rest on one.
func TestHashRecordSeparatesContentHoldingTheSeparator(t *testing.T) {
	t.Parallel()

	pairs := [][2]map[string]string{
		{
			{"a": "x", "b": "y"},
			{"a": "x\x00b\x00y"},
		},
		{
			{"_msg": "a:2:bc"},
			{"_msg": "a", "2": "bc"},
		},
	}

	for _, p := range pairs {
		if hashRecord(p[0]) == hashRecord(p[1]) {
			t.Errorf("%v and %v share a hash", p[0], p[1])
		}
	}
}

// TestEmitDistinguishesRecordsWhoseFieldsRunTogether: a colliding hash at the
// boundary timestamp drops a record from the next page.
func TestEmitDistinguishesRecordsWhoseFieldsRunTogether(t *testing.T) {
	t.Parallel()

	ts := cursorTS.Format(time.RFC3339Nano)
	first := map[string]string{"_time": ts, "a": "x", "b": "y"}
	second := map[string]string{"_time": ts, "a": "x\x00b\x00y"}

	b := newBoundary(cursor{}) //nolint:exhaustruct

	delivered, _, err := emitAll(t, []map[string]string{first}, &b, 100)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	resume, err := decodeCursor(delivered[0].Token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	rb := newBoundary(resume)

	got, _, err := emitAll(t, []map[string]string{first, second}, &rb, 100)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	if len(got) != 1 || got[0].Fields["a"] != second["a"] {
		t.Fatalf("resumed read delivered %d records, want only the second", len(got))
	}
}

func TestResumeAtStaysWithinTheRequestedRange(t *testing.T) {
	t.Parallel()

	to := cursorTS

	cases := []struct {
		name string
		c    cursor
		want time.Time
	}{
		{"no token", cursor{TS: time.Time{}, Hashes: nil}, to},
		{"inside the range", cursor{TS: to.Add(-time.Minute), Hashes: nil}, to.Add(-time.Minute)},
		{"at the bound", cursor{TS: to, Hashes: nil}, to},
		{"from a range that reached further", cursor{TS: to.Add(time.Hour), Hashes: nil}, to},
	}

	for _, tc := range cases {
		if got := resumeAt(tc.c, to); !got.Equal(tc.want) {
			t.Errorf("%s: resumeAt = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func record(ts time.Time, msg string) map[string]string {
	return map[string]string{"_time": ts.Format(time.RFC3339Nano), "_msg": msg, "host": "db-1"}
}

func emitAll(t *testing.T, records []map[string]string, b *boundary, maxHashes int) ([]source.Record, emitResult, error) {
	t.Helper()

	var out []source.Record

	res, err := emit(records, testFieldMap(t), b, maxHashes, func(r source.Record) bool {
		out = append(out, r)

		return true
	})

	return out, res, err
}

// TestEmitOrdersNewestFirst: the order of an answer is not guaranteed, so the
// batch is ordered in the provider.
func TestEmitOrdersNewestFirst(t *testing.T) {
	t.Parallel()

	records := []map[string]string{
		record(cursorTS.Add(time.Second), "middle"),
		record(cursorTS.Add(2*time.Second), "newest"),
		record(cursorTS, "oldest"),
		{"_msg": "no timestamp"},
	}

	b := newBoundary(cursor{}) //nolint:exhaustruct

	got, res, err := emitAll(t, records, &b, 100)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	if res.skipped != 1 {
		t.Errorf("skipped = %d, want 1 record without a timestamp", res.skipped)
	}

	want := []string{"newest", "middle", "oldest"}
	for i, w := range want {
		if got[i].Fields["_msg"] != w {
			t.Fatalf("record %d = %q, want %q", i, got[i].Fields["_msg"], w)
		}
	}
}

// TestEmitSkipsOnlyAsManyDuplicatesAsWereDelivered: two identical records at
// one timestamp are two records, so the skip list is a multiset.
func TestEmitSkipsOnlyAsManyDuplicatesAsWereDelivered(t *testing.T) {
	t.Parallel()

	records := []map[string]string{
		record(cursorTS, "same"),
		record(cursorTS, "same"),
		record(cursorTS, "other"),
	}

	b := newBoundary(cursor{}) //nolint:exhaustruct

	first, _, err := emitAll(t, records[:1], &b, 100)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	resume, err := decodeCursor(first[0].Token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	rb := newBoundary(resume)

	got, _, err := emitAll(t, records, &rb, 100)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("resumed read delivered %d records, want the duplicate and the third", len(got))
	}
}

// TestEmitDropsTheSkipListOnANewTimestamp: the list only ever covers the
// records of the boundary timestamp.
func TestEmitDropsTheSkipListOnANewTimestamp(t *testing.T) {
	t.Parallel()

	b := newBoundary(cursor{TS: cursorTS, Hashes: []string{hashRecord(record(cursorTS, "old"))}})

	_, _, err := emitAll(t, []map[string]string{record(cursorTS.Add(-time.Second), "older")}, &b, 100)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	if len(b.hashes) != 1 || !b.ts.Equal(cursorTS.Add(-time.Second)) {
		t.Errorf("boundary after a new timestamp = %v %v", b.ts, b.hashes)
	}
}

func TestEmitStopsAtMaxBoundaryHashes(t *testing.T) {
	t.Parallel()

	records := []map[string]string{
		record(cursorTS, "one"),
		record(cursorTS, "two"),
		record(cursorTS, "three"),
	}

	b := newBoundary(cursor{}) //nolint:exhaustruct

	_, _, err := emitAll(t, records, &b, 2)
	if !errors.Is(err, source.ErrPartial) {
		t.Fatalf("emit error = %v, want ErrPartial", err)
	}

	if !strings.Contains(err.Error(), "precision") {
		t.Errorf("error does not name the cause: %v", err)
	}
}

// TestEmitStopsWhenTheTokenOutgrowsTheQueryParameter: the cursor travels as a
// query parameter, so its size bounds a read as much as the hash count does.
func TestEmitStopsWhenTheTokenOutgrowsTheQueryParameter(t *testing.T) {
	t.Parallel()

	records := make([]map[string]string, 0, 400)
	for i := range 400 {
		records = append(records, record(cursorTS, strings.Repeat("x", i)))
	}

	b := newBoundary(cursor{}) //nolint:exhaustruct

	got, _, err := emitAll(t, records, &b, 100000)
	if !errors.Is(err, source.ErrPartial) {
		t.Fatalf("emit error = %v, want ErrPartial", err)
	}

	if len(got) == 0 {
		t.Error("no records delivered before the token grew too large")
	}
}
