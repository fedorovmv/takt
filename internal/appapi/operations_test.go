package appapi

import (
	"takt/internal/testsupport/appfixture"
	"testing"
)

func TestCanonicalOperationsAreRegisteredExactlyOnce(t *testing.T) {
	services, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry := New(Dependencies{Core: services.Core, Dynamic: services.Dynamic, Blocks: services.Extensions.Blocks, Notifications: services.Extensions.Notifications})
	seenID, seenTool := map[string]bool{}, map[string]bool{}
	for _, descriptor := range CanonicalOperations() {
		if descriptor.ID == "" || descriptor.MCPTool == "" {
			t.Fatalf("empty descriptor: %#v", descriptor)
		}
		if seenID[descriptor.ID] || seenTool[descriptor.MCPTool] {
			t.Fatalf("duplicate descriptor: %#v", descriptor)
		}
		seenID[descriptor.ID], seenTool[descriptor.MCPTool] = true, true
		if _, ok := registry.handlers[descriptor.ID]; !ok {
			t.Errorf("canonical operation %q has no handler", descriptor.ID)
		}
		if got, ok := CanonicalOperationForMCP(descriptor.MCPTool); !ok || got != descriptor.ID {
			t.Errorf("tool %q => %q, %v", descriptor.MCPTool, got, ok)
		}
	}
}
