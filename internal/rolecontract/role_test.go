package rolecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileBuildsBoundedBriefAndScope(t *testing.T) {
	role := Definition{
		Purpose: "implement narrowly", Session: "fresh",
		Context: ContextRecipe{Include: []string{"prior_results", "signals", "scope"}, MaxChars: 12},
		Paths:   PathScope{Allowed: []string{"**"}, Protected: []string{"api/**"}, Forbidden: []string{".takt/**"}},
	}
	brief, err := Compile("implementer", role, "Change internal/foo/bar.go safely", "fix bug", []string{"regression"}, map[string]string{"inspect": "0123456789abcdef"}, []Check{{Name: "validation", Path: "passed", Level: CheckRequired, Reaction: ReactionRepair}})
	if err != nil {
		t.Fatal(err)
	}
	if brief.Role != "implementer" || brief.Goal == "" || len(brief.Scope.Expected) != 1 || brief.Scope.Expected[0] != "internal/foo/bar.go" {
		t.Fatalf("brief = %#v", brief)
	}
	prior := brief.Context["prior_results"].(map[string]string)
	if len(prior["inspect"]) != 12 {
		t.Fatalf("prior result was not bounded: %#v", prior)
	}
	raw := EncodeBrief(brief)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("brief must remain JSON: %v", err)
	}
}

func TestEvaluateRequiredPreferredAndScope(t *testing.T) {
	checks := []Check{
		{Name: "required", Path: "passed", Level: CheckRequired, Reaction: ReactionRepair},
		{Name: "preferred", Path: "quality", Level: CheckPreferred, Reaction: ReactionWarn},
	}
	results, err := Evaluate(`{"passed":false,"quality":true,"changed_files":[".takt/private","api/v1.go"]}`, checks)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Passed || !results[1].Passed {
		t.Fatalf("results = %#v", results)
	}
	forbidden, err := ForbiddenChanges(`{"changed_files":[".takt/private","api/v1.go"]}`, PathScope{Forbidden: []string{".takt/**"}})
	if err != nil || len(forbidden) != 1 || forbidden[0] != ".takt/private" {
		t.Fatalf("forbidden = %#v err=%v", forbidden, err)
	}
	protected, err := ProtectedChanges(`{"changed_files":["api/v1.go"]}`, PathScope{Protected: []string{"api/**"}})
	if err != nil || strings.Join(protected, ",") != "api/v1.go" {
		t.Fatalf("protected = %#v err=%v", protected, err)
	}
	classified, err := ClassifyChanges(`{"changed_files":["src/ok.go","docs/extra.md","../escape","/abs/path"]}`, PathScope{Expected: []string{"src/**"}, Allowed: []string{"src/**", "docs/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(classified.Unexpected, ",") != "docs/extra.md" || len(classified.OutsideAllowed) != 0 || len(classified.Forbidden) != 2 {
		t.Fatalf("classified = %#v", classified)
	}
}
