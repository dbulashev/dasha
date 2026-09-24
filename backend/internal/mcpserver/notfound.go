package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errNotFound = errors.New("dasha: not found (404)")

const maxSuggestions = 5

const (
	scopeCluster  = "cluster"
	scopeInstance = "instance"
	scopeDatabase = "database"
	scopeFeature  = "feature"
)

type callTarget struct {
	Cluster  string `json:"cluster"`
	Instance string `json:"instance"`
	Database string `json:"database"`
}

type clusterCandidate struct {
	Name      string `json:"name"`
	Source    string `json:"source,omitempty"`
	Instances int    `json:"instances"`
	Databases int    `json:"databases"`
}

type notFoundAnswer struct {
	Error      string `json:"error"`
	Scope      string `json:"scope"`
	Given      string `json:"given,omitempty"`
	Cluster    string `json:"cluster,omitempty"`
	DidYouMean any    `json:"did_you_mean,omitempty"`
	Total      int    `json:"total,omitempty"`
	Hint       string `json:"hint"`
}

// explainNotFound names what did not resolve behind a 404. It never retries
// under a suggested name: the answer stays an error.
func (r *callRecord) explainNotFound(ctx context.Context, fallback string) *mcp.CallToolResult {
	if r == nil || r.client == nil {
		return errResult(fallback)
	}

	var t callTarget
	if err := json.Unmarshal(r.args, &t); err != nil || t.Cluster == "" {
		return errResult(fallback)
	}

	clusters, err := r.client.Clusters(ctx)
	if err != nil {
		return errResult(fallback)
	}

	b, err := json.Marshal(resolveTarget(t, clusters))
	if err != nil {
		return errResult(fallback)
	}

	return errResult(string(b))
}

func resolveTarget(t callTarget, clusters []apiclient.Cluster) notFoundAnswer {
	i := slices.IndexFunc(clusters, func(c apiclient.Cluster) bool { return deref(c.Name) == t.Cluster })
	if i < 0 {
		return clusterMiss(t.Cluster, clusters)
	}

	cl := clusters[i]

	if t.Instance != "" {
		hosts := make([]string, 0)

		for _, inst := range deref(cl.Instances) {
			hosts = append(hosts, deref(inst.HostName))
		}

		if !slices.Contains(hosts, t.Instance) {
			return memberMiss(scopeInstance, t.Cluster, t.Instance, hosts)
		}
	}

	if dbs := deref(cl.Databases); t.Database != "" && len(dbs) > 0 && !slices.Contains(dbs, t.Database) {
		return memberMiss(scopeDatabase, t.Cluster, t.Database, dbs)
	}

	return notFoundAnswer{ //nolint:exhaustruct
		Error: "not_found",
		Scope: scopeFeature,
		Hint: "the cluster, instance and database all exist: the endpoint behind this tool is disabled in " +
			"Dasha's configuration (snapshot storage, metrics datasource, log source or index advisor); " +
			"other names will not help",
	}
}

func clusterMiss(given string, clusters []apiclient.Cluster) notFoundAnswer {
	byName := make(map[string]apiclient.Cluster, len(clusters))
	names := make([]string, 0, len(clusters))

	for _, c := range clusters {
		n := deref(c.Name)
		byName[n] = c
		names = append(names, n)
	}

	similar := similarNames(given, names, maxSuggestions)
	cands := make([]clusterCandidate, 0, len(similar))

	for _, n := range similar {
		c := byName[n]
		cands = append(cands, clusterCandidate{
			Name:      n,
			Source:    deref(c.Source),
			Instances: len(deref(c.Instances)),
			Databases: len(deref(c.Databases)),
		})
	}

	ans := notFoundAnswer{ //nolint:exhaustruct
		Error: "not_found",
		Scope: scopeCluster,
		Given: given,
		Total: len(clusters),
		Hint:  "call again with an exact cluster name from did_you_mean",
	}

	if len(cands) == 0 {
		ans.Hint = "no similar cluster name; call list_clusters"
	} else {
		ans.DidYouMean = cands
	}

	return ans
}

// memberMiss lists the cluster's own hosts or databases; with no similar name
// it falls back to the first few of them.
func memberMiss(scope, cluster, given string, names []string) notFoundAnswer {
	similar := similarNames(given, names, maxSuggestions)
	if len(similar) == 0 {
		sorted := slices.Sorted(slices.Values(names))
		similar = sorted[:min(maxSuggestions, len(sorted))]
	}

	return notFoundAnswer{ //nolint:exhaustruct
		Error:      "not_found",
		Scope:      scope,
		Given:      given,
		Cluster:    cluster,
		DidYouMean: similar,
		Total:      len(names),
		Hint:       "call again with an exact " + scope + " name of this cluster",
	}
}
