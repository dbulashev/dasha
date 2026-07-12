package mcpserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxResultBytes caps a single tool's JSON result so one call cannot flood the
// model's context window (and the transport). An oversized result is refused
// with a hint to narrow the request, rather than returning truncated — and thus
// invalid — JSON the model cannot parse.
const maxResultBytes = 256 * 1024

// jsonResult renders a payload as compact JSON text, or maps an error to an
// isError tool result the model can read and react to (rather than a protocol
// error it cannot see).
func jsonResult(payload any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return errResult(err.Error()), nil, nil
	}

	b, mErr := json.Marshal(payload)
	if mErr != nil {
		return errResult(fmt.Sprintf("mcp: encode result: %v", mErr)), nil, nil
	}

	if len(b) > maxResultBytes {
		return oversizedResult(len(b)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}

// oversizedResult tells the model the result was too large to return and how to
// get a smaller one, as a structured isError payload it can act on.
func oversizedResult(size int) *mcp.CallToolResult {
	msg := fmt.Sprintf(
		`{"error":"result too large","bytes":%d,"limit":%d,`+
			`"suggestion":"narrow the request — target a single database, use a more specific tool, `+
			`or a smaller range — the full result exceeds the response size limit"}`,
		size, maxResultBytes)

	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
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
func sectionsResult(out map[string]any) (*mcp.CallToolResult, any, error) {
	allFailed := len(out) > 0
	for k := range out {
		if !strings.HasSuffix(k, "_error") {
			allFailed = false

			break
		}
	}

	if allFailed {
		b, err := json.Marshal(out)
		if err != nil {
			return errResult(fmt.Sprintf("mcp: encode result: %v", err)), nil, nil
		}

		return errResult(string(b)), nil, nil
	}

	return jsonResult(out, nil)
}
