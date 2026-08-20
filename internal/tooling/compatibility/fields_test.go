package compatibility

import "testing"

func TestFieldAuditCoversExactPublicFields(t *testing.T) {
	if mismatches := auditedFieldMismatches(); len(mismatches) != 0 {
		for _, mismatch := range mismatches {
			t.Error(mismatch)
		}
	}
}

func TestFieldMatrixKeepsAlphaSeamsOutOfStableCore(t *testing.T) {
	matrix := CurrentFieldMatrix()
	byKey := map[string]FieldDecision{}
	for _, field := range matrix.Fields {
		byKey[field.Contract+"."+field.Field] = field
	}
	for _, key := range []string{"Node.executor", "Node.native_hooks", "Node.tool_approval"} {
		if got := byKey[key]; got.Support != "supported-alpha" || got.Decision != "defer" {
			t.Fatalf("%s=%+v", key, got)
		}
	}
	if got := byKey["OutputFormat.type"]; got.Support != "stable-candidate" || got.Decision != "keep" {
		t.Fatalf("OutputFormat.type=%+v", got)
	}
}

func TestFieldMatrixIncludesUnifiedEvaluationAuthoringContracts(t *testing.T) {
	matrix := CurrentFieldMatrix()
	byKey := map[string]FieldDecision{}
	for _, field := range matrix.Fields {
		byKey[field.Contract+"."+field.Field] = field
	}
	for _, key := range []string{
		"Node.matrix", "Node.assessment", "MatrixSpec.items_from", "MatrixSpec.nodes",
		"AssessmentSpec.role", "AssessmentSpec.target_run_id", "WorkflowRunSpec.keep_worktree", "ScriptSpec.stdin",
	} {
		if got := byKey[key]; got.Support != "stable-candidate" || got.Decision != "keep" {
			t.Fatalf("%s=%+v", key, got)
		}
	}
}
