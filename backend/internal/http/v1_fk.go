package http

import (
	"context"
	"errors"
	"fmt"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/dto"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/repository"
)

func (s *Handlers) GetFkTypeMismatch(
	ctx context.Context,
	req serverhttp.GetFkTypeMismatchRequestObject,
) (serverhttp.GetFkTypeMismatchResponseObject, error) {
	fkTypeMismatches, err := s.repo.GetFkTypeMismatch(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetFkTypeMismatch404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetFkTypeMismatch | %w", err)
	}

	var ret serverhttp.GetFkTypeMismatch200JSONResponse = mapstruct.SliceMap(
		fkTypeMismatches,
		func(t dto.FkTypeMismatch) serverhttp.FkTypeMismatch {
			return serverhttp.FkTypeMismatch{
				FkName:        t.FkName,
				FromRel:       t.FromRel,
				RelAttNames:   t.RelAttNames,
				ToRel:         t.ToRel,
				ToRelAttNames: t.ToRelAttNames,
			}
		})

	return ret, nil
}

func (s *Handlers) GetFksPossibleNulls(
	ctx context.Context,
	req serverhttp.GetFksPossibleNullsRequestObject,
) (serverhttp.GetFksPossibleNullsResponseObject, error) {
	fksPossibleNulls, err := s.repo.GetFksPossibleNulls(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetFksPossibleNulls404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetFksPossibleNulls | %w", err)
	}

	var ret serverhttp.GetFksPossibleNulls200JSONResponse = mapstruct.SliceMap(
		fksPossibleNulls,
		func(t dto.FksPossibleNulls) serverhttp.FksPossibleNulls {
			return serverhttp.FksPossibleNulls{
				FkName:   t.FkName,
				RelName:  t.RelName,
				AttNames: t.AttNames,
			}
		})

	return ret, nil
}

func (s *Handlers) GetFksPossibleSimilar(
	ctx context.Context,
	req serverhttp.GetFksPossibleSimilarRequestObject,
) (serverhttp.GetFksPossibleSimilarResponseObject, error) {
	fksPossibleSimilar, err := s.repo.GetFksPossibleSimilar(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetFksPossibleSimilar404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetFksPossibleSimilar | %w", err)
	}

	var ret serverhttp.GetFksPossibleSimilar200JSONResponse = mapstruct.SliceMap(
		fksPossibleSimilar,
		func(t dto.FksPossibleSimilar) serverhttp.FksPossibleSimilar {
			return serverhttp.FksPossibleSimilar{
				Table:   t.Table,
				FkName:  t.FkName,
				Fk1Name: t.Fk1Name,
			}
		})

	return ret, nil
}

func (s *Handlers) GetInvalidConstraints(
	ctx context.Context,
	req serverhttp.GetInvalidConstraintsRequestObject,
) (serverhttp.GetInvalidConstraintsResponseObject, error) {
	invalidConstraints, err := s.repo.GetInvalidConstraints(ctx, req.Params.ClusterName, req.Params.Instance, req.Params.Database)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetInvalidConstraints404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetInvalidConstraints | %w", err)
	}

	var ret serverhttp.GetInvalidConstraints200JSONResponse = mapstruct.SliceMap(
		invalidConstraints,
		func(t dto.InvalidConstraint) serverhttp.InvalidConstraint {
			return serverhttp.InvalidConstraint{
				Schema:           t.Schema,
				Table:            t.Table,
				Name:             t.Name,
				ReferencedSchema: t.ReferencedSchema,
				ReferencedTable:  t.ReferencedTable,
			}
		})

	return ret, nil
}
