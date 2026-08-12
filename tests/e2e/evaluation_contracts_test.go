package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	workflow := writeFile(t, root, "flow.yaml", "name: flow-e2e\nworktree:\n  enabled: true\nnodes:\n  - id: done\n    bash: 'true'\n")
	config := writeFile(t, root, "config.yaml", "apiVersion: takt/v1alpha1\nkind: Config\n")
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
	writeFile(t, root, "flow.yaml", "name: flow-e2e-failing\nworktree:\n  enabled: true\nnodes:\n  - id: done\n    bash: 'exit 1'\n")
	failingOutput := filepath.Join(t.TempDir(), "failing-output")
	takt(t, []string{"TAKT_FLOW_E2E_VALIDATOR=1"}, "eval", "flow", suite, "--output", failingOutput, "--json").RequireFailure(t).Contains(t, "flow evaluation gates failed")
	if _, err := os.Stat(filepath.Join(failingOutput, "report.json")); err != nil {
		t.Fatal(err)
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
