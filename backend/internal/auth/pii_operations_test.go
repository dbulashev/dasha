package auth

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// piiOperations is written by hand: the audit log documents the operationIds
// of doc/swagger.yaml, which the generated code no longer carries.
func TestPIIOperationsMatchSpec(t *testing.T) {
	t.Parallel()

	spec, err := openapi3.NewLoader().LoadFromFile("../../../doc/swagger.yaml")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{}

	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, PIICatalogPathPrefix) {
			continue
		}

		ops := item.Operations()
		if len(ops) != 1 {
			t.Fatalf("%s has %d operations; piiOperations is keyed by path alone", path, len(ops))
		}

		for _, op := range ops {
			want[path] = op.OperationID
		}
	}

	if len(want) != len(piiOperations) {
		t.Fatalf("piiOperations = %v, spec = %v", piiOperations, want)
	}

	for path, id := range want {
		if piiOperations[path] != id {
			t.Errorf("%s: piiOperations = %q, spec = %q", path, piiOperations[path], id)
		}
	}
}
