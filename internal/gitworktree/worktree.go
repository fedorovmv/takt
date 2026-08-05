package gitworktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Options struct {
	Base         string
	BranchPrefix string
	AllowDirty   bool
}

type Info struct {
	RepositoryRoot     string
	ControlWorkspace   string
	ExecutionWorkspace string
	Path               string
	Branch             string
	BaseRef            string
	BaseCommit         string
	BaseDirty          bool
}

type Status struct {
	Dirty bool
	Lines []string
}

var unsafeRefChars = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)

func Prepare(ctx context.Context, workspace, runID, workflowName string, options Options) (*Info, error) {
	control, err := canonicalExistingPath(workspace)
	if err != nil {
		return nil, err
	}
	repoRootText, err := gitOutput(ctx, control, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("worktree requires a Git repository: %w", err)
	}
	repoRoot, err := canonicalExistingPath(repoRootText)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	relWorkspace, err := filepath.Rel(repoRoot, control)
	if err != nil || relWorkspace == ".." || strings.HasPrefix(relWorkspace, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("workspace %s is outside repository root %s", control, repoRoot)
	}
	if err := ensureLocalExcludes(ctx, repoRoot); err != nil {
		return nil, err
	}
	status, err := Inspect(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	if status.Dirty && !options.AllowDirty {
		preview := strings.Join(status.Lines, ", ")
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		return nil, fmt.Errorf("base workspace has uncommitted changes; commit/stash them, use --allow-dirty-worktree, or disable isolation: %s", preview)
	}
	base := strings.TrimSpace(options.Base)
	if base == "" {
		base = "HEAD"
	}
	baseCommit, err := gitOutput(ctx, repoRoot, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve worktree base %q: %w", base, err)
	}
	prefix := strings.Trim(strings.TrimSpace(options.BranchPrefix), "/")
	if prefix == "" {
		prefix = "takt"
	}
	workflowSlug := slug(workflowName)
	if workflowSlug == "" {
		workflowSlug = "run"
	}
	branch := prefix + "/" + workflowSlug + "/" + runID
	if _, err := gitOutput(ctx, repoRoot, "check-ref-format", "--branch", branch); err != nil {
		return nil, fmt.Errorf("invalid worktree branch %q: %w", branch, err)
	}
	path := filepath.Join(repoRoot, ".takt", "worktrees", runID)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := gitOutput(ctx, repoRoot, "worktree", "add", "-b", branch, path, baseCommit); err != nil {
		_ = os.RemoveAll(path)
		return nil, fmt.Errorf("create git worktree: %w", err)
	}
	executionWorkspace := path
	if relWorkspace != "." {
		executionWorkspace = filepath.Join(path, relWorkspace)
		if info, err := os.Stat(executionWorkspace); err != nil || !info.IsDir() {
			_ = Remove(ctx, repoRoot, path, true)
			if err == nil {
				err = fmt.Errorf("not a directory")
			}
			return nil, fmt.Errorf("mapped worktree workspace %s: %w", executionWorkspace, err)
		}
	}
	return &Info{
		RepositoryRoot: repoRoot, ControlWorkspace: control, ExecutionWorkspace: executionWorkspace,
		Path: path, Branch: branch, BaseRef: base, BaseCommit: baseCommit, BaseDirty: status.Dirty,
	}, nil
}

func canonicalExistingPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func Inspect(ctx context.Context, path string) (Status, error) {
	out, err := gitOutput(ctx, path, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return Status{}, fmt.Errorf("inspect git worktree %s: %w", path, err)
	}
	lines := splitLines(out)
	return Status{Dirty: len(lines) > 0, Lines: lines}, nil
}

func Remove(ctx context.Context, repositoryRoot, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if _, err := gitOutput(ctx, repositoryRoot, args...); err != nil {
		return fmt.Errorf("remove git worktree %s: %w", path, err)
	}
	return nil
}

func DeleteBranchIfUnchanged(ctx context.Context, repositoryRoot, branch, baseCommit string) (bool, error) {
	if strings.TrimSpace(branch) == "" || strings.TrimSpace(baseCommit) == "" {
		return false, nil
	}
	head, err := gitOutput(ctx, repositoryRoot, "rev-parse", "--verify", branch+"^{commit}")
	if err != nil {
		return false, nil
	}
	if strings.TrimSpace(head) != strings.TrimSpace(baseCommit) {
		return false, nil
	}
	if _, err := gitOutput(ctx, repositoryRoot, "branch", "-D", branch); err != nil {
		return false, fmt.Errorf("delete empty worktree branch %s: %w", branch, err)
	}
	return true, nil
}

func Prune(ctx context.Context, workspace string) error {
	repoRoot, err := gitOutput(ctx, workspace, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	_, err = gitOutput(ctx, repoRoot, "worktree", "prune")
	return err
}

func ensureLocalExcludes(ctx context.Context, repoRoot string) error {
	gitDir, err := gitOutput(ctx, repoRoot, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("resolve git exclude file: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
		return err
	}
	content, err := os.ReadFile(gitDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		lines[strings.TrimSpace(line)] = true
	}
	additions := []string{"/.takt/runs/", "/.takt/worktrees/", "/.takt/evals/"}
	var missing []string
	for _, line := range additions {
		if !lines[line] {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var builder strings.Builder
	builder.Write(content)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		builder.WriteByte('\n')
	}
	builder.WriteString("# Takt local runtime state\n")
	for _, line := range missing {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return os.WriteFile(gitDir, []byte(builder.String()), 0o644)
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
	}
	return text, nil
}

func splitLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = unsafeRefChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-./")
	value = strings.ReplaceAll(value, "//", "/")
	if len(value) > 48 {
		sum := sha256.Sum256([]byte(value))
		value = strings.Trim(value[:36], "-./") + "-" + hex.EncodeToString(sum[:4])
	}
	return value
}
