package evaluation

import (
	"fmt"
	"os"
	"path/filepath"
)

func InitFlowSuite(workflowSelector, output string) error {
	return initEvaluationScaffold(workflowSelector, output, true)
}

func InitEvaluationWorkflow(workflowSelector, output string) error {
	return initEvaluationScaffold(workflowSelector, output, false)
}

func initEvaluationScaffold(workflowSelector, output string, legacy bool) error {
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("flow evaluation output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(output, "cases", "example", "workspace"), 0o755); err != nil {
		return err
	}
	files := map[string]string{}
	if legacy {
		files["suite.yaml"] = "version: takt-flow-evaluation/v1alpha1\nworkflow: " + workflowSelector + "\nconfig: config.yaml\ncases:\n  directory: cases\nvalidator:\n  id: replace-me\n  version: \"1\"\n  command: [./validator]\n  path: ./validator\n  timeout: 2m\n  max_output_bytes: 1048576\ngates:\n  validation_error_rate: {max: 0}\n"
	} else {
		files["workflows/evaluate.yaml"] = `name: evaluation
description: Authored evaluation workflow; replace the scaffold validator and case setup.
input:
  format: json
  schema:
    type: object
    additionalProperties: true
    properties:
      cases:
        type: array
        minItems: 1
        items:
          type: object
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: candidate
          workflow:
            path: $MATRIX.item.workflow_path
            repository: $MATRIX.item.repository
            input: $MATRIX.item.input
            isolation: worktree
            keep_worktree: true
          allow_failure: true
        - id: validate
          depends_on: [candidate]
          trigger_rule: all_done
          script:
            runtime: command
            path: ../tools/validate.sh
            stdin: |
              {
                "case_id": "$MATRIX.item.case_id",
                "repeat": $MATRIX.item.repeat,
                "workspace": "$candidate.child_execution_workspace",
                "baseline_workspace": "$MATRIX.item.baseline_path",
                "expected_path": "$MATRIX.item.expected_path",
                "run_id": "$candidate.child_run_id",
                "run_status": "$candidate.status"
              }
          output_format:
            type: object
            additionalProperties: false
            properties:
              protocol_version: {type: string}
              type: {type: string}
              valid: {type: boolean}
              diagnostics:
                type: array
                items: {type: object}
            required: [protocol_version, type, valid]
          allow_failure: true
        - id: evidence
          depends_on: [candidate, validate]
          trigger_rule: all_done
          script:
            runtime: command
            path: ../tools/collect-evidence.sh
            args:
              - --workspace
              - $candidate.child_execution_workspace
              - --base
              - $candidate.child_base_commit
              - --output
              - $ARTIFACTS_DIR/evaluation-evidence.txt
          output_type: evaluation-evidence
          output_mime: text/plain
          output_path: $ARTIFACTS_DIR/evaluation-evidence.txt
        - id: assess
          depends_on: [validate, evidence]
          trigger_rule: all_done
          assessment:
            role: primary
            target_run_id: $candidate.child_run_id
            result_from: $validate.output
            scope:
              case_id: $MATRIX.item.case_id
              repeat: $MATRIX.item.repeat
            evidence: [$evidence.artifacts.evaluation-evidence]
      output_node: assess
`
		files["README.md"] = "# Authored evaluation scaffold\n\n1. Create or copy config.yaml with the assistants/models required by the target workflow.\n2. Replace tools/validate.sh, tools/collect-evidence.sh and the example case with deterministic project-specific implementations and data.\n3. Run `takt eval flow workflows/evaluate.yaml --target " + workflowSelector + " --config config.yaml --cases cases`.\n\nThe Route/micro DSL and evaluation contracts remain public OSS surfaces.\n"
		files["tools/validate.sh"] = "#!/bin/sh\nset -eu\nprintf '%s\\n' '{\"protocol_version\":\"takt-validation/v1alpha1\",\"type\":\"validation_result\",\"valid\":false,\"diagnostics\":[{\"code\":\"SCAFFOLD_VALIDATOR\",\"severity\":\"warning\",\"message\":\"Replace tools/validate.sh with the deterministic project validator.\"}]}'\n"
		files["tools/collect-evidence.sh"] = "#!/bin/sh\nset -eu\nworkspace=$2\nbase=$4\noutput=$6\nmkdir -p \"$(dirname \"$output\")\"\nprintf 'workspace=%s\\nbase=%s\\n%s\\n' \"$workspace\" \"$base\" 'Replace this evidence collector with the project-specific deterministic evidence.' > \"$output\"\n"
	}
	files["cases/example/input.md"] = "Describe the task for the selected production workflow.\n"
	files["cases/example/expected.yaml"] = "oracle: {}\n"
	files["cases/example/workspace/README.md"] = "Replace this directory with the complete initial repository for this case.\n"
	for name, content := range files {
		path := filepath.Join(output, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if name == "tools/validate.sh" || name == "tools/collect-evidence.sh" {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			return err
		}
	}
	return nil
}
