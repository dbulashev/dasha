package mcpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP server forwards the caller's token. A client configured with a token
// from pii_catalog.allowed_tokens would hand the PII catalog to the LLM through
// any tool that reaches /api/pii-catalog/*, so none may exist.
func TestNoPIICatalogAccess(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.Ident:
				if strings.Contains(strings.ToLower(v.Name), "piicatalog") {
					t.Errorf("%s: %s references the PII catalog API", fset.Position(v.Pos()), v.Name)
				}
			case *ast.BasicLit:
				if v.Kind == token.STRING && strings.Contains(strings.ToLower(v.Value), "pii-catalog") {
					t.Errorf("%s: string %s names the PII catalog API", fset.Position(v.Pos()), v.Value)
				}
			}

			return true
		})
	}
}

func TestNoPIICatalogTool(t *testing.T) {
	t.Parallel()

	client, err := NewDashaClient(Config{DashaURL: "http://127.0.0.1:1", Token: "x"}) //nolint:exhaustruct
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := NewMCPServer(client, "test", "en").Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "v0"}, nil).Connect(ctx, ct, nil) //nolint:exhaustruct
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range lt.Tools {
		text := strings.ToLower(tool.Name + " " + tool.Description)
		if strings.Contains(text, "pii") {
			t.Errorf("tool %q mentions the PII catalog", tool.Name)
		}
	}
}
