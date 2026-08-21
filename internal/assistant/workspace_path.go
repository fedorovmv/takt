package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkspacePathGuardJS is shared by native assistant pre-tool hooks. It
// resolves dangling symlinks fail-closed instead of treating them as missing
// path suffixes.
const WorkspacePathGuardJS = `import { lstatSync, realpathSync } from "node:fs"
import { basename, dirname, resolve } from "node:path"

function canonical(value) {
  let current = resolve(value)
  const suffix = []
  for (;;) {
    let stat
    try {
      stat = lstatSync(current)
    } catch (error) {
      if (error?.code !== "ENOENT") throw error
    }
    if (stat) {
      let result = realpathSync(current)
      for (let i = suffix.length - 1; i >= 0; i--) result = resolve(result, suffix[i])
      return result
    }
    const parent = dirname(current)
    if (parent === current) throw new Error("no existing parent")
    suffix.push(basename(current))
    current = parent
  }
}

function within(root, value) {
  return value === root || value.startsWith(root.endsWith("/") ? root : root + "/")
}
`

// ValidateToolPath is the pre-execution path boundary for assistant file
// mutation tools. It is shared by local process control and external workers.
func ValidateToolPath(tool string, raw json.RawMessage, workspace, artifacts string) error {
	if tool != "write" && tool != "edit" && tool != "patch" {
		return nil
	}
	if len(raw) == 0 {
		return fmt.Errorf("assistant %s request omitted path input", tool)
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("assistant %s path input is invalid: %w", tool, err)
	}
	if strings.TrimSpace(input.Path) == "" {
		return fmt.Errorf("assistant %s request omitted path", tool)
	}
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("execution workspace is invalid: path is empty")
	}
	workspace, err := resolveWorkspacePath(workspace)
	if err != nil {
		return fmt.Errorf("execution workspace is invalid: %w", err)
	}
	if strings.TrimSpace(artifacts) != "" {
		artifacts, err = resolveWorkspacePath(artifacts)
		if err != nil {
			return fmt.Errorf("Run artifacts path is invalid: %w", err)
		}
	}
	path := input.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	path, err = resolveWorkspacePath(path)
	if err != nil {
		return fmt.Errorf("assistant %s path is invalid: %w", tool, err)
	}
	if withinPath(workspace, path) || artifacts != "" && withinPath(artifacts, path) {
		return nil
	}
	return fmt.Errorf("assistant %s path %q is outside execution workspace", tool, input.Path)
}

func resolveWorkspacePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing := filepath.Clean(abs)
	var suffix []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			resolved, err := filepath.EvalSymlinks(existing)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				component := filepath.Join(resolved, suffix[i])
				if _, lstatErr := os.Lstat(component); lstatErr == nil {
					resolved, err = filepath.EvalSymlinks(component)
					if err != nil {
						return "", err
					}
					continue
				} else if !os.IsNotExist(lstatErr) {
					return "", lstatErr
				}
				resolved = component
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("no existing parent for %q", path)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
}

func withinPath(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
