package cli

import (
	"reflect"
	"testing"
)

func TestModelOverrideFlagAcceptsArbitraryAliasesAndLastValueWins(t *testing.T) {
	var values modelOverrideFlag
	for _, raw := range []string{"tester=openai/gpt-5-mini", "oracle=anthropic/claude-opus-4", "tester=aihub/Qwen/Qwen3.6-27B"} {
		if err := values.Set(raw); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{"tester": "aihub/Qwen/Qwen3.6-27B", "oracle": "anthropic/claude-opus-4"}
	if !reflect.DeepEqual(map[string]string(values), want) {
		t.Fatalf("overrides=%v want=%v", values, want)
	}
}

func TestModelOverrideFlagRejectsMissingAssignment(t *testing.T) {
	var values modelOverrideFlag
	for _, raw := range []string{"tester", "=openai/model", "tester="} {
		if err := values.Set(raw); err == nil {
			t.Fatalf("expected %q to fail", raw)
		}
	}
}

func TestEnvironmentModelOverridesAreGenericAndCLIWins(t *testing.T) {
	environment, err := environmentModelOverrides([]string{
		"PATH=/bin",
		"MODEL_TESTER=openai/gpt-5-mini",
		"MODEL_ORACLE=anthropic/claude-opus-4",
		"MODEL_UNUSED=",
	})
	if err != nil {
		t.Fatal(err)
	}
	cli := modelOverrideFlag{"tester": "aihub/Qwen/Qwen3.6-27B"}
	got := mergeModelOverrides(environment, cli)
	want := map[string]string{"tester": "aihub/Qwen/Qwen3.6-27B", "oracle": "anthropic/claude-opus-4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overrides=%v want=%v", got, want)
	}
}
