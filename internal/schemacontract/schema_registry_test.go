package schemacontract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestSchemaRegistryIsOfflineAndDocumented(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	schemaDir := filepath.Join(root, "schemas")
	entries, err := filepath.Glob(filepath.Join(schemaDir, "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no schemas found")
	}
	readmeBytes, err := os.ReadFile(filepath.Join(schemaDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(readmeBytes)
	actual := map[string]bool{}
	for _, path := range entries {
		name := filepath.Base(path)
		actual[name] = true
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("%s: invalid JSON: %v", name, err)
		}
		rootObject, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s: root must be object", name)
		}
		if rootObject["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatalf("%s: expected Draft 2020-12 $schema", name)
		}
		walkRefs(t, name, value)
		if !strings.Contains(readme, "`"+name+"`") {
			t.Fatalf("schemas/README.md missing %s", name)
		}
	}
	re := regexp.MustCompile("`([^`]+\\.schema\\.json)`")
	var stale []string
	for _, match := range re.FindAllStringSubmatch(readme, -1) {
		if !actual[match[1]] {
			stale = append(stale, match[1])
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("schemas/README.md contains stale entries: %s", strings.Join(stale, ", "))
	}
}

func TestWorkflowSchemaValidatesHookFailureSessions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "workflow.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("workflow.schema.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("workflow.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{
		"name": "hook-session",
		"nodes": []any{map[string]any{
			"id": "run", "bash": "true",
			"hooks": map[string]any{"after_node": []any{map[string]any{
				"bash":       "true",
				"on_failure": map[string]any{"action": "retry", "session": "fresh"},
			}}},
		}},
	}
	for _, session := range []string{"fresh", "resume"} {
		fixture := cloneJSONMap(base)
		hook := fixture["nodes"].([]any)[0].(map[string]any)["hooks"].(map[string]any)["after_node"].([]any)[0].(map[string]any)
		hook["on_failure"].(map[string]any)["session"] = session
		if err := schema.Validate(fixture); err != nil {
			t.Fatalf("retry + %s rejected: %v", session, err)
		}
	}
	for name, mutate := range map[string]func(map[string]any){
		"retry reuse":    func(h map[string]any) { h["session"] = "reuse" },
		"continue fresh": func(h map[string]any) { h["action"] = "continue" },
		"fail resume":    func(h map[string]any) { h["action"], h["session"] = "fail", "resume" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := cloneJSONMap(base)
			hook := fixture["nodes"].([]any)[0].(map[string]any)["hooks"].(map[string]any)["after_node"].([]any)[0].(map[string]any)["on_failure"].(map[string]any)
			mutate(hook)
			if err := schema.Validate(fixture); err == nil {
				t.Fatal("expected schema rejection")
			}
		})
	}
}

