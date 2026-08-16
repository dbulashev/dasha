package http

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/dbulashev/dasha/gen/serverhttp"
	"github.com/dbulashev/dasha/internal/indexadvisor"
	"github.com/dbulashev/dasha/internal/pkg/mapstruct"
	"github.com/dbulashev/dasha/internal/pkg/shortcut"
	"github.com/dbulashev/dasha/internal/repository"
)

const defaultIndexAdvisorLimit = 30

// GetIndexesAdvisor serves the index candidate report for one database, built
// from the workload of every host of the cluster.
func (s *Handlers) GetIndexesAdvisor(
	ctx context.Context,
	req serverhttp.GetIndexesAdvisorRequestObject,
) (serverhttp.GetIndexesAdvisorResponseObject, error) {
	// A disabled feature answers 404 rather than an empty report: an empty
	// candidate list is a statement about the database, and this one would be a
	// statement about the configuration. The two must not look alike.
	if !s.cfg.IndexAdvisor.IsEnabled() {
		return serverhttp.GetIndexesAdvisor404Response{}, nil
	}

	var excludeUsers []string
	if req.Params.ExcludeUsers != nil {
		excludeUsers = *req.Params.ExcludeUsers
	}

	report, err := s.repo.GetIndexAdvisorReport(ctx,
		req.Params.ClusterName, req.Params.Database, excludeUsers)
	if errors.Is(err, repository.ErrNotFound) {
		return serverhttp.GetIndexesAdvisor404Response{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("GetIndexesAdvisor | %w", err)
	}

	// Candidates are ranked against each other over the whole workload, so the
	// page is cut after the ranking — and total keeps the count the ranking saw.
	total := len(report.Candidates)
	limit, offset := paginationDefaults(req.Params.Limit, req.Params.Offset, defaultIndexAdvisorLimit)

	return serverhttp.GetIndexesAdvisor200JSONResponse{
		Candidates: mapstruct.SliceMap(pageOf(report.Candidates, limit, offset), indexAdvisorCandidate),
		NotParsed:  mapstruct.SliceMap(report.NotParsed, indexAdvisorNotParsed),
		Summary: serverhttp.IndexAdvisorSummary{
			PgssAvailable:     report.Summary.PgssAvailable,
			AnalyzedQueries:   report.Summary.AnalyzedQueries,
			CollapsedGroups:   report.Summary.CollapsedGroups,
			NotParsedCount:    report.Summary.NotParsedCount,
			CoveredTimePct:    report.Summary.CoveredTimePct,
			CatalogTruncated:  report.Summary.CatalogTruncated,
			Hosts:             emptyIfNil(report.Summary.Hosts),
			HostsWithoutStats: emptyIfNil(report.Summary.HostsWithoutStats),
		},
		UnreachableHosts: emptyIfNil(report.UnreachableHosts),
		Total:            total,
		DurationMs:       report.DurationMs,
	}, nil
}

func indexAdvisorCandidate(c indexadvisor.Candidate) serverhttp.IndexAdvisorCandidate {
	return serverhttp.IndexAdvisorCandidate{
		Schema:         c.Schema,
		Table:          c.Table,
		Columns:        c.Columns,
		Ddl:            c.DDL,
		WeightPct:      c.WeightPct,
		CoveredQueries: mapstruct.SliceMap(c.Covered, indexAdvisorCovered),
		TableRows:      c.TableRows,
		Writes: serverhttp.IndexAdvisorWrites{
			Inserted: c.Writes.Inserted,
			Updated:  c.Writes.Updated,
			Deleted:  c.Writes.Deleted,
			SeqScans: c.Writes.SeqScans,
			IdxScans: c.Writes.IdxScans,
		},
		Warnings:       mapstruct.SliceMap(c.Warnings, indexAdvisorWarning),
		PlannerChecked: c.PlannerChecked,
	}
}

func indexAdvisorCovered(q indexadvisor.CoveredQuery) serverhttp.IndexAdvisorCoveredQuery {
	ids := make([]string, 0, len(q.QueryIDs))
	for _, id := range q.QueryIDs {
		ids = append(ids, strconv.FormatInt(id, 10))
	}

	return serverhttp.IndexAdvisorCoveredQuery{
		QueryIds:    ids,
		Fingerprint: q.Fingerprint,
		Query:       q.Query,
		WeightPct:   q.WeightPct,
		Calls:       q.Calls,
		Hosts:       emptyIfNil(q.Hosts),
	}
}

func indexAdvisorWarning(w indexadvisor.Warning) serverhttp.IndexAdvisorWarning {
	out := serverhttp.IndexAdvisorWarning{
		Code:   serverhttp.IndexAdvisorWarningCode(w.Code),
		Params: nil,
	}

	// params is optional, and a warning that quotes no numbers leaves it absent
	// rather than serializing an empty object.
	if len(w.Params) > 0 {
		out.Params = shortcut.Ptr(w.Params)
	}

	return out
}

func indexAdvisorNotParsed(n indexadvisor.NotParsed) serverhttp.IndexAdvisorNotParsed {
	return serverhttp.IndexAdvisorNotParsed{
		ReasonCode: n.ReasonCode,
		Count:      n.Count,
	}
}
