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

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
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
		{"0.1.42-alpha", ">=0.1.42", true}, {"0.1.99", "^0.1.42", true}, {"0.2.0", "^0.1.42", false}, {"0.0.3", "^0.0.3", true}, {"0.0.4", "^0.0.3", false}, {"bad", ">=0.1.0", false},
	}
	for _, tc := range cases {
		if got := Satisfies(tc.version, tc.constraint); got != tc.want {
			t.Fatalf("Satisfies(%q,%q)=%v want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
}

func writePackagePolicy(t *testing.T, m *Manager, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(m.Workspace, ".takt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Workspace, ".takt", "package-policy.yaml"), []byte("apiVersion: takt/v1alpha1\nkind: PackagePolicy\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSourceAllowlistUsesPathBoundary(t *testing.T) {
	m := newTestManager(t)
	parent := t.TempDir()
	allowed := filepath.Join(parent, "packages")
	evil := filepath.Join(parent, "packages-evil")
	writeTestPackage(t, evil, "evil-package", "1.0.0", "project", "")
	writePackagePolicy(t, m, fmt.Sprintf("allowed_sources:\n  - 'local:%s'\n", allowed))
	if _, err := m.Install(context.Background(), evil, InstallOptions{Scope: "project"}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected path-boundary rejection, got %v", err)
	}
}

func TestSignaturePolicyNegativeCases(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "private.key")
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("missing", func(t *testing.T) {
		m := newTestManager(t)
		src := t.TempDir()
		writeTestPackage(t, src, "missing-signature", "1.0.0", "corporate", "")
		writePackagePolicy(t, m, fmt.Sprintf("require_signature_scopes: [corporate]\ntrusted_keys:\n  corp: %s\n", base64.StdEncoding.EncodeToString(pub)))
		if _, err := m.Install(context.Background(), src, InstallOptions{Scope: "corporate"}); err == nil || !strings.Contains(err.Error(), "requires a signature") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("untrusted", func(t *testing.T) {
		m := newTestManager(t)
		src := t.TempDir()
		writeTestPackage(t, src, "untrusted-signature", "1.0.0", "corporate", "")
		if err := SignPackage(src, "other", keyFile); err != nil {
			t.Fatal(err)
		}
		writePackagePolicy(t, m, fmt.Sprintf("require_signature_scopes: [corporate]\ntrusted_keys:\n  corp: %s\n", base64.StdEncoding.EncodeToString(pub)))
		if _, err := m.Install(context.Background(), src, InstallOptions{Scope: "corporate"}); err == nil || !strings.Contains(err.Error(), "not trusted") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("tampered", func(t *testing.T) {
		m := newTestManager(t)
		src := t.TempDir()
		writeTestPackage(t, src, "tampered-signature", "1.0.0", "corporate", "")
		if err := SignPackage(src, "corp", keyFile); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "workflow.yaml"), append(mustReadTestFile(t, filepath.Join(src, "workflow.yaml")), []byte("\n# tampered\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		writePackagePolicy(t, m, fmt.Sprintf("require_signature_scopes: [corporate]\ntrusted_keys:\n  corp: %s\n", base64.StdEncoding.EncodeToString(pub)))
		if _, err := m.Install(context.Background(), src, InstallOptions{Scope: "corporate"}); err == nil || !strings.Contains(err.Error(), "digest does not match") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestInstallRejectsIncompatibleTaktRequirement(t *testing.T) {
	m := newTestManager(t)
	src := t.TempDir()
	writeTestPackage(t, src, "future-package", "1.0.0", "project", "requirements:\n  takt: '>=9.0.0'\n")
	if _, err := m.Install(context.Background(), src, InstallOptions{Scope: "project"}); err == nil || !strings.Contains(err.Error(), "requires Takt") {
		t.Fatalf("err=%v", err)
	}
}

func TestLocalSyncRejectsSourceDrift(t *testing.T) {
	m := newTestManager(t)
	src := t.TempDir()
	writeTestPackage(t, src, "local-drift", "1.0.0", "project", "")
	entry, err := m.Install(context.Background(), src, InstallOptions{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.installDir(*entry), "workflow.yaml"), []byte("corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestPackage(t, src, "local-drift", "1.0.0", "project", "")
	if err := os.WriteFile(filepath.Join(src, "workflow.yaml"), append(mustReadTestFile(t, filepath.Join(src, "workflow.yaml")), []byte("\n# source drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(context.Background()); err == nil || !strings.Contains(err.Error(), "no longer reproduces locked package") {
		t.Fatalf("err=%v", err)
	}
}

func TestInstallRollbackWhenLockWriteFails(t *testing.T) {
	m := newTestManager(t)
	src := t.TempDir()
	writeTestPackage(t, src, "rollback-package", "1.0.0", "project", "")
	entry, err := m.Install(context.Background(), src, InstallOptions{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := TreeChecksum(m.installDir(*entry))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "workflow.yaml"), append(mustReadTestFile(t, filepath.Join(src, "workflow.yaml")), []byte("\n# new content\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	m.saveLock = func(string, Lock) error { return fmt.Errorf("injected lock failure") }
	if _, err := m.Update(context.Background(), "rollback-package", "project", ""); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("err=%v", err)
	}
	got, err := TreeChecksum(m.installDir(*entry))
	if err != nil {
		t.Fatal(err)
	}
	if got != old {
		t.Fatalf("installed package changed despite rollback: old=%s got=%s", old, got)
	}
}

func TestDependencyScopeDisambiguatesSameName(t *testing.T) {
	m := newTestManager(t)
	global := t.TempDir()
	project := t.TempDir()
	consumer := t.TempDir()
	writeTestPackage(t, global, "shared-package", "1.2.0", "global", "")
	writeTestPackage(t, project, "shared-package", "1.2.0", "project", "")
	if _, err := m.Install(context.Background(), global, InstallOptions{Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(context.Background(), project, InstallOptions{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	writeTestPackage(t, consumer, "consumer-ambiguous", "1.0.0", "project", "dependencies:\n  - name: shared-package\n    version: ^1.2.0\n")
	if _, err := m.Install(context.Background(), consumer, InstallOptions{Scope: "project"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
	writeTestPackage(t, consumer, "consumer-ambiguous", "1.0.0", "project", "dependencies:\n  - name: shared-package\n    scope: global\n    version: ^1.2.0\n")
	if _, err := m.Install(context.Background(), consumer, InstallOptions{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRefusesToBreakDependent(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	consumer := t.TempDir()
	writeTestPackage(t, base, "base-update", "1.2.0", "project", "")
	if _, err := m.Install(context.Background(), base, InstallOptions{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	writeTestPackage(t, consumer, "consumer-update", "1.0.0", "project", "dependencies:\n  - name: base-update\n    scope: project\n    version: ^1.2.0\n")
	if _, err := m.Install(context.Background(), consumer, InstallOptions{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	writeTestPackage(t, base, "base-update", "2.0.0", "project", "")
	if _, err := m.Update(context.Background(), "base-update", "project", ""); err == nil || !strings.Contains(err.Error(), "consumer-update") {
		t.Fatalf("err=%v", err)
	}
}

func TestGitSyncUsesLockedCommit(t *testing.T) {
	m := newTestManager(t)
	repo := t.TempDir()
	writeTestPackage(t, repo, "git-sync", "1.0.0", "project", "")
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "v1")
	entry, err := m.Install(context.Background(), "git+"+repo, InstallOptions{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	lockedCommit := entry.Source.Commit
	writeTestPackage(t, repo, "git-sync", "2.0.0", "project", "")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "v2")
	if err := os.WriteFile(filepath.Join(m.installDir(*entry), "workflow.yaml"), []byte("corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := m.findLocked("git-sync", "project")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source.Commit != lockedCommit || got.Version != "1.0.0" {
		t.Fatalf("synced to wrong source: %+v", got)
	}
}
