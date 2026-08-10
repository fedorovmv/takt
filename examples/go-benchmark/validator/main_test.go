package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/format"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type benchmarkCase struct {
	caseFile string
	file     string
	fixed    string
}

var benchmarkCases = map[string]benchmarkCase{
	"cliargs": {
		caseFile: "01-cli-separator.md",
		file:     "internal/cliargs/args.go",
		fixed: `package cliargs

// Inject adds runtime-managed flags to a command invocation.
func Inject(args, managed []string) []string {
	separator := len(args)
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	out := make([]string, 0, len(args)+len(managed))
	out = append(out, args[:separator]...)
	out = append(out, managed...)
	return append(out, args[separator:]...)
}
`,
	},
	"opencodeevents": {
		caseFile: "02-opencode-events.md",
		file:     "internal/opencodeevents/events.go",
		fixed: `package opencodeevents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type event struct {
	Type string ` + "`json:\"type\"`" + `
	Part part   ` + "`json:\"part\"`" + `
	Error struct {
		Data struct {
			Message string ` + "`json:\"message\"`" + `
		} ` + "`json:\"data\"`" + `
	} ` + "`json:\"error\"`" + `
}

type part struct {
	ID     string ` + "`json:\"id\"`" + `
	Type   string ` + "`json:\"type\"`" + `
	Tokens struct {
		Input  int ` + "`json:\"input\"`" + `
		Output int ` + "`json:\"output\"`" + `
	} ` + "`json:\"tokens\"`" + `
}

func Summarize(r io.Reader) (Usage, error) {
	var usage Usage
	seen := map[string]bool{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var current event
		if err := json.Unmarshal(scanner.Bytes(), &current); err != nil {
			return Usage{}, fmt.Errorf("decode event: %w", err)
		}
		if current.Type == "error" {
			message := current.Error.Data.Message
			if message == "" {
				message = "opencode error event"
			}
			return Usage{}, fmt.Errorf("%s", message)
		}
		if current.Type == "step_finish" && !seen[current.Part.ID] {
			seen[current.Part.ID] = true
			usage.InputTokens += current.Part.Tokens.Input
			usage.OutputTokens += current.Part.Tokens.Output
		}
	}
	if err := scanner.Err(); err != nil {
		return Usage{}, err
	}
	return usage, nil
}
`,
	},
	"session": {
		caseFile: "03-exact-resume.md",
		file:     "internal/session/resume.go",
		fixed: `package session

import "fmt"

func Resolve(requested string, observed []string) (string, bool, error) {
	if len(observed) == 0 || observed[0] == "" {
		return "", false, fmt.Errorf("session stream did not expose an ID")
	}
	id := observed[0]
	for _, candidate := range observed[1:] {
		if candidate != id {
			return "", false, fmt.Errorf("session changed from %q to %q", id, candidate)
		}
	}
	if requested != "" && id != requested {
		return "", false, fmt.Errorf("resumed session %q instead of requested %q", id, requested)
	}
	return id, requested != "", nil
}
`,
	},
	"terminal": {
		caseFile: "04-terminal-precedence.md",
		file:     "internal/terminal/classify.go",
		fixed: `package terminal

import (
	"context"
	"errors"
)

type Kind string

const (
	KindExit      Kind = "exit"
	KindOverflow  Kind = "overflow"
	KindTimedOut  Kind = "timed_out"
	KindCancelled Kind = "cancelled"
)

func Classify(ctxErr error, _ int, overflow bool) Kind {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return KindTimedOut
	}
	if errors.Is(ctxErr, context.Canceled) {
		return KindCancelled
	}
	if overflow {
		return KindOverflow
	}
	return KindExit
}
`,
	},
	"runstore": {
		caseFile: "05-persistence-error.md",
		file:     "internal/runstore/service.go",
		fixed: `package runstore

func Complete(repo Repository, state *State) error {
	state.Status = "completed"
	return repo.Commit(state)
}
`,
	},
}

func TestValidateRejectsUnchangedCases(t *testing.T) {
	root := benchmarkRoot(t)
	for name, tc := range benchmarkCases {
		t.Run(name, func(t *testing.T) {
			workspace := copyWorkspace(t, filepath.Join(root, "workspace"))
			result := validate(context.Background(), options{
				caseFile:  filepath.Join(root, "cases", tc.caseFile),
				baseline:  filepath.Join(root, "workspace"),
				workspace: workspace,
			})
			if result.Valid || !hasDiagnostic(result, "SCOPE_INVALID") {
				t.Fatalf("unchanged case result = %#v", result)
			}
		})
	}
}

func TestValidateAcceptsEachReferenceFix(t *testing.T) {
	root := benchmarkRoot(t)
	for name, tc := range benchmarkCases {
		t.Run(name, func(t *testing.T) {
			workspace := copyWorkspace(t, filepath.Join(root, "workspace"))
			if err := os.WriteFile(filepath.Join(workspace, tc.file), formattedSource(t, tc.fixed), 0o644); err != nil {
				t.Fatal(err)
			}
			result := validate(context.Background(), options{
				caseFile:  filepath.Join(root, "cases", tc.caseFile),
				baseline:  filepath.Join(root, "workspace"),
				workspace: workspace,
			})
			if !result.Valid {
				t.Fatalf("reference fix result = %#v", result)
			}
		})
	}
}

func TestValidateRejectsProtectedAndNeighborChanges(t *testing.T) {
	root := benchmarkRoot(t)
	tests := []struct {
		name string
		path string
	}{
		{name: "test file", path: "internal/cliargs/args_test.go"},
		{name: "neighbor package", path: "internal/session/resume.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := copyWorkspace(t, filepath.Join(root, "workspace"))
			fixed := benchmarkCases["cliargs"]
			if err := os.WriteFile(filepath.Join(workspace, fixed.file), formattedSource(t, fixed.fixed), 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, tt.path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\n// out of scope\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			result := validate(context.Background(), options{
				caseFile:  filepath.Join(root, "cases", fixed.caseFile),
				baseline:  filepath.Join(root, "workspace"),
				workspace: workspace,
			})
			if result.Valid || !hasDiagnostic(result, "SCOPE_INVALID") {
				t.Fatalf("out-of-scope result = %#v", result)
			}
		})
	}
}

func TestRunEmitsOneValidationEnvelope(t *testing.T) {
	root := benchmarkRoot(t)
	tc := benchmarkCases["cliargs"]
	workspace := copyWorkspace(t, filepath.Join(root, "workspace"))
	if err := os.WriteFile(filepath.Join(workspace, tc.file), formattedSource(t, tc.fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{
		"--case-file", filepath.Join(root, "cases", tc.caseFile),
		"--baseline", filepath.Join(root, "workspace"),
		"--workspace", workspace,
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	decoder := json.NewDecoder(&stdout)
	var result validationResult
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("second JSON value: value=%#v err=%v", extra, err)
	}
	if result.ProtocolVersion != "takt-validation/v1alpha1" || result.Type != "validation_result" || !result.Valid || len(result.Checks) != 5 {
		t.Fatalf("envelope = %#v", result)
	}
}

func benchmarkRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyWorkspace(t *testing.T, source string) string {
	t.Helper()
	destination := t.TempDir()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func hasDiagnostic(result validationResult, code string) bool {
	for _, item := range result.Diagnostics {
		if item.Code == code {
			return true
		}
	}
	return false
}

func formattedSource(t *testing.T, source string) []byte {
	t.Helper()
	formatted, err := format.Source([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return formatted
}
