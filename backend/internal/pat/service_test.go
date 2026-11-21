package pat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	patsecret "github.com/dbulashev/dasha/internal/pkg/pat"
)

type created struct {
	hash                        []byte
	prefix, name, subject, role string
	expiresAt                   *time.Time
}

type fakeStore struct {
	created  []created
	listed   []string
	revoked  []string
	tokens   []Token
	revokeOK bool
	err      error
}

func (f *fakeStore) CreateAPIToken(_ context.Context, hash []byte, prefix, name, subject, role string, expiresAt *time.Time) (string, error) {
	f.created = append(f.created, created{hash, prefix, name, subject, role, expiresAt})

	return "id-1", f.err
}

func (f *fakeStore) ListAPITokens(_ context.Context, subject string, _ bool) ([]Token, error) {
	f.listed = append(f.listed, subject)

	return f.tokens, f.err
}

func (f *fakeStore) RevokeAPIToken(_ context.Context, subject, id string) (bool, error) {
	f.revoked = append(f.revoked, subject+"/"+id)

	return f.revokeOK, f.err
}

var (
	t0     = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	admin  = Caller{OIDC: true, Email: "dba@corp", Role: "admin"}
	viewer = Caller{OIDC: true, Email: "dev@corp", Role: "viewer"}
)

func newTestService(st Store, minRole string) *Service {
	s := NewService(st, minRole)
	s.now = func() time.Time { return t0 }

	return s
}

func TestCallerSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		caller Caller
		want   string
		wantOK bool
	}{
		{"oidc with email", Caller{OIDC: true, Email: "dba@corp"}, "dba@corp", true},
		{"oidc without email rejected", Caller{OIDC: true}, "", false},
		{"static token or pat rejected", Caller{Email: "dba@corp"}, "", false},
		{"empty rejected", Caller{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tt.caller.subject()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("subject(%+v) = (%q, %v), want (%q, %v)", tt.caller, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRoleAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		caller    string
		requested string
		want      bool
	}{
		{"admin mints admin", "admin", "admin", true},
		{"admin mints viewer", "admin", "viewer", true},
		{"viewer mints viewer", "viewer", "viewer", true},
		{"viewer cannot mint admin", "viewer", "admin", false},
		{"unknown requested role rejected", "admin", "superuser", false},
		{"empty requested role rejected", "admin", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := roleAllowed(tt.caller, tt.requested); got != tt.want {
				t.Errorf("roleAllowed(%q, %q) = %v, want %v", tt.caller, tt.requested, got, tt.want)
			}
		})
	}
}

func TestExpiryDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		role      string
		requested int
		want      int
	}{
		{"viewer keeps no expiry", "viewer", 0, 0},
		{"viewer keeps a long expiry", "viewer", 3650, 3650},
		{"viewer keeps a short expiry", "viewer", 7, 7},

		// The "no expiry" case is the one that matters: an unbounded admin token
		// would otherwise be minted by simply omitting expires_in_days.
		{"admin no-expiry is capped", "admin", 0, maxAdminExpiresInDays},
		{"admin over-cap is capped", "admin", 3650, maxAdminExpiresInDays},
		{"admin at the cap is kept", "admin", 30, 30},
		{"admin under the cap is kept", "admin", 7, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := expiryDays(tt.role, tt.requested); got != tt.want {
				t.Errorf("expiryDays(%q, %d) = %d, want %d", tt.role, tt.requested, got, tt.want)
			}
		})
	}
}

func TestMintAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		minRole string
		caller  string
		want    bool
	}{
		{"admin gate passes admin", "admin", "admin", true},
		{"admin gate blocks viewer", "admin", "viewer", false},
		{"viewer gate passes viewer", "viewer", "viewer", true},
		{"viewer gate passes admin", "viewer", "admin", true},
		{"unnormalized config fails closed for viewer", "", "viewer", false},
		{"unnormalized config still passes admin", "", "admin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := mintAllowed(tt.minRole, tt.caller); got != tt.want {
				t.Errorf("mintAllowed(%q, %q) = %v, want %v", tt.minRole, tt.caller, got, tt.want)
			}
		})
	}
}

func TestMintRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		minRole string
		caller  Caller
		req     MintRequest
		want    error
	}{
		{"no subject", "viewer", Caller{Email: "dba@corp", Role: "admin"}, MintRequest{Name: "ci"}, ErrForbidden},
		{"below min role", "admin", viewer, MintRequest{Name: "ci"}, ErrForbidden},
		{"forbidden before invalid", "admin", viewer, MintRequest{}, ErrForbidden},
		{"empty name", "viewer", viewer, MintRequest{}, ErrInvalid},
		{"viewer asks for admin", "viewer", viewer, MintRequest{Name: "ci", Role: "admin"}, ErrForbidden},
		{"unknown role", "viewer", admin, MintRequest{Name: "ci", Role: "root"}, ErrForbidden},
		{"negative expiry", "viewer", viewer, MintRequest{Name: "ci", ExpiresInDays: -1}, ErrInvalid},
		{"expiry over max", "viewer", viewer, MintRequest{Name: "ci", ExpiresInDays: maxExpiresInDays + 1}, ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := &fakeStore{}

			_, err := newTestService(st, tt.minRole).Mint(t.Context(), tt.caller, tt.req)
			require.ErrorIs(t, err, tt.want)
			assert.Empty(t, st.created)
		})
	}
}

func TestMintViewerDefaults(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}

	m, err := newTestService(st, "viewer").Mint(t.Context(), viewer, MintRequest{Name: "ci"})
	require.NoError(t, err)

	require.Len(t, st.created, 1)
	c := st.created[0]
	assert.Equal(t, "dev@corp", c.subject)
	assert.Equal(t, "viewer", c.role)
	assert.Nil(t, c.expiresAt, "a viewer may ask for no expiry")
	assert.Equal(t, patsecret.Hash(m.Secret), c.hash, "only the hash of the returned secret is stored")
	assert.Equal(t, c.prefix, m.Prefix)

	assert.Equal(t, "id-1", m.ID)
	assert.Equal(t, "viewer", m.Role)
	assert.Equal(t, t0, m.CreatedAt)
}

func TestMintAdminExpiryIsCapped(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}

	m, err := newTestService(st, "admin").Mint(t.Context(), admin, MintRequest{Name: "ci", Role: "admin"})
	require.NoError(t, err)

	want := t0.Add(maxAdminExpiresInDays * 24 * time.Hour)
	require.NotNil(t, m.ExpiresAt)
	assert.Equal(t, want, *m.ExpiresAt)
	assert.Equal(t, want, *st.created[0].expiresAt)
}

func TestMintWithoutStore(t *testing.T) {
	t.Parallel()

	s := newTestService(nil, "viewer")

	_, err := s.Mint(t.Context(), viewer, MintRequest{Name: "ci"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrForbidden) || errors.Is(err, ErrInvalid))

	_, err = s.Mint(t.Context(), viewer, MintRequest{})
	require.ErrorIs(t, err, ErrInvalid, "request checks still come first")

	toks, err := s.List(t.Context(), viewer, false)
	require.NoError(t, err)
	assert.Empty(t, toks)

	ok, err := s.Revoke(t.Context(), viewer, "id-1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestListAndRevokeNeedSubject(t *testing.T) {
	t.Parallel()

	st := &fakeStore{tokens: []Token{{ID: "id-1"}}, revokeOK: true}
	s := newTestService(st, "admin")

	toks, err := s.List(t.Context(), Caller{Role: "admin"}, true)
	require.NoError(t, err)
	assert.Empty(t, toks)

	ok, err := s.Revoke(t.Context(), Caller{Role: "admin"}, "id-1")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, st.listed)
	assert.Empty(t, st.revoked)

	// Listing and revoking own tokens is not gated by pat_min_role.
	toks, err = s.List(t.Context(), viewer, false)
	require.NoError(t, err)
	assert.Len(t, toks, 1)

	ok, err = s.Revoke(t.Context(), viewer, "id-1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"dev@corp"}, st.listed)
	assert.Equal(t, []string{"dev@corp/id-1"}, st.revoked)
}
