package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"takt/internal/testsupport/appfixture"
)

func TestCanonicalOperationsAreRegisteredExactlyOnce(t *testing.T) {
	services, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := New(Dependencies{Core: services.Core, Dynamic: services.Dynamic, Blocks: services.Extensions.Blocks, Notifications: services.Extensions.Notifications})
	seenID, seenTool := map[string]bool{}, map[string]bool{}
	for _, descriptor := range CanonicalOperations() {
		if descriptor.ID == "" || descriptor.Title == "" || descriptor.Description == "" || descriptor.InputSchema == nil || descriptor.Stage == "" {
			t.Fatalf("incomplete descriptor: %#v", descriptor)
		}
		if seenID[descriptor.ID] || (descriptor.MCPTool != "" && seenTool[descriptor.MCPTool]) {
			t.Fatalf("duplicate descriptor: %#v", descriptor)
		}
		seenID[descriptor.ID] = true
		if descriptor.MCPTool != "" {
			seenTool[descriptor.MCPTool] = true
		}
		registered, ok := registry.operations[descriptor.ID]
		if !ok {
			t.Errorf("canonical operation %q has no handler", descriptor.ID)
			continue
		}
		if registered.requestType == nil {
			t.Errorf("canonical operation %q has no typed request binding", descriptor.ID)
		}
		if descriptor.MCPTool != "" {
			if got, ok := CanonicalOperationForMCP(descriptor.MCPTool); !ok || got != descriptor.ID {
				t.Errorf("tool %q => %q, %v", descriptor.MCPTool, got, ok)
			}
		}
	}
}

func TestGeneratedOperationDocsMatchDescriptors(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "71-canonical-operation-contracts.generated.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), RenderOperationDocs(); got != want {
		t.Fatal("generated canonical operation documentation is stale; regenerate docs/71-canonical-operation-contracts.generated.md from appapi descriptors")
	}
}

func TestOperationSchemasMatchTypedRequestFields(t *testing.T) {
	services, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := New(Dependencies{Core: services.Core, Dynamic: services.Dynamic, Blocks: services.Extensions.Blocks, Notifications: services.Extensions.Notifications})
	for _, descriptor := range CanonicalOperations() {
		operation := registry.operations[descriptor.ID]
		if err := validateTypedRequestContract(descriptor, operation.requestType); err != nil {
			t.Error(err)
		}
	}
}

func TestOperationDescriptorSnapshotsDoNotMutateCanonicalState(t *testing.T) {
	first, ok := Descriptor("run.get")
	if !ok {
		t.Fatal("run.get descriptor missing")
	}
	first.InputSchema["type"] = "array"
	first.Annotations["readOnlyHint"] = false

	second, _ := Descriptor("run.get")
	if second.InputSchema["type"] != "object" {
		t.Fatalf("descriptor input schema was mutated through returned snapshot: %#v", second.InputSchema)
	}
	if second.Annotations["readOnlyHint"] != true {
		t.Fatalf("descriptor annotations were mutated through returned snapshot: %#v", second.Annotations)
	}
}

func TestRunAssessmentDescriptorIsCanonical(t *testing.T) {
	descriptor, ok := Descriptor("run.assessment")
	if !ok || descriptor.MCPTool != "takt.run.assessment" || descriptor.Stage != StageStable {
		t.Fatalf("descriptor = %#v, ok=%v", descriptor, ok)
	}
	properties := descriptor.InputSchema["properties"].(map[string]any)
	if properties["run_id"] == nil || properties["role"] == nil || properties["include_stale"] == nil {
		t.Fatalf("input schema = %#v", descriptor.InputSchema)
	}
}
