package mcpserver

import (
	"cmp"
	"context"
	"slices"

	"github.com/dbulashev/dasha/gen/apiclient"
)

type clusterRow struct {
	Name         string `json:"name"`
	Source       string `json:"source,omitempty"`
	Instances    int    `json:"instances"`
	Databases    int    `json:"databases"`
	SupportsLogs bool   `json:"supports_logs"`
}

type listClustersResult struct {
	Total      int                 `json:"total"`
	Matched    int                 `json:"matched"`
	Clusters   []clusterRow        `json:"clusters,omitempty"`
	Details    []apiclient.Cluster `json:"details,omitempty"`
	DidYouMean []clusterCandidate  `json:"did_you_mean,omitempty"`
	Hint       string              `json:"hint,omitempty"`
}

func listClusters(ctx context.Context, c *DashaClient, a listClustersArgs) (any, error) {
	all, err := c.Clusters(ctx)
	if err != nil {
		return nil, err
	}

	return buildListClusters(all, a.Cluster, a.WithInstances), nil
}

func buildListClusters(all []apiclient.Cluster, filter string, withInstances bool) *listClustersResult {
	out := &listClustersResult{Total: len(all)} //nolint:exhaustruct

	matched := slices.Clone(all)

	if filter != "" {
		if i := slices.IndexFunc(all, func(c apiclient.Cluster) bool { return deref(c.Name) == filter }); i >= 0 {
			out.Matched = 1
			out.Details = all[i : i+1]

			return out
		}

		q := nameTokens(filter)
		matched = slices.DeleteFunc(matched, func(c apiclient.Cluster) bool {
			return !tokensMatch(q, nameTokens(deref(c.Name)))
		})
	}

	slices.SortFunc(matched, func(a, b apiclient.Cluster) int { return cmp.Compare(deref(a.Name), deref(b.Name)) })
	out.Matched = len(matched)

	if len(matched) == 0 && filter != "" {
		out.DidYouMean = similarClusters(filter, all)
		out.Hint = "no cluster name matches; call again with a name from did_you_mean"

		if len(out.DidYouMean) == 0 {
			out.Hint = "no cluster name matches or resembles it; omit cluster to list all"
		}

		return out
	}

	if withInstances {
		out.Details = matched

		return out
	}

	out.Clusters = make([]clusterRow, 0, len(matched))
	for _, c := range matched {
		out.Clusters = append(out.Clusters, clusterRow{
			Name:         deref(c.Name),
			Source:       deref(c.Source),
			Instances:    len(deref(c.Instances)),
			Databases:    len(deref(c.Databases)),
			SupportsLogs: deref(c.SupportsLogs),
		})
	}

	return out
}

func (r *listClustersResult) shrink() (shapedResult, string, bool) {
	return nil, "pass cluster to narrow the list", false
}

func (r *listClustersResult) note() *shapeNote {
	if r.Clusters == nil {
		return nil
	}

	return &shapeNote{ //nolint:exhaustruct
		Reason: shapeDefaultView,
		Folded: "host names, database names, log streams and severities",
		Total:  r.Matched,
		Full:   "cluster=<exact name>, or with_instances=true",
	}
}
