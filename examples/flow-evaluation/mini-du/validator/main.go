package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"takt/internal/yamlcodec"
)

const validatorProtocol = "takt-evaluation-validator/v1alpha1"
const validationProtocol = "takt-validation/v1alpha1"

var (
	errMissingArtifact    = errors.New("missing artifact")
	errArtifactInspection = errors.New("inspect artifacts")
)

type validatorRequest struct {
	ProtocolVersion string         `json:"protocol_version"`
	Type            string         `json:"type"`
	CaseID          string         `json:"case_id"`
	Repeat          int            `json:"repeat"`
	Workspace       string         `json:"workspace"`
	Baseline        string         `json:"baseline_workspace"`
	ExpectedPath    string         `json:"expected_path"`
	Run             validatorRun   `json:"run"`
	ExternalState   *externalState `json:"external_state,omitempty"`
}
type validatorRun struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ArtifactsDir string `json:"artifacts_dir"`
}
type externalState struct {
	SCMDir string `json:"scm_dir"`
}
type miniDUOracle struct {
	AllowedPaths         []string `json:"allowed_paths"`
	Scenarios            []string `json:"scenarios"`
	RequiredArtifacts    []string `json:"required_artifacts,omitempty"`
	RequirePR            bool     `json:"require_pr,omitempty"`
	RequirePush          bool     `json:"require_push,omitempty"`
	ForbiddenIdentifiers []string `json:"forbidden_identifiers,omitempty"`
	ForbiddenPackages    []string `json:"forbidden_packages,omitempty"`
}
type validationResult struct {
	ProtocolVersion string            `json:"protocol_version"`
	Type            string            `json:"type"`
	Valid           bool              `json:"valid"`
	Diagnostics     []diagnostic      `json:"diagnostics,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}
type diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message,omitempty"`
}

func main() { os.Exit(run(os.Stdin, os.Stdout, os.Stderr)) }

func run(in io.Reader, out, errOut io.Writer) int {
	data, err := io.ReadAll(in)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	req, err := decodeRequest(data)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	result, err := validate(req)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	return 0
}

func decodeRequest(data []byte) (validatorRequest, error) {
	var req validatorRequest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return req, errors.New("request must contain one JSON object")
	}
	if req.ProtocolVersion != validatorProtocol || req.Type != "validation_request" {
		return req, errors.New("unsupported validation request")
	}
	if req.Repeat < 0 || req.CaseID == "" || req.Run.ID == "" || req.Run.Status == "" {
		return req, errors.New("request requires case, repeat, and run")
	}
	for _, item := range []struct{ name, value string }{{"workspace", req.Workspace}, {"baseline_workspace", req.Baseline}, {"expected_path", req.ExpectedPath}} {
		if !filepath.IsAbs(item.value) {
			return req, fmt.Errorf("%s must be absolute", item.name)
		}
	}
	return req, nil
}

func validate(req validatorRequest) (validationResult, error) {
	metadata, err := oracleMetadata()
	if err != nil {
		return validationResult{}, err
	}
	oracle, err := loadOracle(req.ExpectedPath)
	if err != nil {
		return validationResult{}, err
	}
	result := validationResult{ProtocolVersion: validationProtocol, Type: "validation_result", Valid: true, Metadata: metadata}
	if req.Run.Status == "not_started" {
		return result, nil
	}
	if err := productCheck(req, oracle); err != nil {
		if errors.Is(err, errArtifactInspection) {
			return validationResult{}, err
		}
		result.Valid = false
		code := "mini_du_invalid"
		if errors.Is(err, errMissingArtifact) {
			code = "missing_artifact"
		}
		result.Diagnostics = []diagnostic{{Code: code, Severity: "error", Message: err.Error()}}
	}
	return result, nil
}

