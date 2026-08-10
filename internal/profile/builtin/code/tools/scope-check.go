package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type input struct {
	Repository         string   `json:"repository"`
	PlanPath           string   `json:"plan_path"`
	BaseBranch         string   `json:"base_branch"`
	DraftPR            bool     `json:"draft_pr"`
	ValidationCommands []string `json:"validation_commands"`
	AllowedPath        []string `json:"allowed_paths"`
}

type report struct {
	Status         string   `json:"status"`
	BaseCommit     string   `json:"base_commit,omitempty"`
	ChangedFiles   []string `json:"changed_files"`
	OutsideAllowed []string `json:"outside_allowed"`
}

type scopeDriftError struct{}

func (scopeDriftError) Error() string { return "changes outside allowed_paths" }

func main() {
	args := os.Args[1:]
	result, err := execute(args)
	if result.Status != "" {
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			fail(encodeErr)
		}
		encoded = append(encoded, '\n')
		if writeErr := os.WriteFile(args[1], encoded, 0o644); writeErr != nil {
			fail(fmt.Errorf("write scope report: %w", writeErr))
		}
		if _, writeErr := os.Stdout.Write(encoded); writeErr != nil {
			fail(writeErr)
		}
	}
	if err == nil {
		return
	}
	var drift scopeDriftError
	if errors.As(err, &drift) {
		os.Exit(3)
	}
	fail(err)
}

func execute(args []string) (report, error) {
	if len(args) != 2 || args[1] == "" {
		return report{}, fmt.Errorf("expected JSON input and output path arguments")
	}
	var request input
	decoder := json.NewDecoder(strings.NewReader(args[0]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return report{}, fmt.Errorf("decode input: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return report{}, fmt.Errorf("decode input: trailing JSON value")
	}
	if request.BaseBranch == "" {
		return report{}, fmt.Errorf("base_branch is required")
	}
	if len(request.AllowedPath) == 0 {
		return report{}, fmt.Errorf("allowed_paths must not be empty")
	}
	for _, pathspec := range request.AllowedPath {
		if err := validatePathspec(pathspec); err != nil {
			return report{}, err
		}
	}

	workspace := os.Getenv("TAKT_WORKSPACE")
	if workspace == "" {
		workspace = "."
	}
	baseBytes, err := gitOutput(workspace, "merge-base", "HEAD", request.BaseBranch)
	if err != nil {
		return report{}, fmt.Errorf("resolve base branch: %w", err)
	}
	baseCommit := strings.TrimSpace(string(baseBytes))
	if baseCommit == "" {
		return report{}, fmt.Errorf("git merge-base returned an empty commit")
	}

	changed, err := changedPaths(workspace, baseCommit)
	if err != nil {
		return report{}, err
	}
	matched, err := matchedPaths(workspace, baseCommit, request.AllowedPath)
	if err != nil {
		return report{}, err
	}
	outside := difference(changed, matched)
	result := report{Status: "ready", BaseCommit: baseCommit, ChangedFiles: changed, OutsideAllowed: outside}
	if len(outside) != 0 {
		result.Status = "failed"
		return result, scopeDriftError{}
	}
	return result, nil
}

func validatePathspec(pathspec string) error {
	if pathspec == "" {
		return fmt.Errorf("invalid allowed path: empty pathspec")
	}
	if strings.HasPrefix(pathspec, ":") {
		return fmt.Errorf("invalid allowed path %q: Git magic is not allowed", pathspec)
	}
	windowsVolume := len(pathspec) >= 2 && pathspec[1] == ':' && ((pathspec[0] >= 'a' && pathspec[0] <= 'z') || (pathspec[0] >= 'A' && pathspec[0] <= 'Z'))
	if filepath.IsAbs(pathspec) || filepath.VolumeName(pathspec) != "" || windowsVolume || strings.HasPrefix(pathspec, "/") || strings.HasPrefix(pathspec, `\\`) {
		return fmt.Errorf("invalid allowed path %q: path must be repository-relative", pathspec)
	}
	for _, segment := range strings.FieldsFunc(pathspec, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("invalid allowed path %q: parent traversal is not allowed", pathspec)
		}
	}
	return nil
}

func gitOutput(workspace string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = workspace
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, message)
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func changedPaths(workspace, baseCommit string) ([]string, error) {
	tracked, err := gitOutput(workspace, "diff", "--name-only", "-z", "--no-renames", baseCommit, "--")
	if err != nil {
		return nil, fmt.Errorf("read tracked changes: %w", err)
	}
	untracked, err := gitOutput(workspace, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("read untracked changes: %w", err)
	}
	return mergePaths(splitNUL(tracked), splitNUL(untracked)), nil
}

func matchedPaths(workspace, baseCommit string, pathspecs []string) ([]string, error) {
	trackedArgs := []string{"diff", "--name-only", "-z", "--no-renames", baseCommit, "--"}
	trackedArgs = append(trackedArgs, pathspecs...)
	tracked, err := gitOutput(workspace, trackedArgs...)
	if err != nil {
		return nil, fmt.Errorf("match tracked changes: %w", err)
	}
	untrackedArgs := []string{"ls-files", "--others", "--exclude-standard", "-z", "--"}
	untrackedArgs = append(untrackedArgs, pathspecs...)
	untracked, err := gitOutput(workspace, untrackedArgs...)
	if err != nil {
		return nil, fmt.Errorf("match untracked changes: %w", err)
	}
	return mergePaths(splitNUL(tracked), splitNUL(untracked)), nil
}

func splitNUL(data []byte) []string {
	parts := bytes.Split(data, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			paths = append(paths, string(part))
		}
	}
	return paths
}

func mergePaths(groups ...[]string) []string {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, path := range group {
			seen[path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func difference(all, subset []string) []string {
	allowed := make(map[string]struct{}, len(subset))
	for _, path := range subset {
		allowed[path] = struct{}{}
	}
	outside := make([]string, 0)
	for _, path := range all {
		if _, ok := allowed[path]; !ok {
			outside = append(outside, path)
		}
	}
	return outside
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
