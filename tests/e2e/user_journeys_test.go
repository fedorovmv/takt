package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newUserJourneyProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	initialized := resultObject(t, takt(t, nil, "init", "code", "--dir", project, "--json").RequireSuccess(t).JSON(t))
	if initialized["ok"] == false {
		t.Fatalf("init=%#v", initialized)
	}
	if _, err := os.Stat(filepath.Join(project, ".takt", "config.yaml")); err != nil {
		t.Fatalf("init did not create config: %v", err)
	}
	return project
}

func TestUserJourneyValidateRunInspectArtifacts(t *testing.T) {
	project := newUserJourneyProject(t)
	workflow := writeFile(t, project, "workflow.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: first-run
nodes:
  - id: produce
    bash: printf 'hello-user'
    output_type: result
    output_mime: text/plain
`)

	validated := resultObject(t, takt(t, nil, "validate", workflow, "--config", filepath.Join(project, ".takt", "config.yaml"), "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if validated["valid"] != true || validated["workflow"] != "first-run" {
		t.Fatalf("validation=%#v", validated)
	}

	started := resultObject(t, takt(t, nil, "run", workflow, "--config", filepath.Join(project, ".takt", "config.yaml"), "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	runID := stringField(t, started, "id")
	if started["status"] != "completed" || started["output"] != "hello-user" {
		t.Fatalf("run=%#v", started)
	}

	status := resultObject(t, takt(t, nil, "status", runID, "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if status["status"] != "completed" {
		t.Fatalf("status=%#v", status)
	}

	events := takt(t, nil, "events", runID, "--workspace", project, "--json").RequireSuccess(t).Stdout
	if !strings.Contains(events, `"type":"run.completed"`) || !strings.Contains(events, `"node_id":"produce"`) {
		t.Fatalf("events do not expose completed run/node lifecycle:\n%s", events)
	}

	artifacts := resultObject(t, takt(t, nil, "artifacts", runID, "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	items, ok := artifacts["artifacts"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("artifacts=%#v", artifacts)
	}
	artifact := items[0].(map[string]any)
	requireFileContains(t, stringField(t, artifact, "path"), "hello-user")
}

func TestUserJourneyApprovalAnswerContinue(t *testing.T) {
	project := newUserJourneyProject(t)
	workflow := writeFile(t, project, "approval.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: approval
nodes:
  - id: approve
    approval:
      message: Continue?
      capture_response: true
  - id: finish
    depends_on: [approve]
    bash: printf 'approved' > approved.txt
`)

	waiting := resultObject(t, takt(t, nil, "run", workflow, "--config", filepath.Join(project, ".takt", "config.yaml"), "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	runID := stringField(t, waiting, "id")
	if waiting["status"] != "waiting" {
		t.Fatalf("waiting run=%#v", waiting)
	}
	waitInfo := waiting["waiting"].(map[string]any)
	if waitInfo["node_id"] != "approve" || waitInfo["kind"] != "question" {
		t.Fatalf("waiting=%#v", waitInfo)
	}

	completed := resultObject(t, takt(t, nil, "answer", runID, "approve", "--workspace", project, "--value", "yes", "--json").RequireSuccess(t).JSON(t))
	if completed["status"] != "completed" {
		t.Fatalf("completed=%#v", completed)
	}
	requireFileContains(t, filepath.Join(project, "approved.txt"), "approved")
}

func TestUserJourneyFailureRetry(t *testing.T) {
	project := newUserJourneyProject(t)
	workflow := writeFile(t, project, "retry.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: retry
nodes:
  - id: flaky
    bash: |
      if [ ! -f retry.marker ]; then
        touch retry.marker
        exit 17
      fi
      printf retry-ok
`)

	failed := takt(t, nil, "run", workflow, "--config", filepath.Join(project, ".takt", "config.yaml"), "--workspace", project, "--json").RequireFailure(t)
	failed.Contains(t, `"code": "exit"`)

	listed := resultObject(t, takt(t, nil, "run", "list", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	runs := listed["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs=%#v", runs)
	}
	entry := runs[0].(map[string]any)
	runID := stringField(t, entry, "id")
	if entry["status"] != "failed" || entry["error_code"] != "exit" {
		t.Fatalf("failed run=%#v", entry)
	}

	retried := resultObject(t, takt(t, nil, "run", "retry", runID, "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if retried["status"] != "completed" || retried["output"] != "retry-ok" {
		t.Fatalf("retry=%#v", retried)
	}
}

func TestUserJourneyReusableSubworkflow(t *testing.T) {
	project := newUserJourneyProject(t)
	writeFile(t, project, "workflows/write.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: reusable-write
nodes:
  - id: write
    bash: printf '%s' '${inputs.name}' > reusable.txt
`)
	workflow := writeFile(t, project, "main.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: composition
nodes:
  - id: reusable
    subworkflow:
      path: workflows/write.yaml
      inputs:
        name: hello-subworkflow
`)

	completed := resultObject(t, takt(t, nil, "run", workflow, "--config", filepath.Join(project, ".takt", "config.yaml"), "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if completed["status"] != "completed" {
		t.Fatalf("subworkflow run=%#v", completed)
	}
	requireFileContains(t, filepath.Join(project, "reusable.txt"), "hello-subworkflow")
}
