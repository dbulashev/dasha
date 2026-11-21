package pat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAdminStore struct {
	revoked []string
}

func (*fakeAdminStore) ListAllAPITokens(context.Context, bool) ([]AdminToken, error) {
	return []AdminToken{{Subject: "dev@corp"}}, nil
}

func (f *fakeAdminStore) RevokeAPITokenByID(_ context.Context, id string) (bool, error) {
	f.revoked = append(f.revoked, id)

	return true, nil
}

func (*fakeAdminStore) ListUsers(context.Context) ([]User, error) {
	return []User{{Subject: "dev@corp"}}, nil
}

func TestAdminAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		caller Caller
		want   bool
	}{
		{"oidc admin", admin, true},
		{"oidc admin without email", Caller{OIDC: true, Role: "admin"}, true},
		{"oidc viewer", Caller{OIDC: true, Email: "dev@corp", Role: "viewer"}, false},
		{"admin pat or static token", Caller{Role: "admin"}, false},
		{"unknown role", Caller{OIDC: true, Email: "x@corp", Role: "superuser"}, false},
		{"anonymous", Caller{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := &fakeAdminStore{}
			a := NewAdmin(st)

			_, errTokens := a.Tokens(t.Context(), tt.caller, false)
			_, errRevoke := a.Revoke(t.Context(), tt.caller, "id")
			_, errUsers := a.Users(t.Context(), tt.caller)

			if tt.want {
				require.NoError(t, errTokens)
				require.NoError(t, errRevoke)
				require.NoError(t, errUsers)
				assert.Equal(t, []string{"id"}, st.revoked)

				return
			}

			require.ErrorIs(t, errTokens, ErrForbidden)
			require.ErrorIs(t, errRevoke, ErrForbidden)
			require.ErrorIs(t, errUsers, ErrForbidden)
			assert.Empty(t, st.revoked)
		})
	}
}

func TestAdminWithoutStorage(t *testing.T) {
	t.Parallel()

	_, err := NewAdmin(nil).Users(t.Context(), admin)
	require.ErrorIs(t, err, ErrForbidden)
}
