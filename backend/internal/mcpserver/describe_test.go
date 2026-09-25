package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBloatSkipped(t *testing.T) {
	t.Parallel()

	for typ, want := range map[string]string{
		"table":             "",
		"materialized_view": "",
		"partitioned_table": bloatPartitionedParent,
		"view":              bloatNotAHeap,
		"foreign_table":     bloatNotAHeap,
	} {
		got := bloatSkipped(&apiclient.TableDescribe{TableType: typ}) //nolint:exhaustruct
		if (got == nil && want != "") || (got != nil && got.Unavailable != want) {
			t.Errorf("%s: %+v, want %q", typ, got, want)
		}
	}

	if bloatSkipped(nil) != nil {
		t.Error("an unknown table must not skip the bloat call")
	}
}

func TestBloatFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: held", errObjectLocked), bloatLockTimeout},
		{fmt.Errorf("%w: slow", errQueryTimeout), bloatTimeout},
		{fmt.Errorf("%w: %w", errQueryTimeout, context.DeadlineExceeded), bloatTimeout},
		{fmt.Errorf("%w: status 500", errBloatFailed), bloatError},
	} {
		u := bloatFailure(tc.err)
		if u == nil || u.Unavailable != tc.want || u.Hint == "" || u.Detail != tc.err.Error() {
			t.Errorf("%v: %+v, want %s", tc.err, u, tc.want)
		}
	}

	for _, err := range []error{nil, errors.New("dasha: rate limited (429) on describe_table"), errNotFound} {
		if u := bloatFailure(err); u != nil {
			t.Errorf("%v must stay a bloat_error, got %+v", err, u)
		}
	}
}

func partitionList(n int) []apiclient.TableDescribePartition {
	out := make([]apiclient.TableDescribePartition, n)
	for i := range out {
		out[i] = apiclient.TableDescribePartition{ //nolint:exhaustruct
			Name: "events_p" + strconv.Itoa(i), Schema: "public", PartitionExpression: strings.Repeat("x", 200),
		}
	}

	return out
}

func TestTableSections_ShrinkCutsPartitionsOnly(t *testing.T) {
	t.Parallel()

	s := tableSections{"table": "t", "vacuum_stats": "v", "partitions": partitionList(50)}

	next, step, ok := s.shrink()
	if !ok || step != "partitions=25" {
		t.Fatalf("shrink = %q %v", step, ok)
	}

	n, _ := next.(tableSections)
	if len(n["partitions"].([]apiclient.TableDescribePartition)) != 25 || n["table"] != "t" || n["vacuum_stats"] != "v" {
		t.Errorf("next = %+v", n)
	}

	if len(s["partitions"].([]apiclient.TableDescribePartition)) != 50 {
		t.Error("shrink must not mutate the receiver")
	}

	floor := tableSections{"partitions": partitionList(partitionFloor)}
	if _, hint, ok := floor.shrink(); ok || hint == "" {
		t.Errorf("floor: ok=%v hint=%q", ok, hint)
	}

	if _, _, ok := (tableSections{"table": "t"}).shrink(); ok {
		t.Error("a table without partitions has nothing to shrink")
	}
}

func TestTableSections_BudgetShrinksThroughSectionsResult(t *testing.T) {
	t.Parallel()

	ctx, rec := budgetCtx(4 << 10)

	res, _, _ := sectionsResult(ctx, tableSections{"table": "t", "partitions": partitionList(50)})
	if res.IsError {
		t.Fatalf("refused: %s", contentText(res.Content[0]))
	}

	if len(rec.steps) == 0 || !strings.Contains(contentText(res.Content[0]), "partitions=") {
		t.Errorf("steps = %v, note = %s", rec.steps, contentText(res.Content[0]))
	}
}

// describeBackend serves describe_table's five endpoints for a table of typ,
// answering the bloat endpoint with bloatStatus.
func describeBackend(t *testing.T, typ string, bloatStatus int, bloatCalls *atomic.Int32) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/tables/describe":
			_, _ = fmt.Fprintf(w, `{"TableName":"events","Schema":"public","TableType":%q}`, typ)
		case "/api/tables/describe-bloat":
			bloatCalls.Add(1)
			w.WriteHeader(bloatStatus)

			if bloatStatus == http.StatusOK {
				_, _ = w.Write([]byte(`{"DeadTuplePercent":12.5}`))
			} else {
				_, _ = w.Write([]byte(`{"message":"blocked"}`))
			}
		case "/api/tables/describe-partitions":
			_, _ = w.Write([]byte(`[{"Name":"events_p1","Schema":"public"}]`))
		case "/api/tables/describe-row-estimate", "/api/tables/describe-vacuum-stats":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

func callDescribe(t *testing.T, url string) map[string]json.RawMessage {
	t.Helper()

	res, err := connect(t, url).CallTool(context.Background(), &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "describe_table",
		Arguments: map[string]any{"cluster": "demo", "instance": "h1", "database": "app", "table": "events"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if res.IsError {
		t.Fatalf("describe_table returned IsError: %s", firstText(res))
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(contentText(res.Content[len(res.Content)-1])), &out); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}

	return out
}

func TestE2E_DescribeTableBloat(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		typ         string
		status      int
		want        string
		bloatCalled bool
	}{
		{"heap", "table", http.StatusOK, "", true},
		{"partitioned parent", "partitioned_table", http.StatusOK, bloatPartitionedParent, false},
		{"locked", "table", http.StatusLocked, bloatLockTimeout, true},
		{"timeout", "table", http.StatusGatewayTimeout, bloatTimeout, true},
		{"failed", "table", http.StatusInternalServerError, bloatError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			out := callDescribe(t, describeBackend(t, tc.typ, tc.status, &calls))

			for _, key := range []string{"table", "partitions", "row_estimate", "vacuum_stats", "bloat"} {
				if _, ok := out[key]; !ok {
					t.Errorf("section %q missing: %v", key, out)
				}
			}

			if _, ok := out["bloat_error"]; ok {
				t.Errorf("bloat failure must be classified, not passed through: %s", out["bloat_error"])
			}

			var bloat struct {
				Unavailable string `json:"unavailable"`
			}
			_ = json.Unmarshal(out["bloat"], &bloat)

			if bloat.Unavailable != tc.want {
				t.Errorf("bloat = %s, want unavailable=%q", out["bloat"], tc.want)
			}

			if got := calls.Load() > 0; got != tc.bloatCalled {
				t.Errorf("bloat endpoint called = %v, want %v", got, tc.bloatCalled)
			}
		})
	}
}

func TestE2E_DescribeTableAllUnauthorized(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	res, err := connect(t, srv.URL).CallTool(context.Background(), &mcp.CallToolParams{ //nolint:exhaustruct
		Name:      "describe_table",
		Arguments: map[string]any{"cluster": "demo", "instance": "h1", "database": "app", "table": "events"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !res.IsError {
		t.Errorf("every section failed, want IsError: %s", firstText(res))
	}
}
