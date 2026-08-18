package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/workflow"
)

func TestRouteDSLE2EContract(t *testing.T) {
	work := t.TempDir()
	copyTree(t, filepath.Join(repoRoot, "examples", "route-dsl-e2e"), work)
	fakePi := binary(t, "takt-fake-pi")
	cfg := writeFile(t, work, "config-test.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
models:
  route-model:
    provider: openai
    id: fake-route-model
    params:
      reasoning_effort: high
assistants:
  pi:
    type: pi
    binary: %s
    args: ["--fake-case", "route-dsl"]
    session_dir: %s
    project_trust: approve
    max_output_bytes: 1048576
`, fakePi, filepath.Join(work, ".takt", "pi-sessions")))
	wf := filepath.Join(work, "workflow.yaml")
	takt(t, nil, "validate", wf, "--config", cfg, "--workspace", work, "--json").RequireSuccess(t)
	state := resultObject(t, takt(t, nil, "run", wf, "--config", cfg, "--workspace", work, "--input", filepath.Join(work, "specification.md"), "--json").RequireSuccess(t).JSON(t))
	if state["status"] != "waiting" {
		t.Fatalf("state=%#v", state)
	}
	waiting := state["waiting"].(map[string]any)
	if waiting["node_id"] != "approve-result" {
		t.Fatalf("waiting=%#v", waiting)
	}
	implement := state["nodes"].(map[string]any)["implement"].(map[string]any)
	if implement["status"] != "completed" || implement["attempts"].(float64) != 2 || implement["session_id"] != "fake-pi-session-1" || !strings.Contains(implement["feedback"].(string), "ROUTE_INVALID") {
		t.Fatalf("implement=%#v", implement)
	}
	id := stringField(t, state, "id")
	requireFileContains(t, filepath.Join(work, "route.yaml"), "valid: true")
	artifacts := filepath.Join(work, ".takt", "runs", id, "artifacts")
	requireFileContains(t, filepath.Join(artifacts, "validation.json"), `"valid":true`)
	requireFileContains(t, filepath.Join(work, ".takt", "runs", id, "events.jsonl"), "node.retry")
	final := resultObject(t, takt(t, nil, "answer", id, "approve-result", "--workspace", work, "--value", "approved", "--json").RequireSuccess(t).JSON(t))
	if final["status"] != "completed" || final["nodes"].(map[string]any)["approve-result"].(map[string]any)["output"] != "approved" {
		t.Fatalf("final=%#v", final)
	}
}

func TestRouteDSLEvaluationContract(t *testing.T) {
	tmp := t.TempDir()
	cases := filepath.Join(tmp, "cases")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cases, "http-jq.md", "Получить клиента по HTTP, преобразовать ответ через jq и отправить результат в целевую систему.\n")
	writeFile(t, cases, "error-path.md", "Получить данные по HTTP, обработать отсутствие записи контролируемой ошибкой и отправить результат далее.\n")
	fakePi := binary(t, "takt-fake-pi")
	cfg := writeFile(t, tmp, "config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
models:
  route-model:
    provider: openai
    id: fake-route-model
    params:
      reasoning_effort: high
assistants:
  pi:
    type: pi
    binary: %s
    args: ["--fake-case", "route-dsl"]
    session_dir: .takt/pi-sessions
    project_trust: approve
    max_output_bytes: 1048576
`, fakePi))
	output := filepath.Join(tmp, "eval")
	reportEnv := takt(t, nil, "eval", "run", filepath.Join(repoRoot, "examples", "route-dsl-e2e", "workflow.yaml"), "--config", cfg, "--cases", cases, "--workspace-template", filepath.Join(repoRoot, "examples", "route-dsl-e2e"), "--output", output, "--answer", "approved", "--strategy-id", "fake-pi-route-feedback-v1", "--benchmark-id", "route-dsl-infrastructure", "--quality-node", "full-validation", "--generation-node", "implement", "--validator-id", "synthetic-route-tool", "--validator-version", "1", "--validator-path", "route-tool", "--replace", "--json").RequireSuccess(t).JSON(t)
	report := resultObject(t, reportEnv)
	if report["report_version"] != "takt-evaluation/v1alpha1" {
		t.Fatalf("report=%#v", report)
	}
	runs := report["runs"].([]any)
	if len(runs) != 2 {
		t.Fatalf("runs=%d", len(runs))
	}
	summary := report["summary"].(map[string]any)
	if summary["total"].(float64) != 2 || summary["valid"].(float64) != 2 || summary["final_success_rate"].(float64) != 1 {
		t.Fatalf("summary=%#v", summary)
	}
	for _, raw := range runs {
		run := raw.(map[string]any)
		if run["status"] != "completed" || run["quality_node_status"] != "completed" || run["attempts_to_valid"].(float64) != 2 {
			t.Fatalf("run=%#v", run)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "report.json")); err != nil {
		t.Fatal(err)
	}
	takt(t, nil, "eval", "report", output, "--json").RequireSuccess(t)
	collision := filepath.Join(tmp, "collision-cases")
	if err := os.MkdirAll(collision, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, collision, "a b.md", "first\n")
	writeFile(t, collision, "a+b.md", "second\n")
	collisionOut := filepath.Join(tmp, "collision-output")
	takt(t, nil, "eval", "run", filepath.Join(repoRoot, "examples", "route-dsl-e2e", "workflow.yaml"), "--config", cfg, "--cases", collision, "--workspace-template", filepath.Join(repoRoot, "examples", "route-dsl-e2e"), "--output", collisionOut, "--replace").RequireFailure(t).Contains(t, "case id collision")
	if _, err := os.Stat(collisionOut); !os.IsNotExist(err) {
		t.Fatalf("collision output exists: %v", err)
	}
	overlap := filepath.Join(tmp, "overlap-template")
	copyTree(t, filepath.Join(repoRoot, "examples", "route-dsl-e2e"), overlap)
	overlapOut := filepath.Join(overlap, "results")
	takt(t, nil, "eval", "run", filepath.Join(overlap, "workflow.yaml"), "--config", cfg, "--cases", cases, "--workspace-template", overlap, "--output", overlapOut, "--replace").RequireFailure(t).Contains(t, "must not overlap")
	if _, err := os.Stat(overlapOut); !os.IsNotExist(err) {
		t.Fatalf("overlap output exists: %v", err)
	}
}

