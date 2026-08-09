package mcp

import (
	"reflect"
	"testing"

	"takt/internal/appapi"
)

func TestCanonicalToolsAreProjectedFromAppAPIDescriptors(t *testing.T) {
	got := canonicalTools()
	wantDescriptors := make([]appapi.OperationDescriptor, 0)
	for _, descriptor := range appapi.CanonicalOperations() {
		if descriptor.MCPTool != "" {
			wantDescriptors = append(wantDescriptors, descriptor)
		}
	}
	if len(got) != len(wantDescriptors) {
		t.Fatalf("canonical MCP tool count=%d, descriptors=%d", len(got), len(wantDescriptors))
	}
	for i, descriptor := range wantDescriptors {
		tool := got[i]
		if tool.Name != descriptor.MCPTool || tool.Title != descriptor.Title || tool.Description != descriptor.Description {
			t.Fatalf("canonical MCP metadata drift at %s: %#v vs %#v", descriptor.ID, tool, descriptor)
		}
		if !reflect.DeepEqual(tool.InputSchema, descriptor.InputSchema) {
			t.Fatalf("canonical MCP input schema drift at %s", descriptor.ID)
		}
		if !reflect.DeepEqual(tool.Annotations, descriptor.Annotations) {
			t.Fatalf("canonical MCP annotations drift at %s", descriptor.ID)
		}
	}
}
