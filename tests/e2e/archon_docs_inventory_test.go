package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchonCurrentDocumentationInventory(t *testing.T) {
	paths := []string{filepath.Join(repoRoot, "README.md")}
	for _, root := range []string{filepath.Join(repoRoot, "examples"), filepath.Join(repoRoot, "skills", "takt")} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && filepath.Ext(path) == ".md" {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range markdownCodeBlocks(string(raw)) {
			if !strings.Contains(block, "nodes:") {
				continue
			}
			for _, forbidden := range []string{"kind: Workflow", "apiVersion: takt/v1alpha1", "${input}", "${nodes.", "${fanout.", "$USER_MESSAGE"} {
				if strings.Contains(block, forbidden) {
					t.Errorf("%s contains legacy Workflow dialect %q in an executable snippet", path, forbidden)
				}
			}
		}
	}
}

func markdownCodeBlocks(source string) []string {
	var blocks []string
	var current strings.Builder
	inFence := false
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inFence {
				blocks = append(blocks, current.String())
				current.Reset()
			}
			inFence = !inFence
			continue
		}
		if inFence {
			current.WriteString(line)
			current.WriteByte('\n')
		}
	}
	return blocks
}
