package packagedist

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestPackage(t *testing.T, root, name, version, scope, extra string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: %s
  version: %s
  scope: %s
%sblocks:
  test-block:
    workflow: workflow.yaml
    output_paths: [summary]
`, name, version, scope, extra)
	workflow := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: package-test
nodes:
  - id: result
    prompt: Return JSON.
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`
	if err := os.WriteFile(filepath.Join(root, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflow.yaml"), []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.Home = t.TempDir()
	return m
}

func TestInstallDoctorSyncAndUninstallLocalPackage(t *testing.T) {
	m := newTestManager(t)
	src := t.TempDir()
	writeTestPackage(t, src, "sample-package", "1.0.0", "project", "")
	entry, err := m.Install(context.Background(), src, InstallOptions{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "sample-package" || !strings.HasPrefix(entry.Checksum, "sha256:") {
		t.Fatalf("entry=%+v", entry)
	}
	paths, err := InstalledManifestPaths(m.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths=%v", paths)
	}
	installed := filepath.Join(filepath.Dir(paths[0]), "workflow.yaml")
	if err := os.WriteFile(installed, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstalledManifestPaths(m.Workspace); err == nil {
		t.Fatal("corrupted locked package was accepted by profile resolution path")
	}
	report, err := m.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "error" {
		t.Fatalf("report=%+v", report)
	}
	report, err = m.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ready" {
		t.Fatalf("report=%+v", report)
	}
	if err := m.Uninstall("sample-package", "project"); err != nil {
		t.Fatal(err)
	}
	values, _ := m.List()
	if len(values) != 0 {
		t.Fatalf("packages=%v", values)
	}
}

func TestPackageDependencyMustBeSatisfied(t *testing.T) {
	m := newTestManager(t)
	src := t.TempDir()
	writeTestPackage(t, src, "consumer", "1.0.0", "project", "dependencies:\n  - name: base-package\n    version: ^1.2.0\n")
	if _, err := m.Install(context.Background(), src, InstallOptions{Scope: "project"}); err == nil || !strings.Contains(err.Error(), "base-package") {
		t.Fatalf("expected dependency error, got %v", err)
	}
}

func TestGitUpdatePinsCommitAndVersion(t *testing.T) {
	m := newTestManager(t)
	repo := t.TempDir()
	writeTestPackage(t, repo, "git-package", "1.0.0", "project", "")
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "v1")
	first, err := m.Install(context.Background(), "git+"+repo, InstallOptions{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Source.Commit == "" {
		t.Fatal("commit not pinned")
	}
	writeTestPackage(t, repo, "git-package", "1.1.0", "project", "")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "v2")
	second, err := m.Update(context.Background(), "git-package", "project", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != "1.1.0" || second.Source.Commit == first.Source.Commit {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestCorporateSignaturePolicy(t *testing.T) {
	m := newTestManager(t)
	src := t.TempDir()
	writeTestPackage(t, src, "signed-package", "1.0.0", "corporate", "")
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "private.key")
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SignPackage(src, "corp-key", keyFile); err != nil {
		t.Fatal(err)
	}
	policy := fmt.Sprintf("apiVersion: takt/v1alpha1\nkind: PackagePolicy\nrequire_signature_scopes: [corporate]\ntrusted_keys:\n  corp-key: %s\n", base64.StdEncoding.EncodeToString(pub))
	if err := os.MkdirAll(filepath.Join(m.Workspace, ".takt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Workspace, ".takt", "package-policy.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := m.Install(context.Background(), src, InstallOptions{Scope: "corporate"})
	if err != nil {
		t.Fatal(err)
	}
	if !entry.SignatureVerified || entry.SignatureKeyID != "corp-key" {
		t.Fatalf("entry=%+v", entry)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestUninstallRefusesToBreakDependentPackage(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	consumer := t.TempDir()
	writeTestPackage(t, base, "base-package", "1.2.0", "project", "")
	if _, err := m.Install(context.Background(), base, InstallOptions{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	writeTestPackage(t, consumer, "consumer-package", "1.0.0", "project", "dependencies:\n  - name: base-package\n    version: ^1.2.0\n")
	if _, err := m.Install(context.Background(), consumer, InstallOptions{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall("base-package", "project"); err == nil || !strings.Contains(err.Error(), "consumer-package") {
		t.Fatalf("expected dependent uninstall rejection, got %v", err)
	}
}

func TestVersionConstraints(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		{"1.2.3", "1.2.3", true}, {"1.2.4", "1.2.3", false},
		{"1.3.0", ">=1.2.3", true}, {"1.2.2", ">=1.2.3", false},
		{"1.9.0", "^1.2.3", true}, {"2.0.0", "^1.2.3", false},
		{"0.1.42-alpha", ">=0.1.42", true}, {"bad", ">=0.1.0", false},
	}
	for _, tc := range cases {
		if got := Satisfies(tc.version, tc.constraint); got != tc.want {
			t.Fatalf("Satisfies(%q,%q)=%v want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
}
