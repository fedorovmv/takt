package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFlowProgressRendersExternalLiveStatus(t *testing.T) {
	now := time.Date(2026, 8, 14, 15, 0, 8, 0, time.UTC)
	progress := FlowProgress{
		ReportVersion: FlowProgressVersion, Status: "running", Workflow: "code:feature-development", OutputDir: ".takt/evals/run-a",
		StartedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-8 * time.Second), TotalRuns: 3, CompletedRuns: 0,
		Current: &FlowProgressCurrent{CaseID: "implement-basic", Repeat: 1, Ordinal: 1, Phase: "workflow"},
		Runtime: FlowRuntimeProgress{RunID: "run-8d0636ca857eefbc068a767d", Status: "running", TotalNodes: 5, CompletedNodes: 0, RunningNodes: []string{"implement"}, NodeAttempts: 1, ProviderAttempts: 1},
	}
	text := progress.render(now)
	for _, want := range []string{"EVALUATION", "Status", "running", "Updated", "8s ago", "Progress", "0 / 3 runs", "implement-basic#1", "workflow", "FLOW", "run-8d0636ca857eefbc068a767d", "implement", "0 / 5 completed", "Node attempts", "Provider attempts", "RESULTS SO FAR"} {
		if !strings.Contains(text, want) {
			t.Fatalf("progress text misses %q:\n%s", want, text)
		}
	}
}

func TestFlowProgressIsPublishedAtomicallyAndReloaded(t *testing.T) {
	dir := t.TempDir()
	want := &FlowProgress{ReportVersion: FlowProgressVersion, Status: "running", Suite: "suite.yaml", Workflow: "flow", OutputDir: dir, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), TotalRuns: 1, Runtime: FlowRuntimeProgress{RunningNodes: []string{}}, Results: FlowProgressResults{}}
	if err := WriteFlowProgress(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFlowProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportVersion != FlowProgressVersion || got.Status != "running" || got.TotalRuns != 1 {
		t.Fatalf("progress=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "progress.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary progress file remains: %v", err)
	}
}

func TestLoadFlowProgressRejectsTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	data := `{"report_version":"takt-flow-evaluation-progress/v1alpha1","status":"running","suite":"suite.yaml","workflow":"flow","output_dir":"out","started_at":"2026-08-14T12:00:00Z","updated_at":"2026-08-14T12:00:01Z","total_runs":1,"completed_runs":0,"runtime":{"total_nodes":0,"completed_nodes":0,"running_nodes":[],"node_attempts":0,"provider_attempts":0,"input_tokens":0,"output_tokens":0,"cost":0},"results":{"valid":0,"invalid":0,"infrastructure_errors":0,"validation_errors":0}} {}`
	if err := os.WriteFile(filepath.Join(dir, FlowProgressFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFlowProgress(dir); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestFlowProgressRejectsMissingRequiredFieldsAndNegativeResults(t *testing.T) {
	now := time.Now().UTC()
	valid := FlowProgress{ReportVersion: FlowProgressVersion, Status: "running", Suite: "suite.yaml", Workflow: "flow", OutputDir: "out", StartedAt: now, UpdatedAt: now, TotalRuns: 1, Runtime: FlowRuntimeProgress{RunningNodes: []string{}}}
	for _, test := range []struct {
		name   string
		mutate func(*FlowProgress)
	}{
		{name: "suite", mutate: func(progress *FlowProgress) { progress.Suite = "" }},
		{name: "workflow", mutate: func(progress *FlowProgress) { progress.Workflow = "" }},
		{name: "output", mutate: func(progress *FlowProgress) { progress.OutputDir = "" }},
		{name: "started", mutate: func(progress *FlowProgress) { progress.StartedAt = time.Time{} }},
		{name: "updated", mutate: func(progress *FlowProgress) { progress.UpdatedAt = time.Time{} }},
		{name: "running nodes", mutate: func(progress *FlowProgress) { progress.Runtime.RunningNodes = nil }},
		{name: "result", mutate: func(progress *FlowProgress) { progress.Results.Valid = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			progress := valid
			test.mutate(&progress)
			if err := validateFlowProgress(&progress); err == nil {
				t.Fatalf("invalid progress accepted: %+v", progress)
			}
		})
	}
}