func TestRouteDSLBenchmarkContract(t *testing.T) {
	tmp := t.TempDir()
	cases := filepath.Join(tmp, "cases")
	workspace := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(repoRoot, "examples", "route-dsl-e2e", "route-tool"), filepath.Join(workspace, "route-tool"), 0o755)
	writeFile(t, cases, "one.md", "HTTP -> transform -> target\n")
	writeFile(t, cases, "two.md", "HTTP error mapping -> target\n")
	manifest := writeFile(t, tmp, "cases.yaml", `apiVersion: takt/evaluation/v1alpha1
kind: CaseManifest
cases:
  one:
    category: smoke
    difficulty: basic
    source: contract
  two:
    category: errors
    difficulty: basic
    source: contract
`)
	fakePi := binary(t, "takt-fake-pi")
	cfg := writeFile(t, tmp, "config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi
models:
  route-model:
    provider: openai
    id: fake-route-model
assistants:
  pi:
    type: pi
    binary: %s
    args: ["--fake-case", "route-dsl"]
    session_dir: .takt/pi-sessions
    project_trust: approve
    max_output_bytes: 1048576
`, fakePi))
	matrix := writeFile(t, tmp, "matrix.yaml", fmt.Sprintf(`apiVersion: takt/evaluation/v1alpha1
kind: EvaluationMatrix
metadata:
  name: route-dsl-contract
benchmark:
  id: route-dsl-contract-v1
  baseline_strategy: baseline-direct
  cases: %s
  case_manifest: %s
  workspace_template: %s
  repeat: 2
  quality_node: full-validation
  generation_node: implement
  validator:
    id: synthetic-route-tool
    version: "1"
    path: %s
strategies:
  - id: baseline-direct
    workflow: %s
    config: %s
  - id: feedback-repair
    workflow: %s
    config: %s
  - id: inspect-feedback
    workflow: %s
    config: %s
gates:
  - strategy: feedback-repair
    final_success_rate_min: 1
    unstable_cases_max: 0
  - strategy: inspect-feedback
    final_success_rate_min: 1
    unstable_cases_max: 0
`, cases, manifest, workspace, filepath.Join(workspace, "route-tool"), filepath.Join(repoRoot, "examples", "route-dsl-benchmark", "strategies", "baseline-direct.yaml"), cfg, filepath.Join(repoRoot, "examples", "route-dsl-benchmark", "strategies", "feedback-repair.yaml"), cfg, filepath.Join(repoRoot, "examples", "route-dsl-benchmark", "strategies", "inspect-feedback.yaml"), cfg))
	out := filepath.Join(tmp, "results")
	takt(t, nil, "eval", "benchmark", matrix, "--output", out, "--replace", "--json").RequireSuccess(t)
	requireFileContains(t, filepath.Join(out, "benchmark.json"), `"passed": true`, `"candidate_only_valid": 4`, `"average_time_to_valid_ms"`)
	requireFileContains(t, filepath.Join(out, "strategies", "feedback-repair", "report.json"), `"diagnostics_by_fingerprint"`)
	takt(t, nil, "eval", "report", out, "--json").RequireSuccess(t).Contains(t, `"report_version": "takt-evaluation-matrix/v1alpha1"`)
	takt(t, nil, "eval", "compare", filepath.Join(out, "strategies", "baseline-direct"), filepath.Join(out, "strategies", "feedback-repair"), "--json").RequireSuccess(t).Contains(t, `"candidate_only_valid": 4`)
	data, err := os.ReadFile(matrix)
	if err != nil {
		t.Fatal(err)
	}
	failing := strings.Replace(string(data), "    final_success_rate_min: 1\n", "    final_success_rate_min: 1\n    success_at_1_min: 1\n", 1)
	failingMatrix := writeFile(t, tmp, "failing-matrix.yaml", failing)
	failingOut := filepath.Join(tmp, "failing")
	takt(t, nil, "eval", "benchmark", failingMatrix, "--output", failingOut, "--replace", "--json").RequireFailure(t)
	requireFileContains(t, filepath.Join(failingOut, "benchmark.json"), `"passed": false`)
}

func TestFlowEvaluationContract(t *testing.T) {
	if os.Getenv("TAKT_FLOW_E2E_VALIDATOR") == "1" {
		fmt.Print(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"metadata":{"validator":"flow-e2e"}}`)
		os.Exit(0)
	}
	root := t.TempDir()
	caseRoot := filepath.Join(root, "cases", "accept")
	if err := os.MkdirAll(filepath.Join(caseRoot, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, caseRoot, "input.md", "input\n")
	writeFile(t, caseRoot, "expected.yaml", "oracle: {expected: true}\n")
	writeFile(t, filepath.Join(caseRoot, "workspace"), "main.txt", "base\n")
	fakeAssistant := binary(t, "takt-fake-assistant")
	workflow := writeFile(t, root, "flow.yaml", "name: flow-e2e\nprovider: fixture\nmodel: fixture\nworktree:\n  enabled: true\nnodes:\n  - id: done\n    prompt: complete this evaluation case\n")
	config := writeFile(t, root, "config.yaml", fmt.Sprintf("apiVersion: takt/v1alpha1\nkind: Config\nmodels:\n  fixture:\n    provider: fixture\n    id: fixture\nassistants:\n  fixture:\n    type: process\n    protocol: takt-assistant/v1alpha2\n    argv: [%q, %q]\n", fakeAssistant, "--case=success"))
	_ = workflow
	_ = config
	suite := writeFile(t, root, "suite.yaml", fmt.Sprintf("version: takt-flow-evaluation/v1alpha1\nworkflow: flow.yaml\nconfig: config.yaml\ncases: {directory: cases}\nvalidator:\n  id: flow-e2e\n  version: '1'\n  command: [%q, %q, %q]\n  path: flow.yaml\n  timeout: 10s\n  max_output_bytes: 4096\ngates:\n  valid_rate: {min: 1}\n", os.Args[0], "-test.run=TestFlowEvaluationContract", "--"))
	output := filepath.Join(t.TempDir(), "output")
	report := resultObject(t, takt(t, []string{"TAKT_FLOW_E2E_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--json").RequireSuccess(t).JSON(t))
	if report["report_version"] != "takt-evaluation/v1alpha1" || report["mode"] != "flow" || report["summary"].(map[string]any)["flow"].(map[string]any)["true_accept"] != float64(1) {
		t.Fatalf("report=%#v", report)
	}
	if _, err := os.Stat(filepath.Join(output, "report.json")); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(output, "cases", "accept", "repeat-001")
	for _, path := range []string{"run.json", "validation-request.json", "validation-result.json", filepath.Join("artifacts", "manifest.json")} {
		if _, err := os.Stat(filepath.Join(evidence, path)); err != nil {
			t.Fatalf("missing evidence %s: %v", path, err)
		}
	}
	writeFile(t, root, "flow.yaml", "name: flow-e2e-failing\nprovider: fixture\nmodel: fixture\nworktree:\n  enabled: true\nnodes:\n  - id: done\n    bash: 'exit 1'\n")
	failingOutput := filepath.Join(t.TempDir(), "failing-output")
	takt(t, []string{"TAKT_FLOW_E2E_VALIDATOR=1"}, "eval", "flow", suite, "--output", failingOutput, "--json").RequireFailure(t).Contains(t, "flow evaluation gates failed")
	if _, err := os.Stat(filepath.Join(failingOutput, "report.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluationAnalysisBoundary(t *testing.T) {
	if os.Getenv("TAKT_ANALYSIS_VALIDATOR") == "1" {
		fmt.Print(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"diagnostics":[{"code":"missing_artifact","severity":"error","message":"implementation.md is absent"}]}`)
		os.Exit(0)
	}
	root := t.TempDir()
	caseRoot := filepath.Join(root, "cases", "problem")
	if err := os.MkdirAll(filepath.Join(caseRoot, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, caseRoot, "input.md", "find the missing artifact\n")
	writeFile(t, caseRoot, "expected.yaml", "oracle: {expected: false}\n")
	writeFile(t, filepath.Join(caseRoot, "workspace"), "main.txt", "base\n")
	fakePi := binary(t, "takt-fake-pi")
	config := writeFile(t, root, "config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi
models:
  takt_analyze:
    provider: fake
    id: fake-model
assistants:
  pi:
    type: pi
    binary: %q
    args: ["--fake-case", "analysis-success"]
    project_trust: approve
    max_output_bytes: 1048576
`, fakePi))
	workflow := writeFile(t, root, "flow.yaml", `name: analysis-flow
provider: coding-agent
model: takt_analyze
nodes:
  - id: implement
    prompt: inspect the case
`)
	_ = workflow
	suite := writeFile(t, root, "suite.yaml", fmt.Sprintf(`version: takt-flow-evaluation/v1alpha1
workflow: flow.yaml
config: config.yaml
cases: {directory: cases}
validator:
  id: analysis-validator
  version: "1"
  command: [%q, %q, %q]
  path: flow.yaml
  timeout: 10s
  max_output_bytes: 4096
gates: {valid_rate: {min: 0}}
`, os.Args[0], "-test.run=^TestEvaluationAnalysisBoundary$", "--"))
	output := filepath.Join(t.TempDir(), "evaluation")
	os.Setenv("TAKT_ANALYSIS_SECRET", "known-secret")
	t.Cleanup(func() { _ = os.Unsetenv("TAKT_ANALYSIS_SECRET") })
	takt(t, []string{"TAKT_ANALYSIS_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireSuccess(t)
	original, err := os.ReadFile(filepath.Join(output, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	analysis := takt(t, nil, "eval", "analyze", output, "--config", config, "--case", "problem", "--language", "ru", "--trace", "--json").RequireSuccess(t)
	report := resultObject(t, analysis.JSON(t))
	if report["report_version"] != "takt-evaluation-analysis/v1alpha1" || report["status"] != "completed" || report["language"] != "ru" {
		t.Fatalf("analysis report=%#v", report)
	}
	analyses := report["analyses"].([]any)
	if len(analyses) != 1 || analyses[0].(map[string]any)["analysis_status"] != "completed" {
		t.Fatalf("analyses=%#v", analyses)
	}
	caseAnalysis := analyses[0].(map[string]any)
	if caseAnalysis["prompt"] == "" || caseAnalysis["prompt_fingerprint"] == "" || caseAnalysis["trace_path"] == "" {
		t.Fatalf("analysis prompt/session trace metadata missing: %#v", caseAnalysis)
	}
	if prompt, ok := caseAnalysis["prompt"].(string); !ok || !strings.Contains(prompt, `"language":"ru"`) {
		t.Fatalf("analysis language missing from rendered prompt: %q", prompt)
	}
	if session, ok := caseAnalysis["session"].(map[string]any); !ok || session["session_evidence"] != "recorded" || session["session_evidence_path"] == "" {
		t.Fatalf("analysis session evidence metadata missing: %#v", caseAnalysis["session"])
	}
	if after, err := os.ReadFile(filepath.Join(output, "report.json")); err != nil || string(after) != string(original) {
		t.Fatalf("source report changed: err=%v", err)
	}
	entries, err := filepath.Glob(filepath.Join(output, "analyses", "*", "report.json"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("analysis report files=%v err=%v", entries, err)
	}
	analysisRoot := filepath.Dir(entries[0])
	if _, err := os.Stat(filepath.Join(analysisRoot, "trace.log")); err != nil {
		t.Fatalf("analysis trace missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(analysisRoot, "cases", "problem", "repeat-001", "trace.log")); err != nil {
		t.Fatalf("case analysis trace missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(analysisRoot, "cases", "problem", "repeat-001", "sessions", "analyze.jsonl")); err != nil {
		t.Fatalf("analysis session evidence missing: %v", err)
	}
	sessionMatches, _ := filepath.Glob(filepath.Join(output, "cases", "problem", "repeat-001", "sessions", "**", "*.jsonl"))
	_ = sessionMatches
	if _, err := os.Stat(filepath.Join(analysisRoot, "cases", "problem", "repeat-001", "analysis.json")); err != nil {
		t.Fatal(err)
	}
	executorManifest := decodeJSONFile(t, filepath.Join(output, "cases", "problem", "repeat-001", "executor-manifest.json"))
	executions := executorManifest["executions"].([]any)
	if len(executions) != 1 || executions[0].(map[string]any)["session_evidence"] != "recorded" {
		t.Fatalf("executor manifest=%#v", executorManifest)
	}
	sessionPath := filepath.Join(output, "cases", "problem", "repeat-001", "sessions", "implement", "attempt-001-provider-001.jsonl")
	sessionBytes, err := os.ReadFile(sessionPath)
	if err != nil || strings.Contains(string(sessionBytes), "known-secret") {
		t.Fatalf("session evidence err=%v content=%q", err, sessionBytes)
	}
}

func TestEvaluationAnalysisMalformedProviderOutputIsPersisted(t *testing.T) {
	if os.Getenv("TAKT_ANALYSIS_VALIDATOR_MALFORMED") == "1" {
		fmt.Print(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"diagnostics":[{"code":"missing_artifact","severity":"error","message":"implementation.md is absent"}]}`)
		os.Exit(0)
	}
	root := t.TempDir()
	caseRoot := filepath.Join(root, "cases", "problem")
	if err := os.MkdirAll(filepath.Join(caseRoot, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, caseRoot, "input.md", "malformed analysis\n")
	writeFile(t, caseRoot, "expected.yaml", "oracle: {expected: false}\n")
	writeFile(t, filepath.Join(caseRoot, "workspace"), "main.txt", "base\n")
	fakePi := binary(t, "takt-fake-pi")
	config := writeFile(t, root, "config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi
models:
  takt_analyze: {provider: fake, id: fake-model}
assistants:
  pi:
    type: pi
    binary: %q
    args: ["--fake-case", "analysis-malformed"]
    project_trust: approve
    max_output_bytes: 1048576
`, fakePi))
	writeFile(t, root, "flow.yaml", `name: analysis-malformed-flow
provider: coding-agent
model: takt_analyze
nodes:
  - id: implement
    prompt: inspect the case
`)
	suite := writeFile(t, root, "suite.yaml", fmt.Sprintf(`version: takt-flow-evaluation/v1alpha1
workflow: flow.yaml
config: config.yaml
cases: {directory: cases}
validator:
  id: analysis-validator
  version: "1"
  command: [%q, %q, %q]
  path: flow.yaml
  timeout: 10s
  max_output_bytes: 4096
gates: {valid_rate: {min: 0}}
`, os.Args[0], "-test.run=^TestEvaluationAnalysisMalformedProviderOutputIsPersisted$", "--"))
	output := filepath.Join(t.TempDir(), "evaluation")
	takt(t, []string{"TAKT_ANALYSIS_VALIDATOR_MALFORMED=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireSuccess(t)
	original, err := os.ReadFile(filepath.Join(output, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	result := takt(t, nil, "eval", "analyze", output, "--config", config, "--case", "problem", "--json").RequireFailure(t)
	report := resultObject(t, result.JSON(t))
	analyses := report["analyses"].([]any)
	if len(analyses) != 1 || analyses[0].(map[string]any)["analysis_status"] != "protocol" {
		t.Fatalf("analyses=%#v stderr=%s", analyses, result.Stderr)
	}
	caseAnalysis := analyses[0].(map[string]any)
	rawOutputPath, ok := caseAnalysis["raw_output_path"].(string)
	if !ok || rawOutputPath == "" {
		t.Fatalf("protocol output path missing: %#v", caseAnalysis)
	}
	entries, err := filepath.Glob(filepath.Join(output, "analyses", "*", "report.json"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("analysis report files=%v err=%v", entries, err)
	}
	analysisRoot := filepath.Dir(entries[0])
	rawOutput, err := os.ReadFile(filepath.Join(analysisRoot, "cases", "problem", "repeat-001", filepath.FromSlash(rawOutputPath)))
	if err != nil || string(rawOutput) != "{not-json}" {
		t.Fatalf("raw protocol output err=%v content=%q", err, rawOutput)
	}
	if after, err := os.ReadFile(filepath.Join(output, "report.json")); err != nil || string(after) != string(original) {
		t.Fatalf("source report changed: err=%v", err)
	}
}

func TestProductionFlowEvaluation(t *testing.T) {
	if os.Getenv("TAKT_PRODUCTION_FLOW_VALIDATOR") == "1" {
		var request struct {
			Workspace string `json:"workspace"`
			Run       struct {
				ArtifactsDir string `json:"artifacts_dir"`
			} `json:"run"`
			ExternalState *struct {
				SCMDir string `json:"scm_dir"`
			} `json:"external_state"`
		}
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if _, err := os.Stat(request.Workspace); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if request.Run.ArtifactsDir == "" {
			fmt.Print(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}`)
			os.Exit(0)
		}
		check := exec.Command("go", "test", "./...")
		check.Dir = request.Workspace
		if output, err := check.CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, string(output))
			os.Exit(1)
		}
		if request.ExternalState == nil {
			fmt.Fprintln(os.Stderr, "missing SCM state")
			os.Exit(1)
		}
		if _, err := os.ReadFile(filepath.Join(request.ExternalState.SCMDir, "calls.log")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if _, err := os.Stat(request.Run.ArtifactsDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}`)
		os.Exit(0)
	}

	fake := binary(t, "takt-fake-code-agent")
	for _, tc := range []struct {
		name, selector, require string
		pullRequest             int
		input                   string
		wantNodes               []string
		wantArtifacts           []string
		wantGH                  string
	}{
		{"feature", "code:feature-development", "repository", 0, "# Implement the smoke change\n", []string{"implement", "validate-agent", "initial-verdict", "review-acceptance-gate", "validate", "create-pr", "pr-effect-gate", "summary"}, []string{"implementation.md", "validation.md", "pr.md", "pr-url.txt", "summary.md"}, "pr create"},
		{"comprehensive", "code:comprehensive-pr-review", "pull_request", 17, `{"repository":"example/mini-du","pull_request":17,"fixes_permitted":true,"validation_commands":["go test ./..."]}`, []string{"review", "summary", "review-acceptance-gate"}, nil, "pr view"},
		{"architect", "code:architect", "pull_request", 27, `{"repository":"example/mini-du","pull_request":27,"fixes_permitted":true}`, []string{"sweep", "plan", "implement", "review", "summary", "scope", "classify"}, []string{"architecture.md", "plan.md", "implementation.md", "summary.md"}, "pr view"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			suite := writeProductionFlowSuite(t, root, tc.selector, tc.require, tc.pullRequest, tc.input, fake)
			output := filepath.Join(t.TempDir(), "output")
			args := []string{"eval", "flow", suite, "--output", output, "--keep-workspaces", "--trace", "--json"}
			result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, args...).RequireSuccess(t)
			if !strings.Contains(result.Stderr, "CASE smoke#1 | prepare") || !strings.Contains(result.Stderr, "| accepted | id=run-") || !strings.Contains(result.Stderr, "REPORT | finalized | path=") || !strings.Contains(result.Stderr, "EVAL | progress |") {
				t.Fatalf("trace missing from stderr: %s", result.Stderr)
			}
			report := resultObject(t, result.JSON(t))
			run := report["runs"].([]any)[0].(map[string]any)
			if run["status"] != "completed" {
				t.Fatalf("run=%#v", run)
			}
			nodes := run["nodes"].(map[string]any)
			for _, id := range tc.wantNodes {
				node, ok := flowNode(nodes, id)
				if !ok || node["status"] != "completed" {
					t.Fatalf("node %q=%#v", id, node)
				}
			}
			if tc.name == "feature" {
				for _, id := range []string{"repair", "revalidate-agent", "revalidation-verdict"} {
					node, ok := flowNode(nodes, id)
					if !ok || node["status"] != "skipped" {
						t.Fatalf("node %q=%#v", id, node)
					}
				}
				for _, id := range []string{"initial-verdict", "review-acceptance-gate", "validate", "create-pr", "pr-effect-gate", "summary"} {
					node, ok := flowNode(nodes, id)
					if !ok || node["status"] != "completed" {
						t.Fatalf("node %q=%#v", id, node)
					}
				}
			}
			evidence := filepath.Join(output, "cases", "smoke", "repeat-001")
			request := decodeJSONFile(t, filepath.Join(evidence, "validation-request.json"))
			if request["workspace"] == "" {
				t.Fatalf("validator did not observe retained execution workspace: %#v", request)
			}
			external := request["external_state"].(map[string]any)
			log := filepath.Join(external["scm_dir"].(string), "calls.log")
			requireFileContains(t, log, tc.wantGH)
			if tc.pullRequest > 0 {
				requireGHCall(t, log, []string{"pr", "view", fmt.Sprint(tc.pullRequest), "--json", "number,title,state,baseRefName,headRefName"})
			}
			for _, artifact := range tc.wantArtifacts {
				requireEvidenceArtifact(t, evidence, artifact)
			}
			if tc.name == "feature" {
				origin := filepath.Join(output, "workspaces", "smoke", "repeat-001", "origin.git")
				if out := git(t, root, "--git-dir", origin, "show-ref", "--heads"); !strings.Contains(out, "fixture-head") {
					t.Fatalf("origin refs=%s", out)
				}
			}
		})
	}
}

func TestProductionFlowEvaluationFeatureVerdictBranches(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	for _, tc := range []struct {
		name, verdict          string
		wantRun, wantRepair    string
		wantReval, wantVerdict string
		wantGate, wantPR       string
	}{
		{"pass", "PASS", "completed", "skipped", "skipped", "skipped", "completed", "completed"},
		{"blocked", "BLOCKED", "failed", "skipped", "skipped", "skipped", "failed", "skipped"},
		{"repair-pass", "REPAIR+PASS", "completed", "completed", "completed", "completed", "completed", "completed"},
		{"repair-repair", "REPAIR+REPAIR", "failed", "completed", "completed", "completed", "failed", "skipped"},
		{"repair-blocked", "REPAIR+BLOCKED", "failed", "completed", "completed", "completed", "failed", "skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": tc.verdict})
			output := filepath.Join(t.TempDir(), "output")
			result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json")
			if tc.wantRun == "completed" {
				result.RequireSuccess(t)
			} else {
				result.RequireFailure(t)
			}
			report := resultObject(t, result.JSON(t))
			run := report["runs"].([]any)[0].(map[string]any)
			if run["status"] != tc.wantRun {
				t.Fatalf("run status=%v report=%#v", run["status"], run)
			}
			nodes := run["nodes"].(map[string]any)
			validateWant := "skipped"
			if tc.wantRun == "completed" {
				validateWant = "completed"
			}
			for id, want := range map[string]string{"repair": tc.wantRepair, "revalidate-agent": tc.wantReval, "revalidation-verdict": tc.wantVerdict, "review-acceptance-gate": tc.wantGate, "validate": validateWant, "create-pr": tc.wantPR} {
				node, ok := flowNode(nodes, id)
				if !ok || node["status"] != want {
					t.Fatalf("node %q=%#v want=%s", id, node, want)
				}
			}
			repair, _ := flowNode(nodes, "repair")
			revalidate, _ := flowNode(nodes, "revalidate-agent")
			wantAttempts := float64(0)
			if tc.wantRepair == "completed" {
				wantAttempts = 1
			}
			if repair["attempts"] != wantAttempts || revalidate["attempts"] != wantAttempts {
				t.Fatalf("repair attempts=%v revalidate attempts=%v want=%v", repair["attempts"], revalidate["attempts"], wantAttempts)
			}
			evidence := filepath.Join(output, "cases", "smoke", "repeat-001")
			for _, artifact := range []string{"review-fixes.md", "revalidation.md"} {
				got := evidenceArtifactPresent(t, evidence, artifact)
				want := tc.wantRepair == "completed"
				if got != want {
					t.Fatalf("artifact %s present=%v want=%v", artifact, got, want)
				}
			}
		})
	}
}

func TestProductionFlowEvaluationFeatureRevalidationVerdictFailures(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": "REPAIR+missing"})
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
	if run["status"] != "failed" {
		t.Fatalf("run=%#v", run)
	}
	nodes := run["nodes"].(map[string]any)
	for id, want := range map[string]string{"initial-verdict": "completed", "repair": "completed", "revalidate-agent": "completed", "revalidation-verdict": "failed", "review-acceptance-gate": "failed", "create-pr": "skipped"} {
		node, ok := flowNode(nodes, id)
		if !ok || node["status"] != want {
			t.Fatalf("node %q=%#v want=%s", id, node, want)
		}
	}
}

func TestProductionFlowEvaluationFeatureMarkdownEvidence(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	for _, tc := range []struct {
		name, verdict string
		wantNodes     map[string]string
	}{
		{"initial", "extra", map[string]string{"initial-verdict": "completed", "review-acceptance-gate": "completed", "create-pr": "completed"}},
		{"revalidation", "REPAIR+extra", map[string]string{"initial-verdict": "completed", "repair": "completed", "revalidate-agent": "completed", "revalidation-verdict": "completed", "review-acceptance-gate": "completed", "create-pr": "completed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": tc.verdict})
			output := filepath.Join(t.TempDir(), "output")
			result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireSuccess(t)
			run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
			nodes := run["nodes"].(map[string]any)
			for id, want := range tc.wantNodes {
				node, ok := flowNode(nodes, id)
				if !ok || node["status"] != want {
					t.Fatalf("node %q=%#v want=%s", id, node, want)
				}
			}
		})
	}
}

func TestProductionFlowEvaluationFeatureRepairPhaseFailure(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": "REPAIR+PASS", "FAKE_FAIL_PHASE": "feature-repair"})
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
	nodes := run["nodes"].(map[string]any)
	for id, want := range map[string]string{"repair": "failed", "revalidate-agent": "skipped", "revalidation-verdict": "skipped", "review-acceptance-gate": "failed", "create-pr": "skipped"} {
		node, ok := flowNode(nodes, id)
		if !ok || node["status"] != want {
			t.Fatalf("node %q=%#v want=%s", id, node, want)
		}
	}
}

func TestProductionFlowEvaluationFeatureReviewFixArtifactGates(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": "REPAIR+PASS", "FAKE_FLOW_ARTIFACT_KIND": "review-fixes.md:missing"})
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
	nodes := run["nodes"].(map[string]any)
	for id, want := range map[string]string{"repair": "completed", "revalidate-agent": "completed", "revalidation-verdict": "completed", "review-acceptance-gate": "failed", "create-pr": "skipped"} {
		node, ok := flowNode(nodes, id)
		if !ok || node["status"] != want {
			t.Fatalf("node %q=%#v want=%s", id, node, want)
		}
	}
}

func TestProductionFlowEvaluationFeatureVerdictParserFailures(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": "malformed"})
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
	if run["status"] != "failed" {
		t.Fatalf("run=%#v", run)
	}
	nodes := run["nodes"].(map[string]any)
	for id, want := range map[string]string{"initial-verdict": "failed", "review-acceptance-gate": "failed", "repair": "skipped", "revalidate-agent": "skipped"} {
		node, ok := flowNode(nodes, id)
		if !ok || node["status"] != want {
			t.Fatalf("node %q=%#v want=%s", id, node, want)
		}
	}
}

func TestProductionFlowEvaluationFeatureValidationPhaseFailure(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FAIL_PHASE": "feature-validation"})
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
	nodes := run["nodes"].(map[string]any)
	for id, want := range map[string]string{"initial-verdict": "failed", "review-acceptance-gate": "failed", "repair": "skipped", "revalidate-agent": "skipped"} {
		node, ok := flowNode(nodes, id)
		if !ok || node["status"] != want {
			t.Fatalf("node %q=%#v want=%s", id, node, want)
		}
	}
}

func TestProductionFlowEvaluationFeatureProducerFailsAfterArtifact(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	for _, tc := range []struct {
		name, verdict, phase string
		want                 map[string]string
	}{
		{"validation", "PASS", "feature-validation", map[string]string{"initial-verdict": "failed", "repair": "skipped", "revalidate-agent": "skipped", "revalidation-verdict": "skipped", "review-acceptance-gate": "failed", "create-pr": "skipped"}},
		{"revalidation", "REPAIR+PASS", "feature-revalidation", map[string]string{"initial-verdict": "completed", "repair": "completed", "revalidate-agent": "failed", "revalidation-verdict": "failed", "review-acceptance-gate": "failed", "create-pr": "skipped"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FEATURE_VERDICT_KIND": tc.verdict, "FAKE_FAIL_AFTER_ARTIFACT_PHASE": tc.phase})
			output := filepath.Join(t.TempDir(), "output")
			result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
			run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
			nodes := run["nodes"].(map[string]any)
			for id, want := range tc.want {
				node, ok := flowNode(nodes, id)
				if !ok || node["status"] != want {
					t.Fatalf("node %q=%#v want=%s", id, node, want)
				}
			}
		})
	}
}

func TestProductionFlowEvaluationPREffectAndArtifacts(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	for _, tc := range []struct {
		name, env         string
		wantRun, wantGate string
		wantCalls         int
	}{
		{"multiple-create-receipts", "FAKE_DUPLICATE_PR_CREATE=1", "completed", "completed", 2},
		{"exit-after-create", "FAKE_EXIT_AFTER_PR_CREATE=1", "completed", "completed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			key, value, _ := strings.Cut(tc.env, "=")
			suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{key: value})
			output := filepath.Join(t.TempDir(), "output")
			result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json")
			if tc.wantRun == "completed" {
				result.RequireSuccess(t)
			} else {
				result.RequireFailure(t)
			}
			run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
			if run["status"] != tc.wantRun {
				t.Fatalf("run=%#v", run)
			}
			nodes := run["nodes"].(map[string]any)
			gate, ok := flowNode(nodes, "pr-effect-gate")
			if !ok || gate["status"] != tc.wantGate {
				t.Fatalf("gate=%#v", gate)
			}
			request := decodeJSONFile(t, filepath.Join(output, "cases", "smoke", "repeat-001", "validation-request.json"))
			external := request["external_state"].(map[string]any)
			calls, err := os.ReadFile(filepath.Join(external["scm_dir"].(string), "calls.log"))
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(calls), "pr create"); got != tc.wantCalls {
				t.Fatalf("pr create calls=%d want=%d log=%q", got, tc.wantCalls, calls)
			}
			count, err := os.ReadFile(filepath.Join(external["scm_dir"].(string), "pr-count"))
			if err != nil || strings.TrimSpace(string(count)) != fmt.Sprint(tc.wantCalls) {
				t.Fatalf("pr-count=%q err=%v want=%d", count, err, tc.wantCalls)
			}
		})
	}
}

func TestProductionFlowEvaluationFeatureArtifactGates(t *testing.T) {
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake, map[string]string{"FAKE_FLOW_ARTIFACT_KIND": "summary.md:missing"})
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	run := resultObject(t, result.JSON(t))["runs"].([]any)[0].(map[string]any)
	nodes := run["nodes"].(map[string]any)
	if node, ok := flowNode(nodes, "summary"); !ok || node["status"] != "failed" {
		t.Fatalf("summary=%#v", node)
	}
}

func TestProductionFlowEvaluationPRGateRejectsMissingSCMSideEffect(t *testing.T) {
	t.Setenv("FAKE_SKIP_PR_CREATE", "1")
	fake := binary(t, "takt-fake-code-agent")
	root := t.TempDir()
	suite := writeProductionFlowSuite(t, root, "code:feature-development", "repository", 0, "# Implement the smoke change\n", fake)
	output := filepath.Join(t.TempDir(), "output")
	result := takt(t, []string{"TAKT_PRODUCTION_FLOW_VALIDATOR=1"}, "eval", "flow", suite, "--output", output, "--keep-workspaces", "--json").RequireFailure(t)
	report := resultObject(t, result.JSON(t))
	run := report["runs"].([]any)[0].(map[string]any)
	if run["status"] != "failed" {
		t.Fatalf("run=%#v", run)
	}
	nodes := run["nodes"].(map[string]any)
	gate, ok := flowNode(nodes, "pr-effect-gate")
	if !ok || gate["status"] != "failed" {
		t.Fatalf("pr-effect-gate=%#v", gate)
	}
	summary, ok := flowNode(nodes, "summary")
	if !ok || summary["status"] != "skipped" {
		t.Fatalf("summary=%#v", summary)
	}
}

func flowNode(nodes map[string]any, id string) (map[string]any, bool) {
	for path, raw := range nodes {
		if strings.HasSuffix(path, "/"+id) {
			node, ok := raw.(map[string]any)
			return node, ok
		}
	}
	return nil, false
}

func TestProductionFlowEvaluationReviewIntakeRequiresPullRequest(t *testing.T) {
	workspace, bin := t.TempDir(), t.TempDir()
	marker := filepath.Join(t.TempDir(), "gh-called")
	writeFile(t, bin, "gh", "#!/bin/sh\ntouch \"$FAKE_GH_SHIM_MARKER\"\n")
	if err := os.Chmod(filepath.Join(bin, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary(t, "takt-fake-code-agent"))
	cmd.Env = append(os.Environ(), "FAKE_FLOW_EVAL_SMOKE=1", "TAKT_WORKSPACE="+workspace, "FAKE_GH_SHIM_MARKER="+marker, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader("TAKT_PHASE: review-intake\n{\"repository\":\"example/mini-du\",\"fixes_permitted\":true}\n")
	if err := cmd.Run(); err == nil {
		t.Fatal("review-intake without pull_request unexpectedly succeeded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("review-intake invoked gh without pull_request: %v", err)
	}
}

func writeProductionFlowSuite(t *testing.T, root, selector, require string, pullRequest int, input, fake string, fakeEnv ...map[string]string) string {
	t.Helper()
	caseRoot := filepath.Join(root, "cases", "smoke")
	writeFile(t, caseRoot, "input.md", input)
	writeFile(t, caseRoot, "expected.yaml", "oracle: {}\n")
	writeFile(t, caseRoot, "workspace/go.mod", "module example.test/flow-smoke\n\ngo 1.23\n")
	writeFile(t, caseRoot, "workspace/smoke_test.go", "package smoke\n\nimport \"testing\"\n\nfunc TestSmoke(t *testing.T) {}\n")
	writeFile(t, caseRoot, "workspace/head.txt", "baseline\n")
	writeFile(t, caseRoot, "scm/repository.yaml", "repository: example/mini-du\nbase_branch: main\nhead_branch: fixture-head\n")
	if require == "pull_request" {
		writeFile(t, caseRoot, "scm/pull-request.yaml", fmt.Sprintf("number: %d\ntitle: Fixture pull request\nbase: main\nhead: fixture-head\nstate: OPEN\nci_status: passed\nfixes_permitted: true\n", pullRequest))
		writeFile(t, caseRoot, "scm/head.patch", "diff --git a/head.txt b/head.txt\nindex e69de29..a77fa51 100644\n--- a/head.txt\n+++ b/head.txt\n@@ -1 +1 @@\n-baseline\n+fixture head\n")
	}
	env := "      FAKE_FLOW_EVAL_SMOKE: \"1\"\n"
	if len(fakeEnv) > 0 {
		for key, value := range fakeEnv[0] {
			env += fmt.Sprintf("      %s: %q\n", key, value)
		}
	}
	config := writeFile(t, root, "config.yaml", fmt.Sprintf("apiVersion: takt/v1alpha1\nkind: Config\ndefault_assistant: fixture\nmodels:\n  implementation: {provider: fixture, id: implementation}\n  review: {provider: fixture, id: review}\n  routing: {provider: fixture, id: routing}\nassistants:\n  fixture:\n    type: process\n    argv: [%q]\n    env:\n%s    capabilities: [tool_policy, skills, sandbox_filesystem]\n", fake, env))
	_ = config
	return writeFile(t, root, "suite.yaml", fmt.Sprintf("version: takt-flow-evaluation/v1alpha1\nworkflow: %s\nconfig: config.yaml\ncases: {directory: cases}\napprovals: {default: approved}\nexternal: {github: {mode: fixture, require: %s}}\nvalidator:\n  id: production-flow-e2e\n  version: '1'\n  command: [%q, %q]\n  path: %q\n  timeout: 30s\n  max_output_bytes: 1048576\ngates: {valid_rate: {min: 1}}\n", selector, require, os.Args[0], "-test.run=^TestProductionFlowEvaluation$", os.Args[0]))
}

func requireGHCall(t *testing.T, path string, want []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Join(unquoteGHCall(t, line), "\x00") == strings.Join(want, "\x00") {
			return
		}
	}
	t.Fatalf("missing gh argv %v in %q", want, data)
}

func unquoteGHCall(t *testing.T, line string) []string {
	t.Helper()
	var fields []string
	for len(strings.TrimSpace(line)) > 0 {
		line = strings.TrimSpace(line)
		if line[0] != '\'' {
			fields = append(fields, strings.Fields(line)...)
			break
		}
		end := strings.Index(line[1:], "'")
		if end < 0 {
			t.Fatalf("invalid fake gh call %q", line)
		}
		fields = append(fields, line[1:end+1])
		line = line[end+2:]
	}
	for i := range fields {
		fields[i] = strings.ReplaceAll(fields[i], `\,`, ",")
	}
	return fields
}

func requireEvidenceArtifact(t *testing.T, evidence, baseName string) {
	t.Helper()
	if !evidenceArtifactPresent(t, evidence, baseName) {
		t.Fatalf("artifact %q absent from evidence", baseName)
	}
}

func evidenceArtifactPresent(t *testing.T, evidence, baseName string) bool {
	t.Helper()
	manifest := decodeJSONFile(t, filepath.Join(evidence, "artifacts", "manifest.json"))
	for _, raw := range manifest["artifacts"].([]any) {
		artifact := raw.(map[string]any)
		if strings.HasSuffix(artifact["source_path"].(string), "/"+baseName) {
			return true
		}
	}
	return false
}

func TestFlowInventory(t *testing.T) {
	paths := map[string][]string{
		"feature-development.yaml":     {"implement", "validate-agent", "initial-verdict", "repair", "revalidate-agent", "revalidation-verdict", "review-acceptance-gate", "validate", "create-pr", "pr-effect-gate", "summary"},
		"comprehensive-pr-review.yaml": {"review", "summary"},
		"review-block.yaml":            {"scope", "perspectives", "reviews", "synthesize", "fixes", "validate", "post-review-validation-commands", "review-acceptance-gate"},
		"review-perspective.yaml":      {"review"},
		"architect.yaml":               {"sweep", "approve", "plan", "implement", "review", "summary"},
		"smart-review-block.yaml":      {"scope", "classify", "reviews", "synthesize", "fixes", "validate"},
	}
	loaded := map[string]*spec.Workflow{}
	for name, want := range paths {
		wf, err := workflow.Load(filepath.Join(repoRoot, "internal", "profile", "builtin", "code", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		loaded[name] = wf
		var got []string
		for _, node := range wf.Nodes {
			if !node.Hidden && node.PublicParent == "" {
				got = append(got, node.ID)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s nodes=%v want=%v", name, got, want)
		}
	}
	for _, tc := range []struct {
		name  string
		files []string
		want  []string
	}{
		{"feature", []string{"feature-development.yaml"}, []string{"implementation", "review"}},
		{"comprehensive", []string{"comprehensive-pr-review.yaml", "review-block.yaml", "review-perspective.yaml"}, []string{"review"}},
		{"architect", []string{"architect.yaml", "smart-review-block.yaml", "review-perspective.yaml"}, []string{"implementation", "review", "routing"}},
	} {
		models := map[string]bool{}
		for _, name := range tc.files {
			for _, node := range loaded[name].Nodes {
				if (node.Command != "" || node.Prompt != "") && node.Model != "" {
					models[node.Model] = true
				}
			}
		}
		got := make([]string, 0, len(models))
		for model := range models {
			got = append(got, model)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("%s models=%v want=%v", tc.name, got, tc.want)
		}
	}
	var featureImplement *spec.Node
	for index := range loaded["feature-development.yaml"].Nodes {
		node := &loaded["feature-development.yaml"].Nodes[index]
		if node.ID == "implement" {
			featureImplement = node
			break
		}
	}
	if featureImplement == nil || featureImplement.Attempts.Max != 3 || len(featureImplement.Attempts.RetryOn) != 1 || featureImplement.Attempts.RetryOn[0] != "exit" || featureImplement.Attempts.RetrySession != "reuse" {
		t.Fatalf("feature implement retry policy=%+v", featureImplement)
	}
	if len(featureImplement.Hooks.AfterNode) != 1 || featureImplement.Hooks.AfterNode[0].OnFailure.Action != "retry" || featureImplement.Hooks.AfterNode[0].OnFailure.Session != "resume" {
		t.Fatalf("feature implementation artifact hook=%+v", featureImplement.Hooks.AfterNode)
	}
	var featurePREffect *spec.Node
	for index := range loaded["feature-development.yaml"].Nodes {
		node := &loaded["feature-development.yaml"].Nodes[index]
		if node.ID == "pr-effect-gate" {
			featurePREffect = node
			break
		}
	}
	if featurePREffect == nil {
		t.Fatal("feature pr-effect-gate missing")
	}
	if featurePREffect.TriggerRule != "all_done" {
		t.Fatalf("feature pr-effect-gate trigger_rule=%q", featurePREffect.TriggerRule)
	}
	if !strings.Contains(featurePREffect.Bash, "$create-pr.status?") {
		t.Fatalf("feature pr-effect-gate does not require create-pr completion: %q", featurePREffect.Bash)
	}
	var featureReviewGate *spec.Node
	for index := range loaded["feature-development.yaml"].Nodes {
		node := &loaded["feature-development.yaml"].Nodes[index]
		if node.ID == "review-acceptance-gate" {
			featureReviewGate = node
			break
		}
	}
	if featureReviewGate == nil || !strings.Contains(featureReviewGate.Bash, "$ARTIFACTS_DIR/validation.md") {
		t.Fatalf("feature review gate does not re-check validation.md: %+v", featureReviewGate)
	}
	command, err := runtime.NewCommandResolver(filepath.Join(repoRoot, "internal", "profile", "builtin", "code", "workflows", "review-block.yaml"), repoRoot, repoRoot).Resolve("review-intake")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(command.Body, "$ARGUMENTS") != 1 {
		t.Fatalf("review-intake $ARGUMENTS count=%d", strings.Count(command.Body, "$ARGUMENTS"))
	}
}

func decodeJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
