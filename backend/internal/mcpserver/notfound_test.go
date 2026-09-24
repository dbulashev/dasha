package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fleetFixture = `[
 {"name":"acme-prod","source":"yandex-mdb","instances":[{"host_name":"h1"},{"host_name":"h2"}],"databases":["stock","cart"]},
 {"name":"acme-prod-dr","source":"yandex-mdb","instances":[{"host_name":"h9"}],"databases":["stock","cart"]},
 {"name":"billing","source":"static","instances":[{"host_name":"b1"}],"databases":["ledger"]}
]`

func fleet(t *testing.T) []apiclient.Cluster {
	t.Helper()

	var cl []apiclient.Cluster
	if err := json.Unmarshal([]byte(fleetFixture), &cl); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	return cl
}

func TestResolveTarget_Scopes(t *testing.T) {
	t.Parallel()

	cl := fleet(t)

	for _, tc := range []struct {
		name   string
		target callTarget
		scope  string
		first  string
	}{
		{"cluster", callTarget{Cluster: "acme prod"}, scopeCluster, "acme-prod"},                                 //nolint:exhaustruct
		{"instance", callTarget{Cluster: "acme-prod", Instance: "h3"}, scopeInstance, "h1"},                      //nolint:exhaustruct
		{"database", callTarget{Cluster: "acme-prod", Instance: "h1", Database: "stok"}, scopeDatabase, "stock"}, //nolint:exhaustruct
		{"feature", callTarget{Cluster: "acme-prod", Instance: "h1", Database: "stock"}, scopeFeature, ""},       //nolint:exhaustruct
		{"feature without instance", callTarget{Cluster: "billing", Database: "ledger"}, scopeFeature, ""},       //nolint:exhaustruct
	} {
		ans := resolveTarget(tc.target, cl)
		if ans.Scope != tc.scope {
			t.Errorf("%s: scope = %q, want %q", tc.name, ans.Scope, tc.scope)

			continue
		}

		b, _ := json.Marshal(ans.DidYouMean)
		if tc.first != "" && !strings.Contains(string(b), `"`+tc.first+`"`) {
			t.Errorf("%s: did_you_mean = %s, want it to start with %s", tc.name, b, tc.first)
		}
	}
}

func TestResolveTarget_ClusterCandidatesCarryDistinguishingFields(t *testing.T) {
	t.Parallel()

	ans := resolveTarget(callTarget{Cluster: "acme prod"}, fleet(t)) //nolint:exhaustruct

	cands, ok := ans.DidYouMean.([]clusterCandidate)
	if !ok || len(cands) != 2 {
		t.Fatalf("did_you_mean = %#v", ans.DidYouMean)
	}

	want := []clusterCandidate{
		{Name: "acme-prod", Source: "yandex-mdb", Instances: 2, Databases: 2},
		{Name: "acme-prod-dr", Source: "yandex-mdb", Instances: 1, Databases: 2},
	}
	if !slices.Equal(cands, want) {
		t.Errorf("did_you_mean = %+v, want %+v", cands, want)
	}

	if ans.Total != 3 {
		t.Errorf("total = %d, want 3", ans.Total)
	}
}

func TestResolveTarget_CandidatesCapped(t *testing.T) {
	t.Parallel()

	var cl []apiclient.Cluster

	for _, n := range []string{"pg-1", "pg-2", "pg-3", "pg-4", "pg-5", "pg-6", "pg-7"} {
		cl = append(cl, apiclient.Cluster{Name: &n}) //nolint:exhaustruct
	}

	ans := resolveTarget(callTarget{Cluster: "pg"}, cl) //nolint:exhaustruct
	if cands, _ := ans.DidYouMean.([]clusterCandidate); len(cands) != maxSuggestions {
		t.Errorf("did_you_mean has %d names, want %d", len(cands), maxSuggestions)
	}
}

// TestE2E_NotFoundNamesTheMiss drives a 404 through the full middleware chain:
// the answer names the scope, stays IsError, and the cluster list is fetched
// only on the error path.
func TestE2E_NotFoundNamesTheMiss(t *testing.T) {
	t.Parallel()

	var clusterCalls atomic.Int32

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/clusters":
			clusterCalls.Add(1)
			_, _ = w.Write([]byte(fleetFixture))
		case r.URL.Query().Get("cluster_name") == "acme-prod":
			_, _ = w.Write([]byte(`{"score":91,"categories":[],"has_replication":false,"in_recovery":false}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	cs := connect(t, backend.URL)
	ctx := context.Background()

	call := func(cluster string) *mcp.CallToolResult {
		t.Helper()

		res, err := cs.CallTool(ctx, &mcp.CallToolParams{ //nolint:exhaustruct
			Name:      "get_health_score",
			Arguments: map[string]any{"cluster": cluster, "instance": "h1"},
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}

		return res
	}

	if res := call("acme-prod"); res.IsError {
		t.Fatalf("known target returned IsError: %s", firstText(res))
	}

	if n := clusterCalls.Load(); n != 0 {
		t.Errorf("success path fetched the cluster list %d times", n)
	}

	res := call("acme prod")
	if !res.IsError {
		t.Fatalf("a suggestion must not turn the miss into data: %s", firstText(res))
	}

	got := firstText(res)
	for _, want := range []string{`"scope":"cluster"`, `"given":"acme prod"`, `"name":"acme-prod"`, `"name":"acme-prod-dr"`} {
		if !strings.Contains(got, want) {
			t.Errorf("answer lacks %s: %s", want, got)
		}
	}

	if strings.Contains(got, `"score"`) {
		t.Errorf("answer carries data under a suggested name: %s", got)
	}

	if n := clusterCalls.Load(); n != 1 {
		t.Errorf("error path fetched the cluster list %d times, want 1", n)
	}
}

func TestE2E_ToolStatsResource(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fleetFixture))
	}))
	defer backend.Close()

	cs := connect(t, backend.URL)
	ctx := context.Background()

	for range 2 {
		if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_clusters"}); err != nil { //nolint:exhaustruct
			t.Fatalf("CallTool: %v", err)
		}
	}

	rr, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: toolStatsURI}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	var view toolStatsView
	if err := json.Unmarshal([]byte(rr.Contents[0].Text), &view); err != nil {
		t.Fatalf("stats are not JSON: %v", err)
	}

	st := view.Tools["list_clusters"]
	if st.Calls != 2 || st.MaxBytes == 0 {
		t.Errorf("list_clusters stats = %+v", st)
	}
}

func connect(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()

	client, err := NewDashaClient(Config{DashaURL: url, Token: "t"}) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("NewDashaClient: %v", err)
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "en").Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	t.Cleanup(func() { _ = ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil).Connect(ctx, ct, nil) //nolint:exhaustruct
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	t.Cleanup(func() { _ = cs.Close() })

	return cs
}
