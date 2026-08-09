package taskroute

import (
	"testing"
)

func TestNormalizeAddsMinimumProtectedControls(t *testing.T) {
	route := Decision{Route: RouteTemplate, Reason: "change API", Confidence: 0.9, Signals: []string{"public_api"}}
	Normalize(&route, "code")
	if !route.Controls.Baseline || !route.Controls.IndependentTests || !route.Controls.EnhancedReview {
		t.Fatalf("protected controls were not added: %#v", route.Controls)
	}
}

func TestCompileSimpleReliableStandard(t *testing.T) {
	route := Decision{APIVersion: APIVersion, Kind: Kind, Route: RouteTemplate, Template: TemplateSimpleReliable, Reason: "ordinary change", Confidence: 0.9, Controls: Controls{MaxParallel: 2}}
	plan, err := Compile("change code", route, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, phase := range plan.Phases {
		got = append(got, phase.Uses)
	}
	want := []string{"investigate", "implement", "validate", "review"}
	if len(got) != len(want) {
		t.Fatalf("phases=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("phases=%v want=%v", got, want)
		}
	}
}

func TestWorkflowRouteIsProfileQualified(t *testing.T) {
	route := Decision{Route: RouteWorkflow, Workflow: "assist", Reason: "fallback", Confidence: 0.8}
	Normalize(&route, "code")
	if route.Workflow != "code:assist" {
		t.Fatalf("workflow=%q", route.Workflow)
	}
}

func TestInferSignalsIsConservative(t *testing.T) {
	got := InferSignals("Исправить регрессию в публичном API авторизации")
	set := map[string]bool{}
	for _, value := range got {
		set[value] = true
	}
	for _, want := range []string{"regression", "public_api", "security_sensitive"} {
		if !set[want] {
			t.Fatalf("missing %s in %v", want, got)
		}
	}
}

func TestInferSignalsMatchesLexemesNotSubstrings(t *testing.T) {
	for _, tc := range []struct {
		goal   string
		absent string
	}{
		{goal: "Update author metadata", absent: "security_sensitive"},
		{goal: "Improve debug logging", absent: "regression"},
	} {
		set := map[string]bool{}
		for _, value := range InferSignals(tc.goal) {
			set[value] = true
		}
		if set[tc.absent] {
			t.Fatalf("goal %q unexpectedly produced %q: %v", tc.goal, tc.absent, set)
		}
	}
}