func loadOracle(file string) (miniDUOracle, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return miniDUOracle{}, err
	}
	var envelope struct {
		Oracle miniDUOracle `json:"oracle"`
	}
	if err := yamlcodec.Unmarshal(data, &envelope); err != nil {
		return miniDUOracle{}, err
	}
	oracle := envelope.Oracle
	if len(oracle.AllowedPaths) == 0 || len(oracle.Scenarios) == 0 {
		return oracle, errors.New("allowed_paths and scenarios are required")
	}
	known := map[string]bool{"empty": true, "nested": true, "multiple": true, "unicode": true, "spaces": true, "symlink": true, "hardlink": true, "summary": true, "kibibytes": true, "humanized": true, "help_short": true, "help_long": true, "double_dash": true, "combined_flags": true, "invalid_option": true, "missing": true, "mixed-missing": true}
	for _, scenario := range oracle.Scenarios {
		if !known[scenario] {
			return oracle, fmt.Errorf("unknown scenario %q", scenario)
		}
	}
	for _, allowed := range oracle.AllowedPaths {
		if allowed == "" || strings.HasPrefix(allowed, "/") || strings.Contains(allowed, "..") {
			return oracle, fmt.Errorf("invalid allowed path %q", allowed)
		}
	}
	return oracle, nil
}

func oracleMetadata() (map[string]string, error) {
	path, err := exec.LookPath("du")
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	signature := duSignature(path)
	return map[string]string{"oracle_path": path, "oracle_sha256": hex.EncodeToString(hash[:]), "oracle_signature": signature}, nil
}
func duSignature(bin string) string {
	for _, args := range [][]string{{"--version"}, {}} {
		out, err := exec.Command(bin, args...).CombinedOutput()
		if line := firstLine(string(out)); line != "" {
			if err != nil {
				return fmt.Sprintf("exit=%d: %s", exitCode(err), line)
			}
			return line
		}
	}
	return "unknown"
}
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func productCheck(req validatorRequest, oracle miniDUOracle) error {
	if err := compareTrees(req.Baseline, req.Workspace, oracle.AllowedPaths); err != nil {
		return err
	}
	if err := requireArtifacts(req.Run.ArtifactsDir, oracle.RequiredArtifacts); err != nil {
		return err
	}
	if oracle.RequirePR && !hasPR(req.ExternalState) {
		return errors.New("missing pull request effect")
	}
	if oracle.RequirePush && !hasPush(req.Workspace) {
		return errors.New("missing push effect")
	}
	if err := rejectDelegation(req.Workspace); err != nil {
		return err
	}
	if err := rejectForbiddenSource(req.Workspace, oracle); err != nil {
		return err
	}
	bin, err := buildCandidate(req.Workspace)
	if err != nil {
		return err
	}
	for _, scenario := range oracle.Scenarios {
		if err := compareScenario(bin, scenario); err != nil {
			return err
		}
	}
	return nil
}

