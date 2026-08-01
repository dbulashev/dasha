package http

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
	"github.com/dbulashev/dasha/internal/schemalint"
)

const defaultSchemaLintLimit = 100

// GetSchemaLint serves the structural-defect report for one database.
func (s *Handlers) GetSchemaLint(
	ctx context.Context,
	req serverhttp.GetSchemaLintRequestObject,
) (serverhttp.GetSchemaLintResponseObject, error) {
	refresh := req.Params.Refresh != nil && *req.Params.Refresh

	report, err := s.repo.GetSchemaLintReport(ctx,
		req.Params.ClusterName, req.Params.Instance, req.Params.Database, refresh)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetSchemaLint404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetSchemaLint | %w", err)
	}

	// Filters narrow what is returned, never what was counted: the summary is the
	// answer to "what is the state of this schema", and a level filter is a way to
	// read it, not a different question.
	findings := filterSchemaLintFindings(report.Findings, req.Params)
	total := len(findings)

	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultSchemaLintLimit)
	findings = pageOf(findings, limit, offset)

	return serverhttp.GetSchemaLint200JSONResponse{
		Findings: mapstruct.SliceMap(findings, schemaLintFinding),
		Skipped:  mapstruct.SliceMap(report.Skipped, schemaLintSkip),
		Summary: serverhttp.SchemaLintSummary{
			Error:   report.Summary[schemalint.LevelError],
			Warning: report.Summary[schemalint.LevelWarning],
			Notice:  report.Summary[schemalint.LevelNotice],
		},
		Total:      total,
		Truncated:  report.Truncated,
		DurationMs: report.DurationMs,
	}, nil
}

// GetSchemaLintSummary serves the per-database counts for one instance.
func (s *Handlers) GetSchemaLintSummary(
	ctx context.Context,
	req serverhttp.GetSchemaLintSummaryRequestObject,
) (serverhttp.GetSchemaLintSummaryResponseObject, error) {
	summaries, err := s.repo.GetSchemaLintSummary(ctx, req.Params.ClusterName, req.Params.Instance)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetSchemaLintSummary404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetSchemaLintSummary | %w", err)
	}

	var ret serverhttp.GetSchemaLintSummary200JSONResponse = mapstruct.SliceMap(
		summaries,
		func(d schemalint.DatabaseSummary) serverhttp.SchemaLintDatabaseSummary {
			return serverhttp.SchemaLintDatabaseSummary{
				Database: d.Database,
				Error:    d.Error,
				Warning:  d.Warning,
				Notice:   d.Notice,
				Skipped:  d.Skipped,
				Failed:   d.Failed,
			}
		})

	return ret, nil
}

func filterSchemaLintFindings(
	findings []schemalint.Finding,
	params serverhttp.GetSchemaLintParams,
) []schemalint.Finding {
	schemaQuery := substringFilter(params.Schema)
	objectQuery := substringFilter(params.Object)

	if params.Level == nil && params.Code == nil && schemaQuery == "" && objectQuery == "" {
		return findings
	}

	out := make([]schemalint.Finding, 0, len(findings))

	for _, f := range findings {
		if params.Level != nil && string(f.Level) != string(*params.Level) {
			continue
		}

		if params.Code != nil && len(*params.Code) > 0 && !slices.Contains(*params.Code, f.Code) {
			continue
		}

		if schemaQuery != "" && !strings.Contains(strings.ToLower(f.Schema), schemaQuery) {
			continue
		}

		if objectQuery != "" && !strings.Contains(strings.ToLower(f.Object), objectQuery) {
			continue
		}

		out = append(out, f)
	}

	return out
}

func substringFilter(v *string) string {
	if v == nil {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(*v))
}

func schemaLintFinding(f schemalint.Finding) serverhttp.SchemaLintFinding {
	out := serverhttp.SchemaLintFinding{
		Code:       f.Code,
		Level:      serverhttp.SchemaLintFindingLevel(f.Level),
		ObjectType: serverhttp.SchemaLintFindingObjectType(f.ObjectType),
		Schema:     f.Schema,
		Object:     f.Object,
	}

	if f.RelatedRoute != "" {
		out.RelatedRoute = shortcut.Ptr(f.RelatedRoute)
	}

	if params, ok := schemaLintParams(f.Params); ok {
		out.Params = &params
	}

	return out
}

// schemaLintParams leaves absent every field this finding's wording does not
// quote, so a client never has to guess whether a zero is a value or a default.
func schemaLintParams(p schemalint.Params) (serverhttp.SchemaLintFindingParams, bool) {
	out := serverhttp.SchemaLintFindingParams{
		UsedPct:   p.UsedPct,
		LastValue: p.LastValue,
		MaxValue:  p.MaxValue,
	}

	set := p.UsedPct != nil || p.LastValue != nil || p.MaxValue != nil

	if p.OwnedBy != "" {
		out.OwnedBy = shortcut.Ptr(p.OwnedBy)
		set = true
	}

	if p.OwnedColumnType != "" {
		out.OwnedColumnType = shortcut.Ptr(p.OwnedColumnType)
		set = true
	}

	if p.Owner != "" {
		out.Owner = shortcut.Ptr(p.Owner)
		set = true
	}

	if p.Column != "" {
		out.Column = shortcut.Ptr(p.Column)
		set = true
	}

	if p.ColumnType != "" {
		out.ColumnType = shortcut.Ptr(p.ColumnType)
		set = true
	}

	if p.Constraint != "" {
		out.Constraint = shortcut.Ptr(p.Constraint)
		set = true
	}

	if p.ReferencedBy != "" {
		out.ReferencedBy = shortcut.Ptr(p.ReferencedBy)
		set = true
	}

	if p.Index != "" {
		out.Index = shortcut.Ptr(p.Index)
		set = true
	}

	if p.OtherObject != "" {
		out.OtherObject = shortcut.Ptr(p.OtherObject)
		set = true
	}

	if p.Partitions > 0 {
		out.Partitions = shortcut.Ptr(p.Partitions)
		set = true
	}

	if p.UniqueNullable {
		out.UniqueNullable = shortcut.Ptr(true)
		set = true
	}

	return out, set
}

func schemaLintSkip(s schemalint.Skip) serverhttp.SchemaLintSkip {
	out := serverhttp.SchemaLintSkip{
		Code:   s.Code,
		Reason: serverhttp.SchemaLintSkipReason(s.Reason),
	}

	if s.Detail != "" {
		out.Detail = shortcut.Ptr(s.Detail)
	}

	if s.Count > 0 {
		out.Count = shortcut.Ptr(s.Count)
	}

	// Only worth saying when the check was refused: on any other reason a grant
	// would not change the outcome.
	if s.Reason == schemalint.SkipInsufficientPrivileges {
		if priv := schemalint.RequiredPrivilege(s.Code); priv != "" {
			out.RequiredPrivilege = shortcut.Ptr(priv)
		}
	}

	return out
}
