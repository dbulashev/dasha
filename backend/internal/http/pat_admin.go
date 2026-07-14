package http

import (
	"context"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/config"
)

// patAdmin reports whether the caller may administer other principals' tokens.
// The bar is the same as for minting (see patSubject): an interactive OIDC
// session, here additionally holding the admin role. PAT-authenticated callers
// are excluded even when the token carries the admin role — anti-chaining, so a
// leaked token cannot enumerate or revoke the tokens that would replace it.
func patAdmin(u *auth.UserContext) bool {
	if u == nil || u.AuthMethod != auth.MethodOIDC {
		return false
	}

	return u.Role == config.RoleAdmin
}

func (s *Handlers) ListAllPersonalTokens(
	ctx context.Context,
	req serverhttp.ListAllPersonalTokensRequestObject,
) (serverhttp.ListAllPersonalTokensResponseObject, error) {
	if !patAdmin(auth.UserFromContext(ctx)) || s.storage == nil {
		return serverhttp.ListAllPersonalTokens403Response{}, nil
	}

	rows, err := s.storage.ListAllAPITokens(ctx, includeRevoked(req.Params.IncludeRevoked))
	if err != nil {
		return nil, fmt.Errorf("ListAllPersonalTokens | %w", err)
	}

	out := make(serverhttp.ListAllPersonalTokens200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.AdminPersonalAccessToken{
			Id:         r.ID,
			Name:       r.Name,
			Owner:      r.Subject,
			Prefix:     r.Prefix,
			Role:       serverhttp.AdminPersonalAccessTokenRole(r.Role),
			CreatedAt:  r.CreatedAt,
			LastUsedAt: r.LastUsedAt,
			ExpiresAt:  r.ExpiresAt,
			RevokedAt:  r.RevokedAt,
		})
	}

	return out, nil
}

func (s *Handlers) RevokeAnyPersonalToken(
	ctx context.Context,
	req serverhttp.RevokeAnyPersonalTokenRequestObject,
) (serverhttp.RevokeAnyPersonalTokenResponseObject, error) {
	if !patAdmin(auth.UserFromContext(ctx)) || s.storage == nil {
		return serverhttp.RevokeAnyPersonalToken403Response{}, nil
	}

	ok, err := s.storage.RevokeAPITokenByID(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("RevokeAnyPersonalToken | %w", err)
	}

	if !ok {
		return serverhttp.RevokeAnyPersonalToken404Response{}, nil
	}

	return serverhttp.RevokeAnyPersonalToken204Response{}, nil
}

func (s *Handlers) ListUsers(
	ctx context.Context,
	_ serverhttp.ListUsersRequestObject,
) (serverhttp.ListUsersResponseObject, error) {
	if !patAdmin(auth.UserFromContext(ctx)) || s.storage == nil {
		return serverhttp.ListUsers403Response{}, nil
	}

	rows, err := s.storage.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("ListUsers | %w", err)
	}

	out := make(serverhttp.ListUsers200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.AdminUser{
			Subject:     r.Subject,
			Name:        r.Name,
			Role:        serverhttp.AdminUserRole(r.Role),
			CreatedAt:   r.CreatedAt,
			LastLoginAt: r.LastLoginAt,
			Tokens:      r.Tokens,
		})
	}

	return out, nil
}
