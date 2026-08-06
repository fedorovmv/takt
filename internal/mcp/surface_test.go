package mcp

import (
	"bytes"
	"testing"
)

func TestEmptySurfaceDefaultsToAgentWhileNewKeepsCompatibilityAll(t *testing.T) {
	parsed, err := ParseSurface("")
	if err != nil {
		t.Fatal(err)
	}
	if parsed != SurfaceAgent {
		t.Fatalf("ParseSurface empty=%q", parsed)
	}
	server := NewWithSurface(nil, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{}, "")
	if server.surface != SurfaceAgent {
		t.Fatalf("NewWithSurface empty=%q", server.surface)
	}
	legacy := New(nil, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{})
	if legacy.surface != SurfaceAll {
		t.Fatalf("New compatibility surface=%q", legacy.surface)
	}
}
