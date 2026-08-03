package definition

import (
	"os"
	"path/filepath"
	"testing"

	"takt/internal/command"
	"takt/internal/spec"
)

func TestCommandFingerprintChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cmdDir, "build.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "test"}, Nodes: []spec.Node{{ID: "build", Command: "build"}}}
	cfg := &spec.Config{}
	resolver := command.Resolver{Dirs: []string{cmdDir}}
	before, err := Compute(wf, cfg, "<workflow>", "<config>", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Compute(wf, cfg, "<workflow>", "<config>", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if before.Commands == after.Commands {
		t.Fatal("command fingerprint did not change")
	}
	if err := Verify(before, after); err == nil {
		t.Fatal("expected definition change")
	}
}
