package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/pat"
)

func (s *Handlers) ListAllPersonalTokens(
	ctx context.Context,
	req serverhttp.ListAllPersonalTokensRequestObject,
) (serverhttp.ListAllPersonalTokensResponseObject, error) {
	rows, err := s.tokenAdmin.Tokens(ctx, patCaller(auth.UserFromContext(ctx)), includeRevoked(req.Params.IncludeRevoked))

	switch {
	case errors.Is(err, pat.ErrForbidden):
		return serverhttp.ListAllPersonalTokens403Response{}, nil
	case err != nil:
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
	ok, err := s.tokenAdmin.Revoke(ctx, patCaller(auth.UserFromContext(ctx)), req.Id)

	switch {
	case errors.Is(err, pat.ErrForbidden):
		return serverhttp.RevokeAnyPersonalToken403Response{}, nil
	case err != nil:
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
	rows, err := s.tokenAdmin.Users(ctx, patCaller(auth.UserFromContext(ctx)))

	switch {
	case errors.Is(err, pat.ErrForbidden):
		return serverhttp.ListUsers403Response{}, nil
	case err != nil:
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
