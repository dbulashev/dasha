package opensearch

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dbulashev/dasha/internal/logs/source"
)

// cursor resumes a read without server-side state: reading continues at the
// timestamp of the last delivered record, skipping the records already handed
// out at exactly that timestamp.
type cursor struct {
	TS  time.Time `json:"ts"`
	IDs []string  `json:"ids"`
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
