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
		Current: &FlowProgressCurrent{CaseID: "implement-basic", Repeat: 1, Ordinal: 1, Phase: "workflow", PhaseStartedAt: now.Add(-4 * time.Second)},
		Runtime: FlowRuntimeProgress{RunID: "run-8d0636ca857eefbc068a767d", Status: "running", TotalNodes: 5, CompletedNodes: 0, RunningNodes: []string{"implement"}, NodeAttempts: 1, ProviderAttempts: 1, InputTokens: 1200, OutputTokens: 300, ContextTokens: 43439, ContextKnown: true, Timings: &FlowRuntimeTimings{
			Phases:    FlowPhaseTimings{PrepareMS: 1250, WorkflowMS: 4200},
			Assistant: FlowAssistantTimings{WaitMS: 8000, StreamMS: 3200, TotalMS: 11200, ToolMS: 900},
		}, AssistantActivity: []FlowAssistantProgress{{RunID: "run-8d0636ca857eefbc068a767d", NodeID: "implement", Attempt: 1, State: "awaiting_response", Since: now.Add(-2 * time.Second)}},
		},
	}
	text := progress.render(now)
	for _, want := range []string{"EVALUATION", "Status", "running", "Updated", "8s ago", "Elapsed", "10m0s", "Progress", "0 / 3 runs (0.0%)", "implement-basic#1", "workflow", "FLOW", "run-8d0636ca857eefbc068a767d", "implement", "0 / 5 completed (0.0%)", "Node attempts", "Provider attempts", "Context tokens", "43 439", "Tokens input", "1 200", "Tokens output", "300", "Tokens total", "1 500", "TIMINGS", "Prepare", "1.25s", "Workflow", "8.2s", "LLM wait", "10s", "LLM stream", "3.2s", "LLM total", "11.2s", "Assistant tools", "900ms", "RESULTS SO FAR", "Quality valid", "0 / 0 completed (n/a)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("progress text misses %q:\n%s", want, text)
		}
	}
}

func TestFlowProgressUsesFixedElapsedForFinishedEvaluation(t *testing.T) {
	started := time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)
	updated := started.Add(2*time.Minute + 3*time.Second)
	text := (FlowProgress{ReportVersion: FlowProgressVersion, Status: "completed", Suite: "suite.yaml", Workflow: "flow", OutputDir: "out", StartedAt: started, UpdatedAt: updated, TotalRuns: 2, CompletedRuns: 2, Runtime: FlowRuntimeProgress{TotalNodes: 4, CompletedNodes: 4, RunningNodes: []string{}}, Results: FlowProgressResults{Valid: 1, Invalid: 1}}).render(updated.Add(10 * time.Minute))
	for _, want := range []string{"Elapsed", "2m3s", "Progress", "2 / 2 runs (100.0%)", "Nodes", "4 / 4 completed (100.0%)", "TIMINGS", "Measured", "unavailable", "Quality valid", "1 / 2 completed (50.0%)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("finished progress text misses %q:\n%s", want, text)
		}
	}
}

func TestFlowProgressIsPublishedAtomicallyAndReloaded(t *testing.T) {
	dir := t.TempDir()
	started := time.Now().UTC()
	want := &FlowProgress{ReportVersion: FlowProgressVersion, Status: "running", Suite: "suite.yaml", Workflow: "flow", OutputDir: dir, StartedAt: started, UpdatedAt: started, TotalRuns: 1, Current: &FlowProgressCurrent{CaseID: "case", Repeat: 1, Ordinal: 1, Phase: "workflow", PhaseStartedAt: started}, Runtime: FlowRuntimeProgress{RunningNodes: []string{}, Timings: &FlowRuntimeTimings{Phases: FlowPhaseTimings{WorkflowMS: 7}, Assistant: FlowAssistantTimings{WaitMS: 11}}}, Results: FlowProgressResults{}}
	if err := WriteFlowProgress(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFlowProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReportVersion != FlowProgressVersion || got.Status != "running" || got.TotalRuns != 1 || got.Current == nil || got.Current.PhaseStartedAt.IsZero() || got.Runtime.Timings == nil || got.Runtime.Timings.Phases.WorkflowMS != 7 || got.Runtime.Timings.Assistant.WaitMS != 11 {
		t.Fatalf("progress=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "progress.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary progress file remains: %v", err)
	}
}

func TestFlowProgressAccumulatesPhaseTimings(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	tracker, err := newFlowProgressTracker(dir, FlowProgress{ReportVersion: FlowProgressVersion, Status: "running", Suite: "suite.yaml", Workflow: "flow", OutputDir: dir, StartedAt: now, TotalRuns: 1, Runtime: FlowRuntimeProgress{RunningNodes: []string{}}, Results: FlowProgressResults{}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.begin("case", 1, 1); err != nil {
		t.Fatal(err)
	}
	now = now.Add(1500 * time.Millisecond)
	if err := tracker.phase("validator_preflight"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2500 * time.Millisecond)
	if _, err := tracker.runtime(FlowRuntimeProgress{RunID: "run-1", Status: "running", RunningNodes: []string{}}); err != nil {
		t.Fatal(err)
	}
	progress, err := LoadFlowProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Runtime.Timings == nil || progress.Runtime.Timings.Phases.PrepareMS != 1500 || progress.Runtime.Timings.Phases.ValidatorPreflightMS != 2500 {
		t.Fatalf("timings=%+v", progress.Runtime.Timings)
	}
	if progress.Current == nil || !progress.Current.PhaseStartedAt.Equal(now) {
		t.Fatalf("current=%+v", progress.Current)
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

func TestFlowProgressRendersProviderActivity(t *testing.T) {
	dir := t.TempDir()
	data := `{"report_version":"takt-flow-evaluation-progress/v1alpha1","status":"running","suite":"suite.yaml","workflow":"flow","output_dir":"out","started_at":"2026-08-17T14:00:00Z","updated_at":"2026-08-17T14:03:11Z","total_runs":1,"completed_runs":0,"runtime":{"total_nodes":1,"completed_nodes":0,"running_nodes":["implement"],"node_attempts":1,"provider_attempts":0,"input_tokens":0,"output_tokens":0,"cost":0,"assistant_activity":[{"run_id":"run-1","node_id":"implement","attempt":1,"state":"retry_backoff","since":"2026-08-17T14:03:10Z","call":2,"retry":1,"max_retries":3,"delay_ms":2000,"last_error":"HTTP 500 unavailable"}]},"results":{"valid":0,"invalid":0,"infrastructure_errors":0,"validation_errors":0}}`
	if err := os.WriteFile(filepath.Join(dir, FlowProgressFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	progress, err := LoadFlowProgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	text := progress.render(time.Date(2026, 8, 17, 14, 3, 12, 0, time.UTC))
	for _, want := range []string{"PROVIDER ACTIVITY", "implement#1", "retry_backoff", "for 2s", "call 2", "retry 1/3", "delay 2s", "HTTP 500 unavailable"} {
		if !strings.Contains(text, want) {
			t.Fatalf("provider activity misses %q:\n%s", want, text)
		}
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
		{name: "phase timing", mutate: func(progress *FlowProgress) {
			progress.Runtime.Timings = &FlowRuntimeTimings{Phases: FlowPhaseTimings{WorkflowMS: -1}}
		}},
		{name: "assistant timing", mutate: func(progress *FlowProgress) {
			progress.Runtime.Timings = &FlowRuntimeTimings{Assistant: FlowAssistantTimings{WaitMS: -1}}
		}},
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
