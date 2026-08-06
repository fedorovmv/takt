package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"takt/internal/spec"
	"takt/internal/store"
)

func TestValidationCommandsExecuteInOrderAndReturnStructuredReport(t *testing.T) {
	dir := t.TempDir()
	runner := &Runner{Workspace: dir, Store: store.FS{Workspace: dir}}
	state := &store.RunState{ID: "run-test", Input: `{"base_branch":"main","validation_commands":["printf first","printf second"]}`, Nodes: map[string]*store.NodeState{"validate": {Attempts: 1}}}
	node := spec.Node{ID: "validate", Script: &spec.ScriptSpec{Runtime: "validation"}}
	result, err := runner.runValidationCommands(context.Background(), state, node, nil, "", runner.Store.ArtifactsDir(state.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Status  string `json:"status"`
		Results []struct {
			Command  string `json:"command"`
			ExitCode int    `json:"exit_code"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.Output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "ready" || len(report.Results) != 2 || report.Results[0].Command != "printf first" || report.Results[1].Command != "printf second" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if result.Stdout != "firstsecond" {
		t.Fatalf("stdout=%q", result.Stdout)
	}
}

func TestValidationCommandsPreserveFailureReport(t *testing.T) {
	dir := t.TempDir()
	runner := &Runner{Workspace: dir, Store: store.FS{Workspace: dir}}
	state := &store.RunState{ID: "run-test", Input: `{"validation_commands":["printf ok","printf bad >&2; exit 7"]}`, Nodes: map[string]*store.NodeState{"validate": {Attempts: 1}}}
	node := spec.Node{ID: "validate", Script: &spec.ScriptSpec{Runtime: "validation"}}
	result, err := runner.runValidationCommands(context.Background(), state, node, nil, "", runner.Store.ArtifactsDir(state.ID), dir)
	if err == nil {
		t.Fatal("expected command failure")
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit_code=%d", result.ExitCode)
	}
	var report struct {
		Status string `json:"status"`
	}
	if json.Unmarshal([]byte(result.Output), &report) != nil || report.Status != "failed" {
		t.Fatalf("unexpected output %q", result.Output)
	}
}
