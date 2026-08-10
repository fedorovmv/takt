package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const protocolVersion = "takt-validation/v1alpha1"

var allowedPackages = map[string]struct{}{
	"./internal/cliargs":        {},
	"./internal/opencodeevents": {},
	"./internal/session":        {},
	"./internal/terminal":       {},
	"./internal/runstore":       {},
}

type options struct {
	caseFile  string
	baseline  string
	workspace string
}

type validationResult struct {
	ProtocolVersion string                     `json:"protocol_version"`
	Type            string                     `json:"type"`
	Valid           bool                       `json:"valid"`
	Score           int                        `json:"score"`
	Checks          map[string]validationCheck `json:"checks"`
	Diagnostics     []diagnostic               `json:"diagnostics"`
}

type validationCheck struct {
	Passed bool `json:"passed"`
	Score  int  `json:"score"`
	Weight int  `json:"weight"`
}

type diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type fileSnapshot struct {
	data []byte
	mode fs.FileMode
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("takt-go-benchmark-validator", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts options
	flags.StringVar(&opts.caseFile, "case-file", "", "evaluation case file")
	flags.StringVar(&opts.baseline, "baseline", "", "pristine workspace template")
	flags.StringVar(&opts.workspace, "workspace", "", "candidate workspace")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || opts.caseFile == "" || opts.baseline == "" || opts.workspace == "" {
		fmt.Fprintln(stderr, "--case-file, --baseline, and --workspace are required")
		return 2
	}
	result := validate(ctx, opts)
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(stderr, "encode validation result: %v\n", err)
		return 2
	}
	if result.Valid {
		return 0
	}
	return 1
}

func validate(ctx context.Context, opts options) validationResult {
	result := newValidationResult()
	packageName, err := casePackage(opts.caseFile)
	if err != nil {
		return failScope(result, err.Error())
	}
	baseline, err := filepath.Abs(opts.baseline)
	if err != nil {
		return failScope(result, err.Error())
	}
	workspace, err := filepath.Abs(opts.workspace)
	if err != nil {
		return failScope(result, err.Error())
	}
	changed, err := changedFiles(baseline, workspace)
	if err != nil {
		return failScope(result, err.Error())
	}
	if err := validateScope(packageName, changed); err != nil {
		return failScope(result, err.Error())
	}
	passCheck(&result, "scope")

	packageDir := filepath.Join(workspace, filepath.FromSlash(strings.TrimPrefix(packageName, "./")))
	goFiles, err := filepath.Glob(filepath.Join(packageDir, "*.go"))
	if err != nil || len(goFiles) == 0 {
		message := "target package has no Go files"
		if err != nil {
			message = err.Error()
		}
		addFailure(&result, "format", "GOFMT_FAILED", packageName, message)
	} else if passed, output := command(ctx, workspace, "gofmt", append([]string{"-l"}, goFiles...)...); !passed || strings.TrimSpace(output) != "" {
		if strings.TrimSpace(output) == "" {
			output = "gofmt failed"
		}
		addFailure(&result, "format", "GOFMT_FAILED", packageName, output)
	} else {
		passCheck(&result, "format")
	}

	runGoCheck(ctx, &result, workspace, packageName, "test", "GO_TEST_FAILED", "test", "-count=1", packageName)
	runGoCheck(ctx, &result, workspace, packageName, "race", "GO_RACE_FAILED", "test", "-race", "-count=1", packageName)
	runGoCheck(ctx, &result, workspace, packageName, "vet", "GO_VET_FAILED", "vet", packageName)

	result.Valid = true
	weightedScore := 0
	totalWeight := 0
	for _, check := range result.Checks {
		if !check.Passed {
			result.Valid = false
		}
		weightedScore += check.Score * check.Weight
		totalWeight += check.Weight
	}
	if totalWeight > 0 {
		result.Score = weightedScore / totalWeight
	}
	return result
}

func newValidationResult() validationResult {
	return validationResult{
		ProtocolVersion: protocolVersion,
		Type:            "validation_result",
		Checks: map[string]validationCheck{
			"scope":  {Weight: 2},
			"format": {Weight: 1},
			"test":   {Weight: 4},
			"race":   {Weight: 2},
			"vet":    {Weight: 1},
		},
		Diagnostics: []diagnostic{},
	}
}

func casePackage(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read case: %w", err)
	}
	var found []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Benchmark-Package:") {
			found = append(found, strings.TrimSpace(strings.TrimPrefix(line, "Benchmark-Package:")))
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("case must contain exactly one Benchmark-Package header")
	}
	if _, ok := allowedPackages[found[0]]; !ok {
		return "", fmt.Errorf("package %q is not allowlisted", found[0])
	}
	return found[0], nil
}

func changedFiles(baseline, workspace string) ([]string, error) {
	before, err := snapshot(baseline)
	if err != nil {
		return nil, fmt.Errorf("snapshot baseline: %w", err)
	}
	after, err := snapshot(workspace)
	if err != nil {
		return nil, fmt.Errorf("snapshot workspace: %w", err)
	}
	paths := map[string]bool{}
	for path := range before {
		paths[path] = true
	}
	for path := range after {
		paths[path] = true
	}
	var changed []string
	for path := range paths {
		left, leftOK := before[path]
		right, rightOK := after[path]
		if !leftOK || !rightOK || left.mode != right.mode || !bytes.Equal(left.data, right.data) {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func snapshot(root string) (map[string]fileSnapshot, error) {
	out := map[string]fileSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".takt" || strings.HasPrefix(relative, ".takt"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(relative)] = fileSnapshot{data: data, mode: info.Mode().Perm()}
		return nil
	})
	return out, err
}

func validateScope(packageName string, changed []string) error {
	if len(changed) == 0 {
		return fmt.Errorf("no production file changed")
	}
	directory := strings.TrimPrefix(packageName, "./") + "/"
	for _, path := range changed {
		if !strings.HasPrefix(path, directory) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return fmt.Errorf("out-of-scope change: %s", path)
		}
	}
	return nil
}

func runGoCheck(ctx context.Context, result *validationResult, workspace, packageName, check, code string, args ...string) {
	passed, output := command(ctx, workspace, "go", args...)
	if passed {
		passCheck(result, check)
		return
	}
	addFailure(result, check, code, packageName, output)
}

func command(ctx context.Context, directory, binary string, args ...string) (bool, string) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	message := strings.TrimSpace(string(output))
	if err != nil {
		if message != "" {
			message += "\n"
		}
		message += err.Error()
	}
	if len(message) > 8192 {
		message = message[:8192] + "\n[truncated]"
	}
	return err == nil, message
}

func passCheck(result *validationResult, name string) {
	check := result.Checks[name]
	check.Passed = true
	check.Score = 100
	result.Checks[name] = check
}

func addFailure(result *validationResult, check, code, path, message string) {
	if strings.TrimSpace(message) == "" {
		message = code
	}
	result.Diagnostics = append(result.Diagnostics, diagnostic{Code: code, Severity: "error", Path: path, Message: message})
	current := result.Checks[check]
	current.Passed = false
	current.Score = 0
	result.Checks[check] = current
}

func failScope(result validationResult, message string) validationResult {
	addFailure(&result, "scope", "SCOPE_INVALID", "", message)
	return result
}
