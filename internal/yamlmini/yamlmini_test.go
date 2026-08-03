package yamlmini

import (
	"takt/internal/spec"
	"testing"
)

func TestUnmarshalWorkflowShape(t *testing.T) {
	src := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: test
nodes:
  - id: a
    bash: echo ok
  - id: b
    depends_on: [a]
    approval:
      message: Continue?
      capture_response: true
`
	var v spec.Workflow
	if err := Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if v.Metadata.Name != "test" || len(v.Nodes) != 2 || v.Nodes[1].ID != "b" {
		t.Fatalf("unexpected decode: %+v", v)
	}
}
