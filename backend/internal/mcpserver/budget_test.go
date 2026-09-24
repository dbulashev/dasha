package mcpserver

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeShaped struct {
	Items []int `json:"items"`
	total int
}

func newFakeShaped(n int) fakeShaped {
	items := make([]int, n)
	for i := range items {
		items[i] = 1000 + i
	}

	return fakeShaped{Items: items, total: n}
}

func (f fakeShaped) shrink() (shapedResult, string, bool) {
	if len(f.Items) <= 1 {
		return nil, "lower limit", false
	}

	n := len(f.Items) / 2

	return fakeShaped{Items: f.Items[:n], total: f.total}, "limit=" + strconv.Itoa(n), true
}

func (f fakeShaped) note() *shapeNote {
	return &shapeNote{Reason: shapeDefaultView, Folded: "items", Total: f.total, Full: "limit=" + strconv.Itoa(f.total)} //nolint:exhaustruct
}

func budgetCtx(budget int) (context.Context, *callRecord) {
	rec := &callRecord{budget: budget} //nolint:exhaustruct

	return withRecord(context.Background(), rec), rec
}

func contentText(c mcp.Content) string {
	if tc, ok := c.(*mcp.TextContent); ok {
		return tc.Text
	}

	return ""
}

func TestJSONResult_UnshapedRefusedOverBudget(t *testing.T) {
	t.Parallel()

	ctx, rec := budgetCtx(64)

	res, _, _ := jsonResult(ctx)(map[string]string{"blob": strings.Repeat("x", 100)}, nil)
	if !res.IsError {
		t.Fatalf("a payload without shrink() over budget must be refused")
	}

	if got := textOf(t, res); !strings.Contains(got, "result too large") || !strings.Contains(got, `"limit":64`) {
		t.Errorf("refusal = %s", got)
	}

	if !rec.refused {
		t.Errorf("refusal not recorded")
	}
}

func TestJSONResult_DefaultViewNoteComesFirst(t *testing.T) {
	t.Parallel()

	ctx, rec := budgetCtx(defaultMaxResultBytes)

	res, _, _ := jsonResult(ctx)(newFakeShaped(3), nil)
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("want a note block and a data block, got IsError=%v blocks=%d", res.IsError, len(res.Content))
	}

	note := contentText(res.Content[0])
	if !strings.HasPrefix(note, `{"shaped":`) || !strings.Contains(note, `"reason":"default_view"`) {
		t.Errorf("note = %s", note)
	}

	if strings.Contains(note, "budget_steps") {
		t.Errorf("no shrink happened, note = %s", note)
	}

	if got := contentText(res.Content[1]); got != `{"items":[1000,1001,1002]}` {
		t.Errorf("data = %s", got)
	}

	if !rec.shaped || len(rec.steps) != 0 {
		t.Errorf("record shaped=%v steps=%v", rec.shaped, rec.steps)
	}
}

func TestJSONResult_ShrinksUntilWithinBudget(t *testing.T) {
	t.Parallel()

	const budget = 400

	ctx, rec := budgetCtx(budget)

	res, _, _ := jsonResult(ctx)(newFakeShaped(200), nil)
	if res.IsError {
		t.Fatalf("shrinkable payload refused: %s", contentText(res.Content[0]))
	}

	note, data := contentText(res.Content[0]), contentText(res.Content[1])
	if len(note)+len(data) > budget {
		t.Errorf("result is %d bytes, budget %d", len(note)+len(data), budget)
	}

	var n struct {
		Shaped shapeNote `json:"shaped"`
	}
	if err := json.Unmarshal([]byte(note), &n); err != nil {
		t.Fatalf("note is not JSON: %v", err)
	}

	if n.Shaped.Reason != shapeBudget || n.Shaped.Total != 200 || len(n.Shaped.Steps) == 0 {
		t.Errorf("note = %+v", n.Shaped)
	}

	if got := n.Shaped.Steps[len(n.Shaped.Steps)-1]; got != rec.lastStep() {
		t.Errorf("recorded step %q, note says %q", rec.lastStep(), got)
	}
}

func TestJSONResult_ShrinkFloorRefusesWithToolHint(t *testing.T) {
	t.Parallel()

	ctx, rec := budgetCtx(10)

	res, _, _ := jsonResult(ctx)(newFakeShaped(50), nil)
	if !res.IsError {
		t.Fatalf("payload that cannot fit must be refused")
	}

	if got := textOf(t, res); !strings.Contains(got, `"suggestion":"lower limit"`) {
		t.Errorf("refusal must carry the tool's own hint: %s", got)
	}

	if !rec.refused {
		t.Errorf("refusal not recorded")
	}
}

func TestBudgetFor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		shaped bool
		budget int
		want   int
	}{
		{true, 0, defaultMaxResultBytes},
		{true, 32 << 10, 32 << 10},
		{false, 0, unshapedResultBytes},
		{false, 32 << 10, unshapedResultBytes},
		{false, 1 << 20, 1 << 20},
	} {
		if got := budgetFor(tc.shaped, tc.budget); got != tc.want {
			t.Errorf("budgetFor(%v, %d) = %d, want %d", tc.shaped, tc.budget, got, tc.want)
		}
	}
}

func TestToolStats_ConcurrentRecords(t *testing.T) {
	t.Parallel()

	stats := newToolStats()

	var wg sync.WaitGroup

	for i := range 100 {
		wg.Go(func() {
			rec := &callRecord{tool: "a", bytes: 1 << 10} //nolint:exhaustruct
			if i%4 == 0 {
				rec.bytes = 100 << 10
				rec.refused = true
			}

			if i%10 == 0 {
				rec.shaped, rec.steps = true, []string{"limit=5"}
			}

			stats.record(rec)
		})
	}

	wg.Wait()

	got := stats.snapshot(defaultMaxResultBytes).Tools["a"]

	if got.Calls != 100 || got.Refused != 25 || got.Shaped != 10 || got.Shrunk != 10 {
		t.Errorf("counters = %+v", got)
	}

	if got.MaxBytes != 100<<10 {
		t.Errorf("max_bytes = %d", got.MaxBytes)
	}

	if got.Sizes["le_4k"] != 75 || got.Sizes["le_256k"] != 25 {
		t.Errorf("sizes = %v", got.Sizes)
	}

	if want := int64((75*(1<<10) + 25*(100<<10)) / 100); got.AvgBytes != want {
		t.Errorf("avg_bytes = %d, want %d", got.AvgBytes, want)
	}
}
