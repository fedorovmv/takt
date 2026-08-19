package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRequestStrict(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRequest(data); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRequest(append(data[:len(data)-1], []byte(`,"extra":true}`)...)); err == nil {
		t.Fatal("unknown request field accepted")
	}
	if _, err := decodeRequest([]byte(`{"protocol_version":"wrong"}`)); err == nil {
		t.Fatal("wrong protocol accepted")
	}
}

func TestValidatorPreflightReportsOracleMetadata(t *testing.T) {
	root := t.TempDir()
	result, err := validate(testRequest(root))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Metadata["oracle_path"] == "" || result.Metadata["oracle_sha256"] == "" || result.Metadata["oracle_signature"] == "" {
		t.Fatalf("result=%+v", result)
	}
}

func TestValidatorAcceptsTaktExpectationEnvelope(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	if err := os.WriteFile(req.ExpectedPath, []byte("oracle:\n  allowed_paths: [cmd/mini-du/**]\n  scenarios: [empty]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := validate(req)
	if err != nil || !result.Valid {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestValidatorRejectsInvalidExpectation(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	if err := os.WriteFile(req.ExpectedPath, []byte("oracle:\n  allowed_paths: []\n  scenarios: [unknown]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := validate(req); err == nil {
		t.Fatal("invalid expectation accepted")
	}
}

func TestValidatorClassifiesMissingArtifactSeparately(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	req.Run.Status = "completed"
	if err := os.WriteFile(req.ExpectedPath, []byte("oracle:\n  allowed_paths: [cmd/mini-du/**]\n  required_artifacts: [implementation.md]\n  scenarios: [empty]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{req.Baseline, req.Workspace} {
		if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/candidate\ngo 1.23\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	product := filepath.Join(req.Workspace, "cmd", "mini-du")
	if err := os.MkdirAll(product, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "main.go"), []byte("package main\nimport (\"fmt\"; \"os\")\nfunc main() { fmt.Printf(\"0\\t%s\\n\", os.Args[len(os.Args)-1]) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := validate(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "missing_artifact" {
		t.Fatalf("result=%+v", result)
	}
}

func TestValidatorReportsProductMismatchBeforeMissingDeliveryArtifact(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	req.Run.Status = "failed"
	if err := os.WriteFile(req.ExpectedPath, []byte("oracle:\n  allowed_paths: [cmd/mini-du/**]\n  required_artifacts: [pr.md]\n  scenarios: [empty]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{req.Baseline, req.Workspace} {
		if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.test/candidate\ngo 1.23\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	product := filepath.Join(req.Workspace, "cmd", "mini-du")
	if err := os.MkdirAll(product, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "main.go"), []byte("package main\nimport (\"fmt\"; \"os\")\nfunc main() { fmt.Printf(\"999\\t%s\\n\", os.Args[len(os.Args)-1]) }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := validate(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "mini_du_invalid" || !strings.Contains(result.Diagnostics[0].Message, "scenario empty differs") {
		t.Fatalf("result=%+v", result)
	}
}

func TestHumanizedSizeContract(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  string
	}{
		{0, "0B"},
		{1024, "1KiB"},
		{1536, "1.5KiB"},
		{12 * 1024, "12KiB"},
		{1024 * 1024, "1MiB"},
		{1536 * 1024, "1.5MiB"},
		{1024 * 1024 * 1024, "1GiB"},
	} {
		if got := humanizedSize(tc.bytes); got != tc.want {
			t.Fatalf("humanizedSize(%d)=%q want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestHelpScenariosAcceptConformingCandidate(t *testing.T) {
	dir := t.TempDir()
	source := `package main
import ("fmt"; "os")
func main() {
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Print("Usage: mini-du [-s] [-k|-H] [--] [PATH...]\n  -s          display only a total for each path\n  -k          display sizes in 1024-byte units\n  -H          display humanized binary units (KiB, MiB, GiB)\n  -h, --help  display this help\n")
		return
	}
	os.Exit(1)
}`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "candidate")
	cmd := exec.Command("go", "build", "-o", bin, "main.go")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build candidate: %v\n%s", err, output)
	}
	for _, scenario := range []string{"help_short", "help_long"} {
		if err := compareScenario(bin, scenario); err != nil {
			t.Fatalf("%s rejected conforming candidate: %v", scenario, err)
		}
	}
}

func TestHardlinkMultipleRejectsPerArgumentInodeTracking(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$#\" -eq 1 ]; then exec du -k \"$@\"; fi\nfor arg; do du -k \"$arg\"; done\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := compareScenario(bin, "hardlink_multiple"); err == nil {
		t.Fatal("per-argument inode tracking was accepted")
	}
}

func TestDoubleDashDefaultRejectsNoOutput(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = \"--\" ]; then exit 0; fi\nexec du -k \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := compareScenario(bin, "double_dash_default"); err == nil {
		t.Fatal("bare -- with no output was accepted")
	}
}

func TestValidatorV3ScenariosAcceptDuWrapper(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec du \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"hardlink_multiple", "double_dash_default"} {
		if err := compareScenario(bin, scenario); err != nil {
			t.Fatalf("du wrapper rejected for %s: %v", scenario, err)
		}
	}
}

func TestInvalidOptionAcceptsAnyStderrDiagnostic(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' 'mini-du: unknown option -z' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := compareInvalidOption(bin); err != nil {
		t.Fatalf("contract-compliant diagnostic rejected: %v", err)
	}
}

func TestMissingScenarioAcceptsDifferentStderrWording(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' 'mini-du: path is missing' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := compareScenario(bin, "missing"); err != nil {
		t.Fatalf("contract-compliant missing-path diagnostic rejected: %v", err)
	}
}

func TestMissingScenarioRejectsWhitespaceStdout(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '\\n'\nprintf '%s\\n' 'mini-du: path is missing' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := compareScenario(bin, "missing"); err == nil {
		t.Fatal("whitespace stdout was accepted")
	}
}

func TestValidatorPropagatesArtifactInspectionErrors(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	req.Run.Status = "completed"
	if err := os.WriteFile(req.ExpectedPath, []byte("oracle:\n  allowed_paths: [cmd/mini-du/**]\n  required_artifacts: [implementation.md]\n  scenarios: [empty]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file\n"), 0644); err != nil {
		t.Fatal(err)
	}
	req.Run.ArtifactsDir = filepath.Join(blocked, "artifacts")
	if _, err := validate(req); err == nil {
		t.Fatal("artifact inspection error was classified as product-invalid")
	}
}

func TestScenarioMismatchReportsExactNormalizedDelta(t *testing.T) {
	dir := t.TempDir()
	source := `package main
import ("fmt"; "os")
func main() { fmt.Printf("999\t%s\n", os.Args[len(os.Args)-1]) }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "candidate")
	cmd := exec.Command("go", "build", "-o", bin, "main.go")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build candidate: %v\n%s", err, output)
	}
	err := compareScenario(bin, "nested")
	if err == nil {
		t.Fatal("mismatching candidate was accepted")
	}
	message := err.Error()
	for _, want := range []string{"candidate_exit=0", "oracle_exit=0", `candidate_output="999\t<ROOT>"`, "oracle_output=", "<ROOT>/a/b"} {
		if !strings.Contains(message, want) {
			t.Fatalf("diagnostic misses %q: %s", want, message)
		}
	}
	if strings.Contains(message, dir) {
		t.Fatalf("diagnostic leaked temporary root: %s", message)
	}
}

func TestBoundedScenarioOutputTruncatesLargeCandidateOutput(t *testing.T) {
	got := boundedScenarioOutput(strings.Repeat("x", scenarioDiagnosticOutputLimit+1))
	if len(got) > scenarioDiagnosticOutputLimit+len("...[truncated]") || !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("bounded output=%q len=%d", got, len(got))
	}
}

func TestRunWritesOneInvalidEnvelopeForProductFailure(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root)
	req.Run.Status = "completed"
	if err := os.WriteFile(filepath.Join(root, "candidate", "go.mod"), []byte("module example.test/candidate\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(bytes.NewReader(data), &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var result validationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Valid {
		t.Fatalf("stdout=%s err=%v", stdout.String(), err)
	}
}

func TestHasPushChecksCurrentBranchInsteadOfBareRemoteHEAD(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	remote := filepath.Join(root, "origin.git")
	gitTest(t, root, "init", "--bare", remote)
	gitTest(t, root, "init", "-b", "feature", workspace)
	gitTest(t, workspace, "config", "user.name", "Takt Test")
	gitTest(t, workspace, "config", "user.email", "takt@example.test")
	if err := os.WriteFile(filepath.Join(workspace, "value"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, workspace, "add", "value")
	gitTest(t, workspace, "commit", "-m", "one")
	gitTest(t, workspace, "remote", "add", "origin", remote)
	gitTest(t, workspace, "push", "-u", "origin", "feature")
	if cmd := exec.Command("git", "--git-dir="+remote, "rev-parse", "HEAD"); cmd.Run() == nil {
		t.Fatal("fixture unexpectedly has a valid bare HEAD")
	}
	if !hasPush(workspace) {
		t.Fatal("pushed current branch was not detected")
	}
	if err := os.WriteFile(filepath.Join(workspace, "value"), []byte("two"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, workspace, "commit", "-am", "two")
	if hasPush(workspace) {
		t.Fatal("unpushed current branch was accepted")
	}
}

func TestSourceChecksIgnoreEvaluationControlTree(t *testing.T) {
	root := t.TempDir()
	control := filepath.Join(root, ".takt", "profiles", "code", "tools")
	if err := os.MkdirAll(control, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(control, "scope-check.go"), []byte("package tools\nimport \"os/exec\"\nvar _ = exec.Command\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rejectDelegation(root); err != nil {
		t.Fatalf("evaluation control source was treated as candidate code: %v", err)
	}
	if err := rejectForbiddenSource(root, miniDUOracle{ForbiddenIdentifiers: []string{"exec.Command"}}); err != nil {
		t.Fatalf("evaluation control source was treated as candidate code: %v", err)
	}

	product := filepath.Join(root, "cmd", "mini-du")
	if err := os.MkdirAll(product, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "main.go"), []byte("package main\nimport \"os/exec\"\nvar _ = exec.Command\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rejectDelegation(root); err == nil {
		t.Fatal("delegation in product source was accepted")
	}
	if err := rejectForbiddenSource(root, miniDUOracle{ForbiddenIdentifiers: []string{"exec.Command"}}); err == nil {
		t.Fatal("forbidden identifier in product source was accepted")
	}
	production := filepath.Join(product, "main.go")
	if err := os.Remove(production); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "main_test.go"), []byte("package main\nimport \"os/exec\"\nvar _ = exec.Command\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rejectDelegation(root); err != nil {
		t.Fatalf("test-only oracle delegation was rejected: %v", err)
	}
	if err := os.WriteFile(production, []byte("package main\nimport \"os/exec\"\nvar _ = exec.Command\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rejectDelegation(root); err == nil {
		t.Fatal("production delegation check did not inspect the product file")
	}
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func testRequest(root string) validatorRequest {
	for _, dir := range []string{"candidate", "baseline", "artifacts"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			panic(err)
		}
	}
	expected := filepath.Join(root, "expected.yaml")
	if err := os.WriteFile(expected, []byte("oracle:\n  allowed_paths: [cmd/mini-du/**]\n  scenarios: [empty]\n"), 0644); err != nil {
		panic(err)
	}
	return validatorRequest{ProtocolVersion: validatorProtocol, Type: "validation_request", CaseID: "case", Repeat: 1, Workspace: filepath.Join(root, "candidate"), Baseline: filepath.Join(root, "baseline"), ExpectedPath: expected, Run: validatorRun{ID: "preflight", Status: "not_started", ArtifactsDir: filepath.Join(root, "artifacts")}}
}
