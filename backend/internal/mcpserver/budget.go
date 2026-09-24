package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolStatsURI = "dasha://mcp/tool-stats"

// callRecord is shared between the middlewares and jsonResult of one tools/call.
type callRecord struct {
	tool   string
	budget int
	args   json.RawMessage
	client *DashaClient

	bytes   int
	shaped  bool
	steps   []string
	refused bool
}

type callRecordKey struct{}

func withRecord(ctx context.Context, rec *callRecord) context.Context {
	return context.WithValue(ctx, callRecordKey{}, rec)
}

func recordFrom(ctx context.Context) *callRecord {
	rec, _ := ctx.Value(callRecordKey{}).(*callRecord)

	return rec
}

func (r *callRecord) budgetOrDefault() int {
	if r == nil || r.budget <= 0 {
		return defaultMaxResultBytes
	}

	return r.budget
}

func (r *callRecord) markShaped(shaped bool, steps []string) {
	if r != nil {
		r.shaped, r.steps = shaped, steps
	}
}

func (r *callRecord) markRefused() {
	if r != nil {
		r.refused = true
	}
}

func (r *callRecord) lastStep() string {
	if r == nil || len(r.steps) == 0 {
		return ""
	}

	return r.steps[len(r.steps)-1]
}

// budgetMiddleware sets the per-tool result budget for jsonResult and records
// the outcome of every tools/call in stats.
func budgetMiddleware(client *DashaClient, stats *toolStats) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			p, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return next(ctx, method, req)
			}

			rec := recordFrom(ctx)
			if rec == nil {
				rec = &callRecord{} //nolint:exhaustruct
				ctx = withRecord(ctx, rec)
			}

			rec.tool = p.Name
			rec.budget = budgetFor(shapedTools[p.Name], client.maxResultBytes)
			rec.args = p.Arguments
			rec.client = client

			res, err := next(ctx, method, req)

			if r, ok := res.(*mcp.CallToolResult); ok && r != nil {
				rec.bytes = contentBytes(r)
				stats.record(rec)
			}

			return res, err
		}
	}
}

func contentBytes(r *mcp.CallToolResult) int {
	n := 0

	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			n += len(t.Text)
		}
	}

	return n
}

// sizeBuckets are the upper bounds of the result-size histogram; the last
// bucket counts everything above the largest bound.
var sizeBuckets = [...]int{4 << 10, 16 << 10, 64 << 10, 256 << 10}

var sizeBucketNames = [...]string{"le_4k", "le_16k", "le_64k", "le_256k", "over_256k"}

type toolStat struct {
	calls    int
	shaped   int
	shrunk   int
	refused  int
	sumBytes int64
	maxBytes int
	buckets  [len(sizeBuckets) + 1]int
}

// toolStats is one per server: per process in stdio, per token in HTTP mode.
type toolStats struct {
	mu    sync.Mutex
	since time.Time
	tools map[string]*toolStat
}

func newToolStats() *toolStats {
	return &toolStats{since: time.Now(), tools: map[string]*toolStat{}} //nolint:exhaustruct
}

func (s *toolStats) record(rec *callRecord) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.tools[rec.tool]
	if st == nil {
		st = &toolStat{} //nolint:exhaustruct
		s.tools[rec.tool] = st
	}

	st.calls++
	st.sumBytes += int64(rec.bytes)
	st.maxBytes = max(st.maxBytes, rec.bytes)

	if rec.shaped {
		st.shaped++
	}

	if len(rec.steps) > 0 {
		st.shrunk++
	}

	if rec.refused {
		st.refused++
	}

	i := 0
	for i < len(sizeBuckets) && rec.bytes > sizeBuckets[i] {
		i++
	}

	st.buckets[i]++
}

type toolStatView struct {
	Calls    int            `json:"calls"`
	Shaped   int            `json:"shaped"`
	Shrunk   int            `json:"shrunk"`
	Refused  int            `json:"refused"`
	AvgBytes int64          `json:"avg_bytes"`
	MaxBytes int            `json:"max_bytes"`
	Sizes    map[string]int `json:"sizes"`
}

type toolStatsView struct {
	Since  time.Time               `json:"since"`
	Budget int                     `json:"budget_bytes"`
	Tools  map[string]toolStatView `json:"tools"`
}

func (s *toolStats) snapshot(budget int) toolStatsView {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := toolStatsView{Since: s.since, Budget: budget, Tools: make(map[string]toolStatView, len(s.tools))}

	for name, st := range s.tools {
		sizes := make(map[string]int, len(st.buckets))
		for i, n := range st.buckets {
			sizes[sizeBucketNames[i]] = n
		}

		out.Tools[name] = toolStatView{
			Calls:    st.calls,
			Shaped:   st.shaped,
			Shrunk:   st.shrunk,
			Refused:  st.refused,
			AvgBytes: st.sumBytes / int64(st.calls),
			MaxBytes: st.maxBytes,
			Sizes:    sizes,
		}
	}

	return out
}

func registerStatsResource(s *mcp.Server, stats *toolStats, budget int) {
	s.AddResource(&mcp.Resource{ //nolint:exhaustruct
		URI:   toolStatsURI,
		Name:  "tool-stats",
		Title: "Tool result sizes",
		Description: "Operator telemetry, not diagnostics: per-tool call count, result size (average, maximum, " +
			"histogram), how often a result was narrowed or refused, for this identity since the server started.",
		MIMEType: "application/json",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		b, err := json.Marshal(stats.snapshot(budgetFor(true, budget)))
		if err != nil {
			return nil, fmt.Errorf("mcp: encode tool stats: %w", err)
		}

		return &mcp.ReadResourceResult{ //nolint:exhaustruct
			Contents: []*mcp.ResourceContents{
				{URI: toolStatsURI, MIMEType: "application/json", Text: string(b)}, //nolint:exhaustruct
			},
		}, nil
	})
}
