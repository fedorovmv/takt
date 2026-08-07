package workspacecatalog

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"}} {
		c := exec.Command("git", args...)
		c.Dir = path
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(path, "README.md"), []byte("x\n"), 0644)
	c := exec.Command("git", "add", ".")
	c.Dir = path
	_ = c.Run()
	c = exec.Command("git", "commit", "-qm", "init")
	c.Dir = path
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}
func TestDiscoverAndManifestValidation(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "api"))
	initRepo(t, filepath.Join(root, "client"))
	c, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Repositories) != 2 {
		t.Fatalf("repos=%+v", c.Repositories)
	}
	os.MkdirAll(filepath.Join(root, ".takt"), 0755)
	os.WriteFile(filepath.Join(root, ".takt", "workspace.yaml"), []byte("apiVersion: takt/v1alpha1\nkind: Workspace\nrepositories:\n  - id: api\n    path: api\n  - id: client\n    path: client\n    depends_on: [api]\n"), 0644)
	c, err = Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := c.Get("client")
	if len(r.DependsOn) != 1 || r.DependsOn[0] != "api" {
		t.Fatalf("client=%+v", r)
	}
}
func TestManifestRejectsEscapeAndCycle(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	initRepo(t, outside)
	os.MkdirAll(filepath.Join(root, ".takt"), 0755)
	os.WriteFile(filepath.Join(root, ".takt", "workspace.yaml"), []byte("apiVersion: takt/v1alpha1\nkind: Workspace\nrepositories:\n  - id: bad\n    path: ../outside\n"), 0644)
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("escape accepted")
	}
	initRepo(t, filepath.Join(root, "a"))
	initRepo(t, filepath.Join(root, "b"))
	os.WriteFile(filepath.Join(root, ".takt", "workspace.yaml"), []byte("apiVersion: takt/v1alpha1\nkind: Workspace\nrepositories:\n  - id: a\n    path: a\n    depends_on: [b]\n  - id: b\n    path: b\n    depends_on: [a]\n"), 0644)
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestManifestRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	initRepo(t, outside)
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".takt"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: takt/v1alpha1\nkind: Workspace\nrepositories:\n  - id: linked\n    path: linked\n"
	if err := os.WriteFile(filepath.Join(root, ".takt", "workspace.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestManifestRequiresExplicitVersionAndKind(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "api"))
	if err := os.MkdirAll(filepath.Join(root, ".takt"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".takt", "workspace.yaml"), []byte("repositories:\n  - id: api\n    path: api\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), root); err == nil {
		t.Fatal("manifest without apiVersion/kind accepted")
	}
}

func TestDiscoverySanitizesRepositoryIDToPlanContract(t *testing.T) {
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "123-client"))
	catalog, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Repositories) != 1 || catalog.Repositories[0].ID != "repo-123-client" {
		t.Fatalf("repositories=%+v", catalog.Repositories)
	}
}
