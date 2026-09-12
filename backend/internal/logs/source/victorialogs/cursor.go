package victorialogs

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/internal/logs/source"
)

// maxTokenBytes caps the encoded cursor: it travels as a query parameter.
const maxTokenBytes = 4096

// cursor resumes a read without server-side state: reading continues at the
// timestamp of the last delivered record, skipping the records already handed
// out at exactly that timestamp. A VictoriaLogs record carries no id, so its
// content hash stands in for one.
type cursor struct {
	TS     time.Time `json:"ts"`
	Hashes []string  `json:"h"`
}

func encodeCursor(c cursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(token string) (cursor, error) {
	if token == "" {
		return cursor{}, nil //nolint:exhaustruct
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: malformed page token", source.ErrInvalidToken) //nolint:exhaustruct
	}

	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return cursor{}, fmt.Errorf("%w: malformed page token", source.ErrInvalidToken) //nolint:exhaustruct
	}

	return c, nil
}

// hashRecord identifies a record by its content: every field in canonical
// order, truncated to the first 8 bytes of the digest.
func hashRecord(fields map[string]string) string {
	h := sha256.New()

	for _, k := range sortedKeys(fields) {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(fields[k]))
		_, _ = h.Write([]byte{0})
	}

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:8])
}

// boundary tracks the records already delivered at the timestamp reading
// resumes from. Two identical records sharing a timestamp are two records, so
// the skip list is a multiset: only as many as were delivered are skipped.
type boundary struct {
	ts     time.Time
	hashes []string
	// remaining counts the still unmatched entries of the skip list within the
	// current batch; the list itself is re-read on every request.
	remaining map[string]int
}

func newBoundary(c cursor) boundary {
	return boundary{ts: c.TS, hashes: c.Hashes, remaining: map[string]int{}}
}

func (b *boundary) beginBatch() {
	b.remaining = make(map[string]int, len(b.hashes))
	for _, h := range b.hashes {
		b.remaining[h]++
	}
}

func (b *boundary) seen(ts time.Time, hash string) bool {
	if !ts.Equal(b.ts) || b.remaining[hash] == 0 {
		return false
	}

	b.remaining[hash]--

	return true
}

func (b *boundary) add(ts time.Time, hash string) {
	if !ts.Equal(b.ts) {
		b.ts = ts
		b.hashes = nil
		b.remaining = map[string]int{}
	}

	b.hashes = append(b.hashes, hash)
}