func TestEvaluationSchemasValidateFlowFixtures(t *testing.T) {
	sha := strings.Repeat("a", 64)
	metric := map[string]any{"baseline": nil, "candidate": nil, "delta": nil, "delta_percent": nil}
	flow := map[string]any{"evaluated_runs": 0, "flow_completed": 0, "true_accept": 0, "false_accept": 0, "true_reject": 0, "false_reject": 0, "validation_errors": 0, "infrastructure_errors": 0, "valid_rate": nil, "false_accept_rate": nil, "false_reject_rate": nil, "flow_completion_rate": nil, "validation_error_rate": nil}
	summary := map[string]any{"total": 0, "by_status": map[string]any{}, "attempts": 0, "infrastructure_errors": 0, "input_tokens": 0, "output_tokens": 0, "cost": 0, "duration_ms": 0, "answers": 0, "truncated_nodes": 0, "resumed_nodes": 0, "by_assistant": map[string]any{}, "by_assistant_version": map[string]any{}, "by_requested_model": map[string]any{}, "by_resolved_model": map[string]any{}, "usage_by_execution_identity": map[string]any{}, "mixed_execution_identity_nodes": 0, "quality_runs": 0, "valid": 0, "invalid": 0, "valid_at_first_attempt": 0, "scored_runs": 0, "success_at_1": nil, "final_success_rate": nil, "average_attempts_to_valid": nil, "average_score": nil, "cost_per_valid": nil, "amortized_end_to_end_ms_per_valid": nil, "diagnostics_by_severity": map[string]any{}, "diagnostics_by_code": map[string]any{}, "diagnostics_by_fingerprint": map[string]any{}, "average_time_to_valid_ms": nil, "retry_scheduled": 0, "failed_executions": 0, "failed_execution_cost": 0, "stable_valid_cases": 0, "stable_invalid_cases": 0, "unstable_cases": 0, "flow": flow}
	flowRun := map[string]any{"case_id": "case", "repeat": 1, "status": "completed", "workspace": "workspace", "duration_ms": 0, "attempts": 1, "attempts_to_valid": nil, "valid_at_first_attempt": false, "input_tokens": 0, "output_tokens": 0, "cost": 0, "answers": 0, "truncated_nodes": 0, "resumed_nodes": 0, "mixed_execution_identity_nodes": 0, "quality_expected": true, "time_to_valid_ms": nil, "retry_scheduled": 0, "nodes": map[string]any{"run/implement": map[string]any{"status": "completed", "attempts": 1, "duration_ms": 100, "resumed": false, "exit_code": 0, "output_truncated": false, "mixed_execution_identity": false, "executions": []any{}}}, "mode": "flow", "validation": map[string]any{"status": "completed", "duration_ms": 0, "result": map[string]any{"protocol_version": "takt-validation/v1alpha1", "type": "validation_result", "valid": true}}, "outcome": "true_accept", "run_passed": true, "cleanup": map[string]any{"status": "completed", "paths": []any{"workspace"}}}
	report := map[string]any{"report_version": "takt-evaluation/v1alpha1", "takt_version": "test", "started_at": "2026-01-01T00:00:00Z", "finished_at": "2026-01-01T00:00:00Z", "duration_ms": 0, "workflow": "w", "config": "c", "cases_dir": "cases", "output_dir": "out", "mode": "flow", "strategy": map[string]any{"id": "s", "fingerprint": sha, "workflow_fingerprint": sha, "config_fingerprint": sha, "commands_fingerprint": sha}, "benchmark": map[string]any{"id": "b", "fingerprint": sha, "dataset_fingerprint": sha, "workspace_fingerprint": sha, "case_count": 1, "validator": map[string]any{}}, "environment": map[string]any{"goos": "x", "goarch": "x", "go_version": "x", "path_sha256": sha, "oracle_metadata_sha256": sha}, "runs": []any{flowRun}, "summary": summary}
	comparisonCase := map[string]any{"case_id": "case", "repeat": 1, "baseline_valid": false, "candidate_valid": true, "transition": "candidate_only_valid", "baseline_time_to_valid_ms": nil, "candidate_time_to_valid_ms": 0, "baseline_outcome": "false_accept", "candidate_outcome": "true_accept"}
	compare := map[string]any{"report_version": "takt-evaluation-compare/v1alpha1", "benchmark": map[string]any{"id": "b", "fingerprint": sha}, "baseline": map[string]any{"id": "a", "fingerprint": sha}, "candidate": map[string]any{"id": "b", "fingerprint": sha}, "baseline_output_dir": "evals/a", "candidate_output_dir": "evals/b", "metrics": map[string]any{"success_at_1": metric, "final_success_rate": metric, "input_tokens": metric, "output_tokens": metric, "total_tokens": metric, "total_attempts": metric, "total_duration_ms": metric, "average_attempts_to_valid": metric, "average_score": metric, "cost_per_valid": metric, "average_time_to_valid_ms": metric, "flow": map[string]any{"valid_rate": metric, "false_accept_rate": metric, "false_reject_rate": metric, "flow_completion_rate": metric, "validation_error_rate": metric}}, "paired_outcomes": map[string]any{"both_valid": 0, "baseline_only_valid": 0, "candidate_only_valid": 1, "both_invalid": 0}, "cases": []any{comparisonCase}}
	stats := map[string]any{"report_version": "takt-evaluation-stats/v1alpha1", "status": "completed", "complete": true, "mode": "flow", "workflow": "w", "output_dir": "out", "started_at": "2026-01-01T00:00:00Z", "finished_at": "2026-01-01T00:00:00Z", "strategy": report["strategy"], "benchmark": report["benchmark"], "total": 1, "valid": 1, "invalid": 0, "attempts": 1, "assistant_executions": 1, "retry_scheduled": 0, "resumed_nodes": 0, "infrastructure_errors": 0, "input_tokens": 10, "output_tokens": 5, "total_tokens": 15, "duration_ms": 100, "cost": 0, "average_time_to_valid_ms": 100, "usage_by_execution_identity": map[string]any{}, "flow": flow, "outcomes": map[string]any{"true_accept": 1}, "diagnostics": map[string]any{}, "cases": []any{map[string]any{"case_id": "case", "repeat": 1, "status": "completed", "outcome": "true_accept", "cause_source": "validator", "cause": "invalid: product check failed", "attempts": 1, "input_tokens": 10, "output_tokens": 5, "duration_ms": 100, "time_to_valid_ms": 100}}, "assistant_steps": []any{map[string]any{"case_id": "case", "repeat": 1, "step": "implement", "model": "implementation", "executions": 1, "input_tokens": 10, "output_tokens": 5, "duration_ms": 100}}, "assistant_sessions": []any{map[string]any{"case_id": "case", "repeat": 1, "step": "implement", "attempt": 1, "provider_attempt": 1, "resumed": false, "session_id": "session-1"}}, "total_runs": 1, "completed_runs": 1, "current": map[string]any{"case_id": "case", "repeat": 1, "ordinal": 1, "phase": "finalized"}, "timings": map[string]any{"phases": map[string]any{"prepare_ms": 0, "validator_preflight_ms": 0, "workflow_ms": 100, "validator_ms": 0, "evidence_ms": 0, "cleanup_ms": 0}, "assistant": map[string]any{"wait_ms": 0, "stream_ms": 0, "total_ms": 0, "tool_ms": 0}}}
	progress := map[string]any{"report_version": "takt-flow-evaluation-progress/v1alpha1", "status": "running", "suite": "suite.yaml", "workflow": "flow", "output_dir": "out", "started_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:01Z", "total_runs": 1, "completed_runs": 0, "current": map[string]any{"case_id": "case", "repeat": 1, "ordinal": 1, "phase": "workflow", "phase_started_at": "2026-01-01T00:00:00Z"}, "runtime": map[string]any{"run_id": "run-1", "status": "running", "total_nodes": 2, "completed_nodes": 1, "running_nodes": []any{"implement"}, "node_attempts": 1, "provider_attempts": 1, "input_tokens": 10, "output_tokens": 5, "cost": 0, "timings": map[string]any{"phases": map[string]any{"prepare_ms": 1, "validator_preflight_ms": 2, "workflow_ms": 3, "validator_ms": 4, "evidence_ms": 5, "cleanup_ms": 6}, "assistant": map[string]any{"wait_ms": 7, "stream_ms": 8, "total_ms": 9, "tool_ms": 10}}}, "results": map[string]any{"valid": 0, "invalid": 0, "infrastructure_errors": 0, "validation_errors": 0}}
	inspection := map[string]any{"report_version": "takt-evaluation-inspection/v1alpha1", "output_dir": "out", "workflow": "w", "cases": []any{map[string]any{"case_id": "case", "repeat": 1, "run_id": "run-1", "status": "completed", "outcome": "false_accept", "reported_cause": map[string]any{"confidence": "CONFIRMED", "source": "validator", "message": "invalid: product check failed"}, "non_completed_nodes": []any{map[string]any{"id": "validate", "status": "failed", "error_code": "exit", "error": "exit 2"}}, "evidence": map[string]any{"root": "cases/case/repeat-001", "run": "cases/case/repeat-001/run.json", "validation": "cases/case/repeat-001/validation-result.json", "diff": "cases/case/repeat-001/diff.patch", "diff_bytes": 0, "source": "cases/case/repeat-001/source", "source_present": true, "repository_bundle": "cases/case/repeat-001/repository.bundle", "activity": "cases/case/repeat-001/activity.json", "scm_calls_path": "cases/case/repeat-001/scm/calls.log", "scm_calls_recorded": true, "scm_calls": 1, "artifacts": []any{"files/run-1/pr-url.txt"}}, "causal_chain": []any{map[string]any{"code": "assistant_output_limit", "confidence": "CONFIRMED", "message": "node implement reached model output limit", "evidence": "cases/case/repeat-001/run.json"}}, "observations": []any{map[string]any{"code": "control_workspace_mutation", "confidence": "CONFIRMED", "message": "assistant mutation targeted control workspace instead of execution workspace", "evidence": "cases/case/repeat-001/activity.json"}}}}}
	for name, fixture := range map[string]any{"evaluation-report.schema.json": report, "evaluation-compare.schema.json": compare, "evaluation-stats.schema.json": stats, "evaluation-inspection.schema.json": inspection, "flow-evaluation-progress.schema.json": progress} {
		data, err := os.ReadFile(filepath.Join("..", "..", "schemas", name))
		if err != nil {
			t.Fatal(err)
		}
		compiler := jsonschema.NewCompiler()
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource(name, document); err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(fixture); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestEvaluationAnalysisSchemaValidatesStrictReport(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "evaluation-analysis.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("evaluation-analysis.schema.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("evaluation-analysis.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{
		"report_version": "takt-evaluation-analysis/v1alpha1", "output_dir": "analyses/2026", "source_evaluation_dir": "evaluation", "status": "completed",
		"started_at": "2026-01-01T00:00:00Z", "finished_at": "2026-01-01T00:00:01Z", "duration_ms": 1000, "language": "ru",
		"model":          map[string]any{"preset": "default", "alias": "takt_analyze", "provider": "example", "id": "analysis-model"},
		"selected_cases": []any{map[string]any{"case_id": "case", "repeat": 1}},
		"analyses": []any{map[string]any{
			"case_id": "case", "repeat": 1,
			"deterministic":   map[string]any{"status": "completed", "outcome": "false_reject", "cause_source": "validator", "cause": "invalid"},
			"analysis_status": "completed",
			"analysis": map[string]any{
				"primary_class": "validator", "failure_mode": "bad_assertion", "confidence": "high", "root_cause": "validator mismatch",
				"causal_mechanism": "the validator applied an assertion that did not match the task contract", "failure_point": "validator", "prevention": "align the assertion with the task contract",
				"causal_chain": []any{map[string]any{"fact": "validator rejected", "consequence": "run failed", "evidence": []any{"run.json#/status"}}},
				"evidence":     []any{map[string]any{"path": "run.json", "pointer": "/status", "fact": "failed"}}, "contributing_factors": []any{}, "recommended_actions": []any{"fix validator"}, "missing_evidence": []any{},
				"disagreement": map[string]any{"with_deterministic_cause": false, "explanation": "matches"},
			},
			"evidence_fingerprint": "abc", "model": map[string]any{"preset": "default", "alias": "takt_analyze", "provider": "example", "id": "analysis-model"},
			"raw_output_path": "raw-output.txt",
			"session":         map[string]any{"adapter": "fake", "session_id": "session", "session_evidence": "unavailable"}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 2, "cost": 0, "duration_ms": 3},
		}},
	}
	if err := schema.Validate(report); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"extra top-level property": func(value map[string]any) { value["extra"] = true },
		"unknown primary class": func(value map[string]any) {
			value["analyses"].([]any)[0].(map[string]any)["analysis"].(map[string]any)["primary_class"] = "random"
		},
		"localized failure mode": func(value map[string]any) {
			value["analyses"].([]any)[0].(map[string]any)["analysis"].(map[string]any)["failure_mode"] = "ошибка ассистента"
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := cloneJSONMap(report)
			mutate(copy)
			if err := schema.Validate(copy); err == nil {
				t.Fatal("expected strict schema rejection")
			}
		})
	}
	for _, status := range []string{"provider_unavailable", "timed_out", "protocol", "persistence_error", "not_run"} {
		t.Run("terminal status "+status, func(t *testing.T) {
			copy := cloneJSONMap(report)
			copy["status"] = "failed"
			caseReport := copy["analyses"].([]any)[0].(map[string]any)
			caseReport["analysis_status"] = status
			delete(caseReport, "analysis")
			caseReport["error_code"] = status
			caseReport["error"] = status + " error"
			if err := schema.Validate(copy); err != nil {
				t.Fatalf("status %s rejected: %v", status, err)
			}
		})
	}
}

