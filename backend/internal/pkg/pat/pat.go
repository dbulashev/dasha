// Package pat generates and hashes personal access tokens. The full secret is
// shown to the user once at creation; only its SHA-256 is stored, and lookups
// match on that hash (the secret is high-entropy, so the hash is the index key
// and no per-row constant-time compare is needed).
package pat

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// Prefix tags every personal access token so leaked secrets are greppable by
// secret scanners and recognisable in incident triage.
const Prefix = "dasha_pat_"

const (
	bodyBytes    = 32 // random entropy of the secret body
	displayChars = 6  // chars of the body kept as a non-secret display prefix
)

// Generate returns a new secret (Prefix + base64url body), its SHA-256 hash for
// storage, and a short non-secret display prefix (Prefix + first chars).
func Generate() (secret string, hash []byte, display string, err error) {
	b := make([]byte, bodyBytes)
	if _, e := rand.Read(b); e != nil {
		return "", nil, "", fmt.Errorf("pat: random: %w", e)
	}

	body := base64.RawURLEncoding.EncodeToString(b)
	secret = Prefix + body

	n := displayChars
	if len(body) < n {
		n = len(body)
	}

	return secret, Hash(secret), Prefix + body[:n], nil
}

// Hash returns the SHA-256 of a presented secret, for storage and lookup.
func Hash(secret string) []byte {
	h := sha256.Sum256([]byte(secret))

	return h[:]
}
