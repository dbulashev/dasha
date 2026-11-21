package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/auth"
	"github.com/dbulashev/dasha/internal/pat"
)

func patCaller(u *auth.UserContext) pat.Caller {
	if u == nil {
		return pat.Caller{}
	}

	return pat.Caller{OIDC: u.AuthMethod == auth.MethodOIDC, Email: u.Email, Role: u.Role}
}

func (s *Handlers) ListPersonalTokens(
	ctx context.Context,
	req serverhttp.ListPersonalTokensRequestObject,
) (serverhttp.ListPersonalTokensResponseObject, error) {
	rows, err := s.pats.List(ctx, patCaller(auth.UserFromContext(ctx)), includeRevoked(req.Params.IncludeRevoked))
	if err != nil {
		return nil, fmt.Errorf("ListPersonalTokens | %w", err)
	}

	out := make(serverhttp.ListPersonalTokens200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverhttp.PersonalAccessToken{
			Id:         r.ID,
			Name:       r.Name,
			Prefix:     r.Prefix,
			Role:       serverhttp.PersonalAccessTokenRole(r.Role),
			CreatedAt:  r.CreatedAt,
			LastUsedAt: r.LastUsedAt,
			ExpiresAt:  r.ExpiresAt,
			RevokedAt:  r.RevokedAt,
		})
	}

	return out, nil
}

// includeRevoked reads the optional include_revoked flag, defaulting to false so
// an omitted parameter keeps revoked tokens out of the listing.
func includeRevoked(v *bool) bool {
	return v != nil && *v
}

func (s *Handlers) CreatePersonalToken(
	ctx context.Context,
	req serverhttp.CreatePersonalTokenRequestObject,
) (serverhttp.CreatePersonalTokenResponseObject, error) {
	var mr pat.MintRequest
	if b := req.Body; b != nil {
		mr.Name = b.Name
		mr.ExpiresInDays = deref(b.ExpiresInDays)

		if b.Role != nil {
			mr.Role = string(*b.Role)
		}
	}

	m, err := s.pats.Mint(ctx, patCaller(auth.UserFromContext(ctx)), mr)

	switch {
	case errors.Is(err, pat.ErrForbidden):
		return serverhttp.CreatePersonalToken403Response{}, nil
	case errors.Is(err, pat.ErrInvalid):
		return serverhttp.CreatePersonalToken400Response{}, nil
	case err != nil:
		return nil, fmt.Errorf("CreatePersonalToken | %w", err)
	}

	return serverhttp.CreatePersonalToken201JSONResponse{
		Id:        m.ID,
		Name:      m.Name,
		Prefix:    m.Prefix,
		Role:      serverhttp.PersonalAccessTokenCreatedRole(m.Role),
		Token:     m.Secret,
		CreatedAt: m.CreatedAt,
		ExpiresAt: m.ExpiresAt,
	}, nil
}

func (s *Handlers) RevokePersonalToken(
	ctx context.Context,
	req serverhttp.RevokePersonalTokenRequestObject,
) (serverhttp.RevokePersonalTokenResponseObject, error) {
	ok, err := s.pats.Revoke(ctx, patCaller(auth.UserFromContext(ctx)), req.Id)
	if err != nil {
		return nil, fmt.Errorf("RevokePersonalToken | %w", err)
	}

	if !ok {
		return serverhttp.RevokePersonalToken404Response{}, nil
	}

	return serverhttp.RevokePersonalToken204Response{}, nil
}
