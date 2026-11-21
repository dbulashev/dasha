package http

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/pat"
)

type fakePATStore struct {
	subject  string
	revokeOK bool
}

func (f *fakePATStore) CreateAPIToken(_ context.Context, _ []byte, _, _, subject, _ string, _ *time.Time) (string, error) {
	f.subject = subject

	return "id-1", nil
}

func (f *fakePATStore) ListAPITokens(_ context.Context, subject string, _ bool) ([]pat.Token, error) {
	f.subject = subject

	return []pat.Token{{ID: "id-1", Name: "ci", Role: "viewer"}}, nil
}

func (f *fakePATStore) RevokeAPIToken(_ context.Context, subject, _ string) (bool, error) {
	f.subject = subject

	return f.revokeOK, nil
}

func (*fakePATStore) APITokensReady(context.Context) bool { return true }

func userCtx(u *auth.UserContext) context.Context {
	c := echo.New().NewContext(httptest.NewRequest("GET", "/", nil), httptest.NewRecorder())
	auth.SetUser(c, u)

	return c.Request().Context()
}

func patHandlers(st pat.Store) *Handlers {
	return &Handlers{pats: pat.NewService(st, "viewer")} //nolint:exhaustruct
}

var oidcViewer = &auth.UserContext{AuthMethod: auth.MethodOIDC, Email: "dev@corp", Name: "dev", Role: "viewer"}

func TestPatCaller(t *testing.T) {
	t.Parallel()

	assert.Equal(t, pat.Caller{}, patCaller(nil))
	assert.Equal(t, pat.Caller{OIDC: true, Email: "dev@corp", Role: "viewer"}, patCaller(oidcViewer))
	assert.False(t, patCaller(&auth.UserContext{AuthMethod: auth.MethodPAT, Email: "dev@corp"}).OIDC)
	assert.False(t, patCaller(&auth.UserContext{AuthMethod: auth.MethodToken, Email: "dev@corp"}).OIDC)
}

func TestCreatePersonalTokenMapping(t *testing.T) {
	t.Parallel()

	admin := serverhttp.PersonalAccessTokenCreateRole("admin")

	tests := []struct {
		name string
		user *auth.UserContext
		body *serverhttp.PersonalAccessTokenCreate
		want serverhttp.CreatePersonalTokenResponseObject
	}{
		{"no user", nil, &serverhttp.PersonalAccessTokenCreate{Name: "ci"}, serverhttp.CreatePersonalToken403Response{}},
		{"static token", &auth.UserContext{AuthMethod: auth.MethodToken, Email: "dev@corp", Role: "admin"},
			&serverhttp.PersonalAccessTokenCreate{Name: "ci"}, serverhttp.CreatePersonalToken403Response{}},
		{"no body", oidcViewer, nil, serverhttp.CreatePersonalToken400Response{}},
		{"role above caller", oidcViewer, &serverhttp.PersonalAccessTokenCreate{Name: "ci", Role: &admin},
			serverhttp.CreatePersonalToken403Response{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, err := patHandlers(&fakePATStore{}).CreatePersonalToken(userCtx(tt.user),
				serverhttp.CreatePersonalTokenRequestObject{Body: tt.body})
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp)
		})
	}
}

func TestCreatePersonalToken(t *testing.T) {
	t.Parallel()

	st := &fakePATStore{}
	days := 7

	resp, err := patHandlers(st).CreatePersonalToken(userCtx(oidcViewer), serverhttp.CreatePersonalTokenRequestObject{
		Body: &serverhttp.PersonalAccessTokenCreate{Name: "ci", ExpiresInDays: &days},
	})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.CreatePersonalToken201JSONResponse)
	require.True(t, ok, "%T", resp)
	assert.Equal(t, "id-1", got.Id)
	assert.Equal(t, "ci", got.Name)
	assert.Equal(t, serverhttp.PersonalAccessTokenCreatedRole("viewer"), got.Role)
	assert.NotEmpty(t, got.Token)
	assert.NotEmpty(t, got.Prefix)
	require.NotNil(t, got.ExpiresAt)
	assert.Equal(t, got.CreatedAt.Add(7*24*time.Hour), *got.ExpiresAt)
	assert.Equal(t, "dev@corp", st.subject)
}

func TestCreatePersonalTokenWithoutStorage(t *testing.T) {
	t.Parallel()

	_, err := patHandlers(nil).CreatePersonalToken(userCtx(oidcViewer), serverhttp.CreatePersonalTokenRequestObject{
		Body: &serverhttp.PersonalAccessTokenCreate{Name: "ci"},
	})
	require.Error(t, err)
}

func TestListPersonalTokens(t *testing.T) {
	t.Parallel()

	for _, h := range []*Handlers{patHandlers(&fakePATStore{}), patHandlers(nil)} {
		resp, err := h.ListPersonalTokens(userCtx(nil), serverhttp.ListPersonalTokensRequestObject{})
		require.NoError(t, err)
		assert.Equal(t, serverhttp.ListPersonalTokens200JSONResponse{}, resp)
	}

	resp, err := patHandlers(&fakePATStore{}).ListPersonalTokens(userCtx(oidcViewer), serverhttp.ListPersonalTokensRequestObject{})
	require.NoError(t, err)

	got, ok := resp.(serverhttp.ListPersonalTokens200JSONResponse)
	require.True(t, ok, "%T", resp)
	require.Len(t, got, 1)
	assert.Equal(t, "id-1", got[0].Id)
	assert.Equal(t, serverhttp.PersonalAccessTokenRole("viewer"), got[0].Role)
}

func TestRevokePersonalToken(t *testing.T) {
	t.Parallel()

	req := serverhttp.RevokePersonalTokenRequestObject{Id: "id-1"}

	tests := []struct {
		name string
		h    *Handlers
		user *auth.UserContext
		want serverhttp.RevokePersonalTokenResponseObject
	}{
		{"revoked", patHandlers(&fakePATStore{revokeOK: true}), oidcViewer, serverhttp.RevokePersonalToken204Response{}},
		{"not found", patHandlers(&fakePATStore{}), oidcViewer, serverhttp.RevokePersonalToken404Response{}},
		{"no subject", patHandlers(&fakePATStore{revokeOK: true}), nil, serverhttp.RevokePersonalToken404Response{}},
		{"no storage", patHandlers(nil), oidcViewer, serverhttp.RevokePersonalToken404Response{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, err := tt.h.RevokePersonalToken(userCtx(tt.user), req)
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp)
		})
	}
}
