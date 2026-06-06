package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"bearer", map[string]string{"Authorization": "Bearer dasha_pat_abc"}, "dasha_pat_abc"},
		{"bearer trimmed", map[string]string{"Authorization": "Bearer  dasha_pat_abc "}, "dasha_pat_abc"},
		{"x-api-key", map[string]string{"X-API-Key": "dasha_pat_xyz"}, "dasha_pat_xyz"},
		{"non-bearer authorization ignored", map[string]string{"Authorization": "Basic zzz"}, ""},
		{"none", map[string]string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodPost, "/", nil)
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}

			if got := tokenFromRequest(r); got != tt.want {
				t.Errorf("tokenFromRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTokenCacheKey(t *testing.T) {
	t.Parallel()

	// Deterministic, hashed (64 hex chars), and never the raw secret.
	const secret = "dasha_pat_secret"

	k1 := tokenCacheKey(secret)
	if k1 != tokenCacheKey(secret) {
		t.Errorf("tokenCacheKey is not deterministic")
	}

	if k1 == secret {
		t.Errorf("tokenCacheKey must not return the raw token")
	}

	if len(k1) != 64 {
		t.Errorf("tokenCacheKey length = %d, want 64 (sha256 hex)", len(k1))
	}

	if tokenCacheKey("a") == tokenCacheKey("b") {
		t.Errorf("distinct tokens must not collide")
	}
}