func compareTrees(base, candidate string, allowed []string) error {
	files := map[string][]byte{}
	for _, root := range []struct{ prefix, path string }{{"base", base}, {"candidate", candidate}} {
		err := filepath.WalkDir(root.path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root.path, p)
			rel = filepath.ToSlash(rel)
			if rel == "." || rel == ".git" || strings.HasPrefix(rel, ".git/") || rel == ".takt/runs" || strings.HasPrefix(rel, ".takt/runs/") || rel == ".takt/worktrees" || strings.HasPrefix(rel, ".takt/worktrees/") || rel == ".takt/evals" || strings.HasPrefix(rel, ".takt/evals/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink in workspace %s", rel)
			}
			if d.IsDir() {
				return nil
			}
			data, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			files[root.prefix+"/"+rel] = append([]byte(fmt.Sprintf("%t\x00", d.Type().IsRegular() && d.Type()&0111 != 0)), data...)
			return nil
		})
		if err != nil {
			return err
		}
	}
	for key, value := range files {
		side, rel, _ := strings.Cut(key, "/")
		other := map[string]string{"base": "candidate", "candidate": "base"}[side] + "/" + rel
		if bytes.Equal(value, files[other]) {
			continue
		}
		if allowedPath(rel, allowed) {
			continue
		}
		return fmt.Errorf("forbidden changed path %s", rel)
	}
	return nil
}
func allowedPath(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/**") && strings.HasPrefix(name, strings.TrimSuffix(pattern, "/**")+"/") {
			return true
		}
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}
func requireArtifacts(dir string, names []string) error {
	for _, name := range names {
		found := false
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() && filepath.Base(p) == name {
				found = true
			}
			return nil
		})
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w %s", errMissingArtifact, name)
			}
			return fmt.Errorf("%w: %v", errArtifactInspection, err)
		}
		if !found {
			return fmt.Errorf("%w %s", errMissingArtifact, name)
		}
	}
	return nil
}

func hasPR(external *externalState) bool {
	if external == nil || external.SCMDir == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(external.SCMDir, "calls.log"))
	return err == nil && strings.Contains(string(data), "pr create")
}

func hasPush(workspace string) bool {
	remote, err := exec.Command("git", "-C", workspace, "remote", "get-url", "origin").Output()
	if err != nil {
		return false
	}
	remotePath := strings.TrimSpace(string(remote))
	if !filepath.IsAbs(remotePath) {
		remotePath = filepath.Join(workspace, remotePath)
	}
	branch, err := exec.Command("git", "-C", workspace, "branch", "--show-current").Output()
	if err != nil || len(bytes.TrimSpace(branch)) == 0 {
		return false
	}
	head, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").Output()
	if err != nil {
		return false
	}
	ref, err := exec.Command("git", "--git-dir="+remotePath, "rev-parse", "refs/heads/"+strings.TrimSpace(string(branch))).Output()
	return err == nil && bytes.Equal(bytes.TrimSpace(head), bytes.TrimSpace(ref))
}

func rejectDelegation(root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() && p != root && (d.Name() == ".git" || d.Name() == ".takt") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(p) != ".go" || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		file, e := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if e != nil {
			return e
		}
		for _, imported := range file.Imports {
			if imported.Path.Value == `"os/exec"` {
				return fmt.Errorf("delegation source %s", p)
			}
		}
		bad := false
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.CallExpr:
				if selected, ok := value.Fun.(*ast.SelectorExpr); ok {
					if ident, ok := selected.X.(*ast.Ident); ok && ident.Name == "exec" && (selected.Sel.Name == "Command" || selected.Sel.Name == "CommandContext") {
						bad = true
					}
				}
			case *ast.BasicLit:
				if value.Kind == token.STRING {
					text, err := strconv.Unquote(value.Value)
					if err == nil && (text == "du" || strings.HasSuffix(text, "/du")) {
						bad = true
					}
				}
			}
			return !bad
		})
		if bad {
			return fmt.Errorf("delegation source %s", p)
		}
		return nil
	})
}

func rejectForbiddenSource(root string, oracle miniDUOracle) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == ".takt") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		for _, pkg := range oracle.ForbiddenPackages {
			if strings.HasPrefix(rel, strings.TrimSuffix(pkg, "/")+"/") {
				return fmt.Errorf("forbidden package %s", pkg)
			}
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, identifier := range oracle.ForbiddenIdentifiers {
			if strings.Contains(string(data), identifier) {
				return fmt.Errorf("forbidden identifier %s", identifier)
			}
		}
		return nil
	})
}
func buildCandidate(workspace string) (string, error) {
	out := filepath.Join(os.TempDir(), "mini-du-validator-bin")
	dir, err := os.MkdirTemp("", "mini-du-validator-")
	if err != nil {
		return "", err
	}
	out = filepath.Join(dir, "mini-du")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/mini-du")
	cmd.Dir = workspace
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build candidate: %s", strings.TrimSpace(string(data)))
	}
	return out, nil
}
func compareScenario(bin, scenario string) error {
	root, err := os.MkdirTemp("", "mini-du-scenario-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	args := []string{root}
	switch scenario {
	case "help_short":
		return compareHelp(bin, []string{"-h"})
	case "help_long":
		return compareHelp(bin, []string{"--help"})
	case "invalid_option":
		return compareInvalidOption(bin)
	case "double_dash":
		if err = os.WriteFile(filepath.Join(root, "-s"), []byte("x"), 0644); err != nil {
			return err
		}
		return compareCandidateOracle(bin, []string{"--", "-s"}, []string{"-k", "--", "-s"}, root, root, scenario)
	case "combined_flags":
		for _, flags := range []string{"-sk", "-ks"} {
			if err := compareCandidateOracle(bin, []string{flags, root}, []string{"-k", "-s", root}, "", root, scenario+flags); err != nil {
				return err
			}
		}
		return nil
	case "humanized":
		if err = os.WriteFile(filepath.Join(root, "payload"), bytes.Repeat([]byte("x"), 1536*1024), 0644); err != nil {
			return err
		}
		return compareHumanized(bin, root)
	case "empty":
	case "nested":
		if err = os.MkdirAll(filepath.Join(root, "a", "b"), 0755); err != nil {
			return err
		}
		err = os.WriteFile(filepath.Join(root, "a", "b", "file"), bytes.Repeat([]byte("x"), 2048), 0644)
	case "multiple":
		for _, n := range []string{"a", "b"} {
			if err = os.WriteFile(filepath.Join(root, n), []byte("x"), 0644); err != nil {
				return err
			}
		}
		args = []string{filepath.Join(root, "a"), filepath.Join(root, "b")}
	case "unicode":
		err = os.WriteFile(filepath.Join(root, "café"), []byte("x"), 0644)
	case "spaces":
		err = os.WriteFile(filepath.Join(root, "with space"), []byte("x"), 0644)
	case "symlink":
		if err = os.WriteFile(filepath.Join(root, "target"), []byte("x"), 0644); err == nil {
			err = os.Symlink("target", filepath.Join(root, "link"))
		}
	case "hardlink":
		if err = os.WriteFile(filepath.Join(root, "target"), []byte("x"), 0644); err == nil {
			err = os.Link(filepath.Join(root, "target"), filepath.Join(root, "link"))
		}
	case "summary":
		args = []string{"-s", root}
	case "kibibytes":
		args = []string{"-k", root}
	case "missing":
		args = []string{filepath.Join(root, "missing")}
	case "mixed-missing":
		if err = os.WriteFile(filepath.Join(root, "ok"), []byte("x"), 0644); err == nil {
			args = []string{filepath.Join(root, "ok"), filepath.Join(root, "missing")}
		}
	}
	if err != nil {
		return err
	}
	if scenario == "missing" || scenario == "mixed-missing" {
		return compareFailureScenario(bin, args, append([]string{"-k"}, args...), "", root, scenario)
	}
	return compareCandidateOracle(bin, args, append([]string{"-k"}, args...), "", root, scenario)
}

func compareFailureScenario(bin string, candidateArgs, oracleArgs []string, dir, normalizeRoot, scenario string) error {
	var candidateOut, candidateErr, oracleOut, oracleErr bytes.Buffer
	candidate := exec.CommandContext(context.Background(), bin, candidateArgs...)
	oracle := exec.CommandContext(context.Background(), "du", oracleArgs...)
	candidate.Dir, oracle.Dir = dir, dir
	env := append(os.Environ(), "LC_ALL=C", "LANG=C", "BLOCKSIZE=1024")
	candidate.Env, oracle.Env = env, env
	candidate.Stdout, candidate.Stderr = &candidateOut, &candidateErr
	oracle.Stdout, oracle.Stderr = &oracleOut, &oracleErr
	candidateRunErr, oracleRunErr := candidate.Run(), oracle.Run()
	candidateExit, oracleExit := exitCode(candidateRunErr), exitCode(oracleRunErr)
	candidateOutput := normalizeOutput(candidateOut.String(), normalizeRoot, scenario)
	oracleOutput := normalizeOutput(oracleOut.String(), normalizeRoot, scenario)
	stdoutMatches := candidateOutput == oracleOutput
	if oracleOut.Len() == 0 {
		stdoutMatches = candidateOut.Len() == 0
	}
	if candidateExit != oracleExit || !stdoutMatches || strings.TrimSpace(candidateErr.String()) == "" {
		return fmt.Errorf("scenario %s differs: candidate_exit=%d oracle_exit=%d candidate_stdout=%q oracle_stdout=%q candidate_stderr=%q", scenario, candidateExit, oracleExit, boundedScenarioOutput(candidateOutput), boundedScenarioOutput(oracleOutput), boundedScenarioOutput(normalizeOutput(candidateErr.String(), normalizeRoot, scenario)))
	}
	return nil
}

func compareCandidateOracle(bin string, candidateArgs, oracleArgs []string, dir, normalizeRoot, scenario string) error {
	candidate := exec.CommandContext(context.Background(), bin, candidateArgs...)
	oracle := exec.CommandContext(context.Background(), "du", oracleArgs...)
	candidate.Dir, oracle.Dir = dir, dir
	env := append(os.Environ(), "LC_ALL=C", "LANG=C", "BLOCKSIZE=1024")
	candidate.Env = env
	oracle.Env = env
	co, ce := candidate.CombinedOutput()
	oo, oe := oracle.CombinedOutput()
	candidateExit, oracleExit := exitCode(ce), exitCode(oe)
	candidateOutput := normalizeOutput(string(co), normalizeRoot, scenario)
	oracleOutput := normalizeOutput(string(oo), normalizeRoot, scenario)
	if candidateExit != oracleExit || candidateOutput != oracleOutput {
		return fmt.Errorf("scenario %s differs: candidate_exit=%d oracle_exit=%d candidate_output=%q oracle_output=%q", scenario, candidateExit, oracleExit, boundedScenarioOutput(candidateOutput), boundedScenarioOutput(oracleOutput))
	}
	return nil
}

const scenarioDiagnosticOutputLimit = 4096

func boundedScenarioOutput(value string) string {
	if len(value) <= scenarioDiagnosticOutputLimit {
		return value
	}
	return value[:scenarioDiagnosticOutputLimit] + "...[truncated]"
}

const miniDUHelp = "Usage: mini-du [-s] [-k|-H] [--] [PATH...]\n" +
	"  -s          display only a total for each path\n" +
	"  -k          display sizes in 1024-byte units\n" +
	"  -H          display humanized binary units (KiB, MiB, GiB)\n" +
	"  -h, --help  display this help\n"

func compareHelp(bin string, args []string) error {
	cmd := exec.CommandContext(context.Background(), bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != miniDUHelp {
		return fmt.Errorf("help scenario differs: exit=%d output=%q", exitCode(err), out)
	}
	return nil
}

func compareInvalidOption(bin string) error {
	cmd := exec.CommandContext(context.Background(), bin, "-z")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if exitCode(err) != 1 || stdout.Len() != 0 || strings.TrimSpace(stderr.String()) == "" {
		return fmt.Errorf("invalid option scenario differs: exit=%d stdout=%q stderr=%q", exitCode(err), stdout.Bytes(), stderr.Bytes())
	}
	return nil
}

func compareHumanized(bin, root string) error {
	oracle := exec.CommandContext(context.Background(), "du", "-ks", root)
	env := append(os.Environ(), "LC_ALL=C", "LANG=C", "BLOCKSIZE=1024")
	oracle.Env = env
	oracleOut, oracleErr := oracle.Output()
	if oracleErr != nil {
		return fmt.Errorf("humanized oracle: %w", oracleErr)
	}
	fields := strings.Fields(string(oracleOut))
	if len(fields) < 1 {
		return errors.New("humanized oracle returned no size")
	}
	kib, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return fmt.Errorf("humanized oracle size: %w", err)
	}
	candidate := exec.CommandContext(context.Background(), bin, "-sH", root)
	candidate.Env = env
	out, runErr := candidate.CombinedOutput()
	want := fmt.Sprintf("%s\t%s\n", humanizedSize(kib*1024), root)
	if runErr != nil || string(out) != want {
		return fmt.Errorf("humanized scenario differs: exit=%d got=%q want=%q", exitCode(runErr), out, want)
	}
	return nil
}

func humanizedSize(bytes int64) string {
	if bytes == 0 {
		return "0B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if value < 10 && math.Abs(value-math.Round(value)) > 1e-9 {
		return fmt.Sprintf("%.1f%s", value, units[unit])
	}
	return fmt.Sprintf("%.0f%s", value, units[unit])
}
func normalizeOutput(s, root, scenario string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\\", "/"), filepath.ToSlash(root), "<ROOT>")
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if scenario == "multiple" || scenario == "mixed-missing" {
		for i := range lines {
			for j := i + 1; j < len(lines); j++ {
				if lines[j] < lines[i] {
					lines[i], lines[j] = lines[j], lines[i]
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}
