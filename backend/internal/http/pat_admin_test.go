package http

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/pat"
)

type fakeTokenAdminStore struct{}

func (fakeTokenAdminStore) ListAllAPITokens(context.Context, bool) ([]pat.AdminToken, error) {
	return []pat.AdminToken{{Subject: "dev@corp"}}, nil
}

func (fakeTokenAdminStore) RevokeAPITokenByID(_ context.Context, id string) (bool, error) {
	return id == "live", nil
}

func (fakeTokenAdminStore) ListUsers(context.Context) ([]pat.User, error) {
	return []pat.User{{Subject: "dev@corp"}}, nil
}

func TestPATAdminMapping(t *testing.T) {
	t.Parallel()

	h := &Handlers{tokenAdmin: pat.NewAdmin(fakeTokenAdminStore{})} //nolint:exhaustruct
	adminCtx := userCtx(&auth.UserContext{AuthMethod: auth.MethodOIDC, Email: "dba@corp", Role: "admin"})
	patCtx := userCtx(&auth.UserContext{AuthMethod: auth.MethodPAT, Role: "admin"})

	users, err := h.ListUsers(adminCtx, serverhttp.ListUsersRequestObject{})
	require.NoError(t, err)
	assert.Len(t, users, 1)

	tokens, err := h.ListAllPersonalTokens(adminCtx, serverhttp.ListAllPersonalTokensRequestObject{}) //nolint:exhaustruct
	require.NoError(t, err)
	assert.Len(t, tokens, 1)

	revoked, err := h.RevokeAnyPersonalToken(adminCtx, serverhttp.RevokeAnyPersonalTokenRequestObject{Id: "live"})
	require.NoError(t, err)
	assert.IsType(t, serverhttp.RevokeAnyPersonalToken204Response{}, revoked)

	missing, err := h.RevokeAnyPersonalToken(adminCtx, serverhttp.RevokeAnyPersonalTokenRequestObject{Id: "gone"})
	require.NoError(t, err)
	assert.IsType(t, serverhttp.RevokeAnyPersonalToken404Response{}, missing)

	forbidden, err := h.ListUsers(patCtx, serverhttp.ListUsersRequestObject{})
	require.NoError(t, err)
	assert.IsType(t, serverhttp.ListUsers403Response{}, forbidden)

	noStorage := &Handlers{tokenAdmin: pat.NewAdmin(nil)} //nolint:exhaustruct

	resp, err := noStorage.ListAllPersonalTokens(adminCtx, serverhttp.ListAllPersonalTokensRequestObject{}) //nolint:exhaustruct
	require.NoError(t, err)
	assert.IsType(t, serverhttp.ListAllPersonalTokens403Response{}, resp)
}
