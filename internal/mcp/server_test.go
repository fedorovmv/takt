package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"takt/internal/control"
	"takt/internal/store"
)

func TestServeStdioSupportsLegacyInitializeAndModernDiscover(t *testing.T) {
	service, err := control.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"},"_meta":{"trace":"test"}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}` + "\n",
	)
	var output bytes.Buffer
	if err := New(service, input, &output, &bytes.Buffer{}).ServeStdio(context.Background()); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	legacy := responses["1"]
	legacyResult := legacy["result"].(map[string]any)
	if got := legacyResult["protocolVersion"]; got != Protocol2025 {
		t.Fatalf("legacy protocol = %v", got)
	}
	modern := responses["2"]["result"].(map[string]any)
	if got := modern["protocolVersion"]; got != Protocol2026 {
		t.Fatalf("modern protocol = %v", got)
	}
	listed := responses["3"]["result"].(map[string]any)["tools"].([]any)
	if len(listed) != 10 {
		t.Fatalf("tools count = %d", len(listed))
	}
	first := listed[0].(map[string]any)["name"]
	last := listed[len(listed)-1].(map[string]any)["name"]
	if first != "takt.workflow.list" || last != "takt.run.events" {
		t.Fatalf("unexpected deterministic tool order: %v ... %v", first, last)
	}
}

func TestToolLifecycleStartEventsArtifactsAndAnswer(t *testing.T) {
	workspace := t.TempDir()
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	configPath := filepath.Join(workspace, "config.yaml")
	writeFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: mcp-contract
nodes:
  - id: produce
    bash: |
      printf 'hello from artifact' > result.txt
    output_type: report
    output_mime: text/plain
    output_path: result.txt
  - id: approve
    depends_on: [produce]
    approval:
      message: Continue?
      capture_response: true
`)
	service, err := control.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(service, nil, nil, nil)

	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{
		"selector": workflowPath, "config_path": configPath, "detached": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := startedValue.(*control.StartResult)
	if started.State == nil || started.State.Status != store.RunWaiting {
		t.Fatalf("started state = %#v", started.State)
	}
	runID := started.RunID

	eventsValue, err := server.executeTool(context.Background(), "takt.run.events", map[string]any{"run_id": runID, "after_revision": 0})
	if err != nil {
		t.Fatal(err)
	}
	events := eventsValue.(*control.EventsResult)
	if len(events.Events) < 3 || events.NextRevision == 0 {
		t.Fatalf("events = %#v", events)
	}

	artifactsValue, err := server.executeTool(context.Background(), "takt.run.artifacts", map[string]any{
		"run_id": runID, "include_content": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifactsValue.(map[string]any)["artifacts"].([]map[string]any)
	if len(artifacts) != 1 || artifacts[0]["content"] != "hello from artifact" {
		t.Fatalf("artifacts = %#v", artifacts)
	}

	answeredValue, err := server.executeTool(context.Background(), "takt.run.answer", map[string]any{
		"run_id": runID, "node_id": "approve", "value": "approved",
	})
	if err != nil {
		t.Fatal(err)
	}
	answered := answeredValue.(*store.RunState)
	if answered.Status != store.RunCompleted {
		t.Fatalf("answered status = %s error=%s", answered.Status, answered.Error)
	}
	if answered.Approvals["approve"] != "approved" {
		t.Fatalf("approval = %#v", answered.Approvals)
	}
}

func TestRunStartDefaultsToDetachedAndCanBeCancelled(t *testing.T) {
	workspace := t.TempDir()
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	configPath := filepath.Join(workspace, "config.yaml")
	writeFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: detached-cancel
nodes:
  - id: wait
    bash: sleep 30
`)
	service, err := control.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(service, nil, nil, nil)

	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{
		"selector": workflowPath, "config_path": configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := startedValue.(*control.StartResult)
	if !started.Accepted || started.RunID == "" {
		t.Fatalf("start result = %#v", started)
	}

	if _, err := server.executeTool(context.Background(), "takt.run.cancel", map[string]any{
		"run_id": started.RunID, "reason": "test cancellation",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		stateValue, getErr := server.executeTool(context.Background(), "takt.run.get", map[string]any{"run_id": started.RunID})
		if getErr == nil {
			state := stateValue.(*store.RunState)
			if state.Status == store.RunCancelled {
				if state.ErrorCode != "cancelled" {
					t.Fatalf("cancelled state = %#v", state)
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not become cancelled: %v", started.RunID, getErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestToolArgumentsRejectUnknownFields(t *testing.T) {
	service, err := control.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	server := New(service, nil, nil, nil)
	_, err = server.executeTool(context.Background(), "takt.run.get", map[string]any{"run_id": "run-1", "typo": true})
	if err == nil {
		t.Fatal("expected strict argument error")
	}
}

func decodeResponses(t *testing.T, raw []byte) map[string]map[string]any {
	t.Helper()
	result := map[string]map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var value map[string]any
		if err := decoder.Decode(&value); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		id := jsonNumberString(value["id"])
		result[id] = value
	}
	return result
}

func jsonNumberString(value any) string {
	switch typed := value.(type) {
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
