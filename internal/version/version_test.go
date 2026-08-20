package version

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValueMatchesRootVersion(t *testing.T) {
	const expected = "0.1.64-alpha"
	if Value != expected {
		t.Fatalf("runtime version = %q, want %q", Value, expected)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(string(data)); Value != want {
		t.Fatalf("runtime version = %q, root VERSION = %q", Value, want)
	}
	security, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "SECURITY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(security), "Takt `v"+Value+"`") {
		t.Fatalf("SECURITY.md does not declare current version %q", Value)
	}
}
