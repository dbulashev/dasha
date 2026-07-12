package mcpserver

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dbulashev/dasha/gen/apiclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	if len(res.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(res.Content))
	}

	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want *mcp.TextContent", res.Content[0])
	}

	return tc.Text
}

func TestJSONResult_Success(t *testing.T) {
	t.Parallel()

	res, _, err := jsonResult(map[string]int{"a": 1}, nil)
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}

	if res.IsError {
		t.Errorf("IsError = true, want false")
	}

	if got := textOf(t, res); got != `{"a":1}` {
		t.Errorf("text = %q, want compact JSON %q", got, `{"a":1}`)
	}
}

func TestJSONResult_Error(t *testing.T) {
	t.Parallel()

	res, _, err := jsonResult(nil, errors.New("boom"))
	if err != nil {
		t.Fatalf("jsonResult must not surface a protocol error: %v", err)
	}

	if !res.IsError {
		t.Errorf("IsError = false, want true")
	}

	if got := textOf(t, res); got != "boom" {
		t.Errorf("text = %q, want %q", got, "boom")
	}
}

func TestJSONResult_OversizedRefused(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", maxResultBytes+1)

	res, _, err := jsonResult(map[string]string{"blob": big}, nil)
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}

	if !res.IsError {
		t.Errorf("oversized result must be IsError")
	}

	got := textOf(t, res)
	if !strings.Contains(got, "result too large") {
		t.Errorf("text = %q, want it to mention 'result too large'", got)
	}
}

func TestJSONResult_UnderLimitPasses(t *testing.T) {
	t.Parallel()

	res, _, err := jsonResult(map[string]string{"ok": "small"}, nil)
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}

	if res.IsError {
		t.Errorf("small result must not be IsError")
	}
}

func TestSection(t *testing.T) {
	t.Parallel()

	out := map[string]any{}

	section(out, "status", "ok", nil)
	if out["status"] != "ok" {
		t.Errorf("status = %v, want ok", out["status"])
	}

	if _, exists := out["status_error"]; exists {
		t.Errorf("status_error must not be set on success")
	}

	section(out, "slots", nil, errors.New("denied"))
	if out["slots_error"] != "denied" {
		t.Errorf("slots_error = %v, want denied", out["slots_error"])
	}

	if _, exists := out["slots"]; exists {
		t.Errorf("slots must not be set on error")
	}
}

func TestSectionsResult_AllFailedIsError(t *testing.T) {
	t.Parallel()

	// Every sub-request errored (e.g. permission denied on all) -> must be IsError
	// so the model does not read an all-errors payload as a successful result.
	allFailed := map[string]any{}
	section(allFailed, "status", nil, errors.New("access denied"))
	section(allFailed, "slots", nil, errors.New("access denied"))

	res, _, err := sectionsResult(allFailed)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if !res.IsError {
		t.Errorf("IsError = false, want true when every section failed")
	}
}

func TestSectionsResult_PartialSuccessNotError(t *testing.T) {
	t.Parallel()

	// At least one section succeeded -> a normal (non-error) result the model
	// can use, with the per-part error preserved inside.
	out := map[string]any{}
	section(out, "status", "ok", nil)
	section(out, "slots", nil, errors.New("denied"))

	res, _, err := sectionsResult(out)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if res.IsError {
		t.Errorf("IsError = true, want false when a section succeeded")
	}

	if body := textOf(t, res); !strings.Contains(body, "slots_error") {
		t.Errorf("result must retain the per-part error, got %s", body)
	}
}

func TestTrendWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rng      string
		wantSpan time.Duration
		wantStep int
	}{
		{"", 24 * time.Hour, 300},
		{"24h", 24 * time.Hour, 300},
		{"7d", 7 * 24 * time.Hour, 1800},
		{"30d", 30 * 24 * time.Hour, 3600},
		{"bogus", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.rng, func(t *testing.T) {
			t.Parallel()

			span, step := trendWindow(tt.rng)
			if span != tt.wantSpan || step != tt.wantStep {
				t.Errorf("trendWindow(%q) = (%v, %d), want (%v, %d)", tt.rng, span, step, tt.wantSpan, tt.wantStep)
			}
		})
	}
}

func TestLogsParams_Defaults(t *testing.T) {
	t.Parallel()

	p, errMsg := logsParams(searchLogsArgs{Cluster: "prod"}) //nolint:exhaustruct
	if errMsg != "" {
		t.Fatalf("logsParams(defaults) error = %q, want none", errMsg)
	}

	if p.ClusterName != "prod" || p.ServiceType != "postgresql" {
		t.Errorf("target = (%v, %v), want (prod, postgresql)", p.ClusterName, p.ServiceType)
	}

	if p.Dedup == nil || !*p.Dedup {
		t.Errorf("dedup must default to true")
	}

	if p.PageSize == nil || *p.PageSize != logsDefaultPageSize {
		t.Errorf("page_size must default to %d", logsDefaultPageSize)
	}

	if got := p.To.Sub(p.From); got != logsDefaultSince {
		t.Errorf("window = %v, want %v", got, logsDefaultSince)
	}

	if p.Severity != nil || p.Host != nil || p.PageToken != nil {
		t.Errorf("unset optional params must be omitted (nil)")
	}
}

