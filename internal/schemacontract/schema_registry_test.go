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

func TestEvaluationSchemasValidateFlowFixtures(t *testing.T) {
	sha := strings.Repeat("a", 64)
	metric := map[string]any{"baseline": nil, "candidate": nil, "delta": nil, "delta_percent": nil}
	flow := map[string]any{"evaluated_runs": 0, "flow_completed": 0, "true_accept": 0, "false_accept": 0, "true_reject": 0, "false_reject": 0, "validation_errors": 0, "valid_rate": nil, "false_accept_rate": nil, "false_reject_rate": nil, "flow_completion_rate": nil, "validation_error_rate": nil}
	summary := map[string]any{"total": 0, "by_status": map[string]any{}, "attempts": 0, "input_tokens": 0, "output_tokens": 0, "cost": 0, "duration_ms": 0, "answers": 0, "truncated_nodes": 0, "resumed_nodes": 0, "by_assistant": map[string]any{}, "by_assistant_version": map[string]any{}, "by_requested_model": map[string]any{}, "by_resolved_model": map[string]any{}, "usage_by_execution_identity": map[string]any{}, "mixed_execution_identity_nodes": 0, "quality_runs": 0, "valid": 0, "invalid": 0, "valid_at_first_attempt": 0, "scored_runs": 0, "success_at_1": nil, "final_success_rate": nil, "average_attempts_to_valid": nil, "average_score": nil, "cost_per_valid": nil, "amortized_end_to_end_ms_per_valid": nil, "diagnostics_by_severity": map[string]any{}, "diagnostics_by_code": map[string]any{}, "diagnostics_by_fingerprint": map[string]any{}, "average_time_to_valid_ms": nil, "retry_scheduled": 0, "failed_executions": 0, "failed_execution_cost": 0, "stable_valid_cases": 0, "stable_invalid_cases": 0, "unstable_cases": 0, "flow": flow}
	flowRun := map[string]any{"case_id": "case", "repeat": 1, "status": "completed", "workspace": "workspace", "duration_ms": 0, "attempts": 0, "attempts_to_valid": nil, "valid_at_first_attempt": false, "input_tokens": 0, "output_tokens": 0, "cost": 0, "answers": 0, "truncated_nodes": 0, "resumed_nodes": 0, "mixed_execution_identity_nodes": 0, "quality_expected": true, "time_to_valid_ms": nil, "retry_scheduled": 0, "nodes": map[string]any{}, "mode": "flow", "validation": map[string]any{"status": "completed", "duration_ms": 0, "result": map[string]any{"protocol_version": "takt-validation/v1alpha1", "type": "validation_result", "valid": true}}, "outcome": "true_accept", "run_passed": true, "cleanup": map[string]any{"status": "completed", "paths": []any{"workspace"}}}
	report := map[string]any{"report_version": "takt-evaluation/v1alpha1", "takt_version": "test", "started_at": "2026-01-01T00:00:00Z", "finished_at": "2026-01-01T00:00:00Z", "duration_ms": 0, "workflow": "w", "config": "c", "cases_dir": "cases", "output_dir": "out", "mode": "flow", "strategy": map[string]any{"id": "s", "fingerprint": sha, "workflow_fingerprint": sha, "config_fingerprint": sha, "commands_fingerprint": sha}, "benchmark": map[string]any{"id": "b", "fingerprint": sha, "dataset_fingerprint": sha, "workspace_fingerprint": sha, "case_count": 1, "validator": map[string]any{}}, "environment": map[string]any{"goos": "x", "goarch": "x", "go_version": "x", "path_sha256": sha, "oracle_metadata_sha256": sha}, "runs": []any{flowRun}, "summary": summary}
	comparisonCase := map[string]any{"case_id": "case", "repeat": 1, "baseline_valid": false, "candidate_valid": true, "transition": "candidate_only_valid", "baseline_time_to_valid_ms": nil, "candidate_time_to_valid_ms": 0, "baseline_outcome": "false_accept", "candidate_outcome": "true_accept"}
	compare := map[string]any{"report_version": "takt-evaluation-compare/v1alpha1", "benchmark": map[string]any{"id": "b", "fingerprint": sha}, "baseline": map[string]any{"id": "a", "fingerprint": sha}, "candidate": map[string]any{"id": "b", "fingerprint": sha}, "metrics": map[string]any{"success_at_1": metric, "final_success_rate": metric, "average_attempts_to_valid": metric, "average_score": metric, "cost_per_valid": metric, "average_time_to_valid_ms": metric, "flow": map[string]any{"valid_rate": metric, "false_accept_rate": metric, "false_reject_rate": metric, "flow_completion_rate": metric, "validation_error_rate": metric}}, "paired_outcomes": map[string]any{"both_valid": 0, "baseline_only_valid": 0, "candidate_only_valid": 1, "both_invalid": 0}, "cases": []any{comparisonCase}}
	for name, fixture := range map[string]any{"evaluation-report.schema.json": report, "evaluation-compare.schema.json": compare} {
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
