package main

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceArchiveIsDeterministicAndNeverFollowsSymlinks(t *testing.T) {
	for _, test := range []struct {
		name       string
		linkTarget string
		wantSource bool
	}{
		{name: "regular", wantSource: true},
		{name: "symlink", linkTarget: "main.txt"},
		{name: "broken symlink", linkTarget: "missing.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := evidenceGitWorkspace(t)
			if test.linkTarget != "" {
				if err := os.Symlink(test.linkTarget, filepath.Join(workspace, "link")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if err := os.WriteFile(filepath.Join(workspace, "main.txt"), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			first, second := filepath.Join(t.TempDir(), "first.tar"), filepath.Join(t.TempDir(), "second.tar")
			for _, output := range []string{first, second} {
				if err := collect(evidenceOptions{Workspace: workspace, BaseCommit: "HEAD", Output: output}); err != nil {
					t.Fatal(err)
				}
			}
			left, _ := os.ReadFile(first)
			right, _ := os.ReadFile(second)
			if !bytes.Equal(left, right) {
				t.Fatal("archive is not deterministic")
			}
			entries := readEvidenceTar(t, left)
			for _, required := range []string{"manifest.json", "diff.patch", "repository.bundle"} {
				if len(entries[required]) == 0 {
					t.Fatalf("missing %s: %v", required, entryNames(entries))
				}
			}
			if !strings.Contains(string(entries["diff.patch"]), "changed") {
				t.Fatalf("diff=%q", entries["diff.patch"])
			}
			if test.wantSource {
				if string(entries["source/main.txt"]) != "changed\n" || entries["source-unavailable.txt"] != nil {
					t.Fatalf("entries=%v", entryNames(entries))
				}
			} else {
				if len(entries["source-unavailable.txt"]) == 0 || entries["source/link"] != nil {
					t.Fatalf("entries=%v", entryNames(entries))
				}
			}
		})
	}
}

func TestEvidenceArchiveRejectsBinarySecretWithoutPublishing(t *testing.T) {
	workspace := evidenceGitWorkspace(t)
	t.Setenv("EVAL_EVIDENCE_TOKEN", "secret-value")
	if err := os.WriteFile(filepath.Join(workspace, "binary.bin"), append([]byte{0}, []byte("secret-value")...), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "evidence.tar")
	err := collect(evidenceOptions{Workspace: workspace, BaseCommit: "HEAD", Output: output})
	if err == nil || !strings.Contains(err.Error(), "binary file contains known secret") {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("partial archive published: %v", statErr)
	}
}

func evidenceGitWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Eval"}, {"config", "user.email", "eval@example.test"}, {"add", "main.txt"}, {"commit", "-m", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	return workspace
}

func readEvidenceTar(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	entries := map[string][]byte{}
	reader := tar.NewReader(bytes.NewReader(raw))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatalf("non-regular tar entry: %+v", header)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = data
	}
}

func entryNames(entries map[string][]byte) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	return names
}
