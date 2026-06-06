package mcpserver

import (
	"errors"
	"strings"
	"testing"
	"time"

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