func cloneJSONMap(value map[string]any) map[string]any {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(b, &clone); err != nil {
		panic(err)
	}
	return clone
}

func TestRunStateSchemaAcceptsAssistantSessionMetadata(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "run-state.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("run-state.schema.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("run-state.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := map[string]any{
		"id": "run", "status": "completed", "workflow_path": "workflow.yaml", "config_path": "config.yaml", "workspace": "workspace", "input": "", "approvals": map[string]any{}, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z", "revision": 1,
		"nodes": map[string]any{"agent": map[string]any{"status": "completed", "adapter": "pi", "session_path": "/tmp/session.jsonl", "executions": []any{map[string]any{"attempt": 1, "status": "completed", "adapter": "pi", "session_path": "/tmp/session.jsonl"}}}},
	}
	if err := schema.Validate(fixture); err != nil {
		t.Fatal(err)
	}
}

func walkRefs(t *testing.T, schemaName string, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$ref" {
				ref, ok := child.(string)
				if !ok {
					t.Fatalf("%s: $ref must be string", schemaName)
				}
				if !strings.HasPrefix(ref, "#") {
					t.Fatalf("%s: external/cross-file $ref is forbidden: %s", schemaName, ref)
				}
			}
			walkRefs(t, schemaName, child)
		}
	case []any:
		for _, child := range v {
			walkRefs(t, schemaName, child)
		}
	}
}