func TestLogsParams_Errors(t *testing.T) {
	t.Parallel()

	boolPtr := func(v bool) *bool { return &v }

	tests := []struct {
		name string
		args searchLogsArgs
	}{
		{"bad service type", searchLogsArgs{Cluster: "c", ServiceType: "syslog"}},                                 //nolint:exhaustruct
		{"bad since", searchLogsArgs{Cluster: "c", Since: "yesterday"}},                                           //nolint:exhaustruct
		{"negative since", searchLogsArgs{Cluster: "c", Since: "-5m"}},                                            //nolint:exhaustruct
		{"from without to", searchLogsArgs{Cluster: "c", From: "2026-07-10T12:00:00Z"}},                           //nolint:exhaustruct
		{"bad from", searchLogsArgs{Cluster: "c", From: "10.07.2026", To: "2026-07-10T13:00:00Z"}},                //nolint:exhaustruct
		{"from after to", searchLogsArgs{Cluster: "c", From: "2026-07-10T13:00:00Z", To: "2026-07-10T12:00:00Z"}}, //nolint:exhaustruct
		{"page_token with dedup", searchLogsArgs{Cluster: "c", PageToken: "tok", Dedup: boolPtr(true)}},           //nolint:exhaustruct
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, errMsg := logsParams(tt.args); errMsg == "" {
				t.Errorf("logsParams(%s) must return a validation error", tt.name)
			}
		})
	}
}

func TestLogsParams_PageTokenImpliesRaw(t *testing.T) {
	t.Parallel()

	p, errMsg := logsParams(searchLogsArgs{Cluster: "c", PageToken: "tok"}) //nolint:exhaustruct
	if errMsg != "" {
		t.Fatalf("logsParams(page_token) error = %q, want none", errMsg)
	}

	if p.Dedup == nil || *p.Dedup {
		t.Errorf("a page_token continuation must force dedup=false")
	}

	if p.PageToken == nil || *p.PageToken != "tok" {
		t.Errorf("page_token must pass through")
	}
}

func TestLogsParams_ExplicitWindow(t *testing.T) {
	t.Parallel()

	p, errMsg := logsParams(searchLogsArgs{ //nolint:exhaustruct
		Cluster: "c", From: "2026-07-10T12:00:00Z", To: "2026-07-10T13:00:00Z", Since: "24h",
	})
	if errMsg != "" {
		t.Fatalf("logsParams(from/to) error = %q, want none", errMsg)
	}

	// Explicit from/to wins over since.
	if got := p.To.Sub(p.From); got != time.Hour {
		t.Errorf("window = %v, want 1h (from/to must override since)", got)
	}
}

func TestClip(t *testing.T) {
	t.Parallel()

	if got := clip("short"); got != "short" {
		t.Errorf("clip(short) = %q, must be unchanged", got)
	}

	long := strings.Repeat("я", maxLogFieldBytes) // 2-byte runes force a boundary adjustment
	got := clip(long)

	if !strings.Contains(got, "[truncated,") {
		t.Errorf("clip(long) must carry the truncation marker, got tail %q", got[len(got)-40:])
	}

	if !utf8.ValidString(got) {
		t.Errorf("clip must cut on a rune boundary")
	}
}

func TestTruncateLogEntries(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxLogFieldBytes+1)
	text := long
	fields := map[string]string{"query": long, "message": "ok"}
	res := &apiclient.LogSearchResult{ //nolint:exhaustruct
		Items: []apiclient.LogEntry{{Text: &text, Fields: &fields}}, //nolint:exhaustruct
	}

	truncateLogEntries(res)

	if !strings.Contains(*res.Items[0].Text, "[truncated,") {
		t.Errorf("text must be clipped")
	}

	if !strings.Contains((*res.Items[0].Fields)["query"], "[truncated,") {
		t.Errorf("fields values must be clipped")
	}

	if (*res.Items[0].Fields)["message"] != "ok" {
		t.Errorf("short field values must be unchanged")
	}
}

func TestOptStrings(t *testing.T) {
	t.Parallel()

	if optStrings(nil) != nil {
		t.Errorf("optStrings(nil) must be nil so the param is omitted")
	}

	if optStrings([]string{}) != nil {
		t.Errorf("optStrings(empty) must be nil so the param is omitted")
	}

	p := optStrings([]string{"alice", "bob"})
	if p == nil {
		t.Fatalf("optStrings(non-empty) must not be nil")
	}

	if len(*p) != 2 || (*p)[0] != "alice" {
		t.Errorf("optStrings returned %v, want [alice bob]", *p)
	}
}

func TestDBSuffix(t *testing.T) {
	t.Parallel()

	if got := dbSuffix(""); got != "" {
		t.Errorf("dbSuffix(empty) = %q, want empty", got)
	}

	if got := dbSuffix("app"); got != " (database app)" {
		t.Errorf("dbSuffix(app) = %q, want %q", got, " (database app)")
	}
}
