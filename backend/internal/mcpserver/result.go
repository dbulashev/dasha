package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxResultBytes = 64 << 10
	unshapedResultBytes   = 256 << 10

	// maxShrinkSteps stops a shrink() that never reaches its floor.
	maxShrinkSteps = 32
)

// shapedTools narrow their own result and live under the configured budget; the
// rest keep unshapedResultBytes. A tool joins together with its shapedResult.
var shapedTools = map[string]bool{
	"describe_table":   true,
	"health_trend":     true,
	"list_clusters":    true,
	"plan_insights":    true,
	"plan_regressions": true,
	"query_compare":    true,
	"query_plans":      true,
}

func budgetFor(shaped bool, budget int) int {
	if budget <= 0 {
		budget = defaultMaxResultBytes
	}

	if !shaped && budget < unshapedResultBytes {
		return unshapedResultBytes
	}

	return budget
}

// shapedResult is a tool answer that knows how to narrow itself.
type shapedResult interface {
	// shrink returns a narrower form and the step name; once nothing is left to
	// narrow it returns ok=false and the string names the parameter to reduce.
	shrink() (shapedResult, string, bool)
	note() *shapeNote
}

const (
	shapeDefaultView = "default_view"
	shapeBudget      = "budget"
)

type shapeNote struct {
	Reason string   `json:"reason"`
	Folded string   `json:"folded,omitempty"`
	Total  int      `json:"total,omitempty"`
	Full   string   `json:"full,omitempty"`
	Steps  []string `json:"budget_steps,omitempty"`
}

type toolResultFunc func(payload any, err error) (*mcp.CallToolResult, any, error)

// jsonResult renders a payload as compact JSON text, or an error as an isError
// tool result. A result over the call's budget is narrowed by its own shrink().
func jsonResult(ctx context.Context) toolResultFunc {
	return func(payload any, err error) (*mcp.CallToolResult, any, error) {
		return renderResult(ctx, payload, err)
	}
}

func renderResult(ctx context.Context, payload any, err error) (*mcp.CallToolResult, any, error) {
	rec := recordFrom(ctx)

	if err != nil {
		if errors.Is(err, errNotFound) {
			return rec.explainNotFound(ctx, err.Error()), nil, nil
		}

		return errResult(err.Error()), nil, nil
	}

	budget := rec.budgetOrDefault()
	shaped, _ := payload.(shapedResult)

	var steps []string

	for {
		b, mErr := json.Marshal(payload)
		if mErr != nil {
			return errResult(fmt.Sprintf("mcp: encode result: %v", mErr)), nil, nil
		}

		note, nErr := noteBlock(shaped, steps)
		if nErr != nil {
			return errResult(fmt.Sprintf("mcp: encode result note: %v", nErr)), nil, nil
		}

		size := len(b) + len(note)
		if size <= budget {
			rec.markShaped(note != "", steps)

			return shapedContent(note, b), nil, nil
		}

		hint := ""

		if shaped != nil && len(steps) < maxShrinkSteps {
			next, step, ok := shaped.shrink()
			if ok {
				shaped, payload, steps = next, next, append(steps, step)

				continue
			}

			// at its floor a shaped result gets the unshaped tools' ceiling
			if size <= unshapedResultBytes {
				rec.markShaped(note != "", steps)

				return shapedContent(note, b), nil, nil
			}

			hint = step
		}

		rec.markRefused()

		return oversizedResult(size, budget, hint), nil, nil
	}
}

func noteBlock(shaped shapedResult, steps []string) (string, error) {
	var n shapeNote

	if shaped != nil {
		if p := shaped.note(); p != nil {
			n = *p
		}
	}

	if len(steps) > 0 {
		n.Reason = shapeBudget
		n.Steps = steps
	}

	if n.Reason == "" {
		return "", nil
	}

	b, err := json.Marshal(map[string]shapeNote{"shaped": n})

	return string(b), err
}

func shapedContent(note string, body []byte) *mcp.CallToolResult {
	content := make([]mcp.Content, 0, 2)
	if note != "" {
		content = append(content, &mcp.TextContent{Text: note})
	}

	return &mcp.CallToolResult{
		Content: append(content, &mcp.TextContent{Text: string(body)}),
	}
}

// oversizedResult tells the model the result was too large to return and how to
// get a smaller one, as a structured isError payload it can act on.
func oversizedResult(size, limit int, hint string) *mcp.CallToolResult {
	if hint == "" {
		hint = "narrow the request — target a single database, use a more specific tool, " +
			"a smaller range, limit or page_size — the full result exceeds the response size limit"
	}

	b, _ := json.Marshal(map[string]any{
		"error":      "result too large",
		"bytes":      size,
		"limit":      limit,
		"suggestion": hint,
	})

	return errResult(string(b))
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// section folds one part of a composite result in, recording a per-part error
// rather than failing the whole call (e.g. one replication sub-query failing).
func section(out map[string]any, key string, v any, err error) {
	if err != nil {
		out[key+"_error"] = err.Error()
	} else {
		out[key] = v
	}
}

// sectionsResult renders a composite result, but marks it IsError when EVERY
// section failed (e.g. a permission error on every sub-request) so the model
// does not treat an all-errors payload as usable data.
func sectionsResult[M ~map[string]any](ctx context.Context, out M) (*mcp.CallToolResult, any, error) {
	allFailed := len(out) > 0
	allNotFound := allFailed

	for k, v := range out {
		if !strings.HasSuffix(k, "_error") {
			allFailed = false

			break
		}

		if msg, _ := v.(string); !strings.HasPrefix(msg, errNotFound.Error()) {
			allNotFound = false
		}
	}

	if !allFailed {
		return renderResult(ctx, out, nil)
	}

	b, err := json.Marshal(out)
	if err != nil {
		return errResult(fmt.Sprintf("mcp: encode result: %v", err)), nil, nil
	}

	if allNotFound {
		return recordFrom(ctx).explainNotFound(ctx, string(b)), nil, nil
	}

	return errResult(string(b)), nil, nil
}
