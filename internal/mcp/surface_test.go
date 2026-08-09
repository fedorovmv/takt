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
	server := NewWithSurface(Dependencies{}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{}, "")
	if server.surface != SurfaceAgent {
		t.Fatalf("NewWithSurface empty=%q", server.surface)
	}
	legacy := New(Dependencies{}, bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{})
	if legacy.surface != SurfaceAll {
		t.Fatalf("New compatibility surface=%q", legacy.surface)
	}
}

func TestSurfaceToolCountsRemainExplicitContracts(t *testing.T) {
	want := map[Surface]int{SurfaceAgent: 5, SurfaceHost: 7, SurfaceWorker: 13, SurfaceOperator: 29, SurfaceAll: 54}
	for surface, count := range want {
		if got := len(tools(surface)); got != count {
			t.Fatalf("surface %s tools=%d want=%d", surface, got, count)
		}
	}
}
