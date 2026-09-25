package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dbulashev/dasha/gen/apiclient"
)

func clusterListItem(name string, hosts int, dbs ...string) apiclient.Cluster {
	src, logs := "yandex-mdb", true
	inst := make([]apiclient.ClusterInstance, hosts)

	for i := range inst {
		h := name + "-h" + string(rune('1'+i))
		inst[i].HostName = &h
	}

	return apiclient.Cluster{ //nolint:exhaustruct
		Name: &name, Source: &src, SupportsLogs: &logs, Instances: &inst, Databases: &dbs,
	}
}

func clusterListFixture() []apiclient.Cluster {
	return []apiclient.Cluster{
		clusterListItem("shop-prod-dr", 2, "orders", "cart"),
		clusterListItem("billing", 3, "ledger"),
		clusterListItem("shop-prod", 3, "orders", "cart", "search"),
	}
}

func rowNames(r *listClustersResult) []string {
	out := make([]string, 0, len(r.Clusters))
	for _, c := range r.Clusters {
		out = append(out, c.Name)
	}

	return out
}

func TestListClusters_CompactView(t *testing.T) {
	t.Parallel()

	r := buildListClusters(clusterListFixture(), "", false)

	if r.Total != 3 || r.Matched != 3 || r.Details != nil {
		t.Fatalf("result = %+v", r)
	}

	if got := strings.Join(rowNames(r), ","); got != "billing,shop-prod,shop-prod-dr" {
		t.Errorf("rows = %s, want sorted by name", got)
	}

	row := r.Clusters[1]
	if row.Instances != 3 || row.Databases != 3 || !row.SupportsLogs || row.Source != "yandex-mdb" {
		t.Errorf("row = %+v", row)
	}

	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "host_name") || strings.Contains(string(b), "search") {
		t.Errorf("compact view must not carry host or database names: %s", b)
	}

	if n := r.note(); n == nil || n.Reason != shapeDefaultView || n.Total != 3 {
		t.Errorf("note = %+v", n)
	}
}

func TestListClusters_TokenFilter(t *testing.T) {
	t.Parallel()

	for filter, want := range map[string]string{
		"shop prod": "shop-prod,shop-prod-dr",
		"PROD_shop": "shop-prod,shop-prod-dr",
		"dr":        "shop-prod-dr",
		"bill":      "billing",
	} {
		r := buildListClusters(clusterListFixture(), filter, false)
		if got := strings.Join(rowNames(r), ","); got != want || r.Total != 3 {
			t.Errorf("cluster=%q: rows = %s (total %d), want %s", filter, got, r.Total, want)
		}
	}
}

func TestListClusters_ExactNameExpands(t *testing.T) {
	t.Parallel()

	r := buildListClusters(clusterListFixture(), "shop-prod", false)

	if r.Clusters != nil || len(r.Details) != 1 || deref(r.Details[0].Name) != "shop-prod" {
		t.Fatalf("exact name must return that cluster alone in full: %+v", r)
	}

	if len(deref(r.Details[0].Instances)) != 3 || r.note() != nil {
		t.Errorf("details = %+v, note = %+v", r.Details[0], r.note())
	}
}

func TestListClusters_WithInstances(t *testing.T) {
	t.Parallel()

	r := buildListClusters(clusterListFixture(), "shop", true)

	if r.Clusters != nil || len(r.Details) != 2 || r.note() != nil {
		t.Fatalf("with_instances must expand every match: %+v", r)
	}
}

func TestListClusters_NoMatchSuggests(t *testing.T) {
	t.Parallel()

	r := buildListClusters(clusterListFixture(), "biling", false)

	if r.Matched != 0 || r.Clusters != nil || r.Details != nil {
		t.Fatalf("no match must carry no cluster data: %+v", r)
	}

	if len(r.DidYouMean) != 1 || r.DidYouMean[0].Name != "billing" || r.Total != 3 || r.Hint == "" {
		t.Errorf("did_you_mean = %+v, total = %d", r.DidYouMean, r.Total)
	}

	if far := buildListClusters(clusterListFixture(), "warehouse", false); len(far.DidYouMean) != 0 || far.Hint == "" {
		t.Errorf("far miss = %+v", far)
	}
}

func TestListClusters_DoesNotReorderInput(t *testing.T) {
	t.Parallel()

	all := clusterListFixture()
	buildListClusters(all, "", false)

	if deref(all[0].Name) != "shop-prod-dr" {
		t.Errorf("input reordered: first = %s", deref(all[0].Name))
	}
}

func TestListClusters_OverBudgetReturnedUnderUnshapedCeiling(t *testing.T) {
	t.Parallel()

	ctx, rec := budgetCtx(256)

	res, _, _ := renderResult(ctx, buildListClusters(clusterListFixture(), "", true), nil)
	if res.IsError || rec.refused {
		t.Errorf("result = %s", contentText(res.Content[0]))
	}
}
