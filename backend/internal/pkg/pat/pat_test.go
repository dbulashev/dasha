package pat

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	secret, hash, display, err := Generate()
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	if !strings.HasPrefix(secret, Prefix) {
		t.Errorf("secret %q missing prefix %q", secret, Prefix)
	}

	if !strings.HasPrefix(display, Prefix) {
		t.Errorf("display %q missing prefix %q", display, Prefix)
	}

	if len(display) != len(Prefix)+displayChars {
		t.Errorf("display length = %d, want %d", len(display), len(Prefix)+displayChars)
	}

	if len(hash) != sha256.Size {
		t.Errorf("hash length = %d, want %d", len(hash), sha256.Size)
	}

	// The returned hash must be the hash of the returned secret (lookup invariant).
	want := Hash(secret)
	if string(hash) != string(want) {
		t.Errorf("returned hash does not match Hash(secret)")
	}

	// The display prefix must be a non-secret leading slice of the secret.
	if !strings.HasPrefix(secret, display) {
		t.Errorf("display %q is not a prefix of secret %q", display, secret)
	}
}

func TestGenerateUnique(t *testing.T) {
	t.Parallel()

	const n = 100

	seen := make(map[string]struct{}, n)
	for range n {
		secret, _, _, err := Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}

		if _, dup := seen[secret]; dup {
			t.Fatalf("duplicate secret generated: %q", secret)
		}

		seen[secret] = struct{}{}
	}
}

func TestHashDeterministic(t *testing.T) {
	t.Parallel()

	const secret = Prefix + "abc123"

	if string(Hash(secret)) != string(Hash(secret)) {
		t.Errorf("Hash is not deterministic")
	}

	if string(Hash(secret)) == string(Hash(secret+"x")) {
		t.Errorf("Hash collision for distinct inputs")
	}
}
