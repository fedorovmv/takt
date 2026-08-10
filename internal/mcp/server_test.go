package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"takt/internal/appapi"
	"takt/internal/application"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/extensions/notification"
	"takt/internal/externalworker"
	"takt/internal/profile"
	"takt/internal/store"
	"takt/internal/testsupport/appfixture"
)

func testDependencies(f *appfixture.Fixture) Dependencies {
	return Dependencies{
		API:   appapi.New(appapi.Dependencies{Core: f.Core, Dynamic: f.Dynamic, Blocks: f.Extensions.Blocks, Notifications: f.Extensions.Notifications}),
		Plans: f.Dynamic.PlanService, External: f.External, Maintenance: f.Maintenance,
	}
}

func TestServeStdioSupportsLegacyInitializeAndModernDiscover(t *testing.T) {
	service, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"},"_meta":{"trace":"test"}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}` + "\n",
	)
	var output bytes.Buffer
	if err := New(testDependencies(service), input, &output, &bytes.Buffer{}).ServeStdio(context.Background()); err != nil {
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
	if len(listed) != 54 {
		t.Fatalf("tools count = %d", len(listed))
	}
	first := listed[0].(map[string]any)["name"]
	last := listed[len(listed)-1].(map[string]any)["name"]
	if first != "takt.task.start" || last != "takt.node.fail" {
		t.Fatalf("unexpected deterministic tool order: %v ... %v", first, last)
	}
}

func TestAgentSurfaceExposesOnlyCompactTaskAPI(t *testing.T) {
	service, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithSurface(testDependencies(service), nil, nil, nil, SurfaceAgent)
	tools := tools(SurfaceAgent)
	want := []string{
		"takt.task.start",
		"takt.task.status",
		"takt.task.respond",
		"takt.task.stop",
		"takt.task.explain",
	}
	if len(tools) != len(want) {
		t.Fatalf("agent tools = %d, want %d", len(tools), len(want))
	}
	for i, tool := range tools {
		if tool.Name != want[i] {
			t.Fatalf("agent tool[%d] = %s, want %s", i, tool.Name, want[i])
		}
		if !strings.HasPrefix(tool.Description, "Experimental: ") {
			t.Fatalf("agent Dynamic Flow tool must disclose experimental stability: %#v", tool)
		}
	}
	denied := server.callTool(context.Background(), callParams{Name: "takt.run.list", Arguments: map[string]any{}})
	if denied["isError"] != true {
		t.Fatalf("operator tool was not denied on agent surface: %#v", denied)
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
	service, err := appfixture.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)

	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{
		"selector": workflowPath, "config_path": configPath, "detached": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := startedValue.(*application.StartResult)
	if started.State == nil || started.State.Status != store.RunWaiting {
		t.Fatalf("started state = %#v", started.State)
	}
	runID := started.RunID

	eventsValue, err := server.executeTool(context.Background(), "takt.run.events", map[string]any{"run_id": runID, "after_revision": 0})
	if err != nil {
		t.Fatal(err)
	}
	events := eventsValue.(*application.EventsResult)
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
	service, err := appfixture.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)

	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{
		"selector": workflowPath, "config_path": configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := startedValue.(*application.StartResult)
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
	service, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)
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

func TestRequestIDKeyPreservesLargeNumericIDs(t *testing.T) {
	first := requestIDKey(json.RawMessage("9007199254740992"))
	second := requestIDKey(json.RawMessage("9007199254740993"))
	if first == second {
		t.Fatalf("large request ids collided: %q", first)
	}
	if first != "j:9007199254740992" || second != "j:9007199254740993" {
		t.Fatalf("unexpected keys: %q %q", first, second)
	}
}

func TestServeStdioAcceptsEnvelopeExtensionsAndRejectsInvalidEnvelope(t *testing.T) {
	service, err := appfixture.New(t.TempDir(), ".takt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{},"traceparent":"00-test"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"params":{}}` + "\n",
	)
	var output bytes.Buffer
	if err := New(testDependencies(service), input, &output, &bytes.Buffer{}).ServeStdio(context.Background()); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	if responses["1"]["error"] != nil {
		t.Fatalf("extension field was rejected: %#v", responses["1"])
	}
	rpcErr := responses["2"]["error"].(map[string]any)
	if int(rpcErr["code"].(float64)) != -32600 {
		t.Fatalf("invalid envelope code = %#v", rpcErr)
	}
}

func TestExternalNodeMCPClaimEventsAndCompletion(t *testing.T) {
	workspace := t.TempDir()
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	configPath := filepath.Join(workspace, "config.yaml")
	writeFile(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	writeFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: external-mcp-contract
defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: Produce a structured result.
    executor: external
    allowed_tools: [read]
    output_format:
      type: object
      properties:
        ok:
          type: boolean
      required: [ok]
    output_type: result
    output_mime: application/json
  - id: verify
    depends_on: [delegated]
    bash: test '${nodes.delegated.output.ok}' = 'true'
`)
	service, err := appfixture.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)
	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{"selector": workflowPath, "config_path": configPath, "detached": false})
	if err != nil {
		t.Fatal(err)
	}
	started := startedValue.(*application.StartResult)
	if started.State == nil || started.State.Status != store.RunWaiting || started.State.Waiting == nil || started.State.Waiting.Kind != "external_node" {
		t.Fatalf("start state = %#v", started.State)
	}
	pendingValue, err := server.executeTool(context.Background(), "takt.node.pending", map[string]any{"run_id": started.RunID})
	if err != nil {
		t.Fatal(err)
	}
	tasks := pendingValue.(map[string]any)["tasks"].([]externalworker.Task)
	if len(tasks) != 1 || tasks[0].NodeID != "delegated" {
		t.Fatalf("tasks = %#v", tasks)
	}
	if _, err := server.executeTool(context.Background(), "takt.node.claim", map[string]any{"run_id": started.RunID, "node_id": "delegated", "worker_id": "worker-1"}); err == nil {
		t.Fatal("expected missing tool_policy capability")
	}
	claimedValue, err := server.executeTool(context.Background(), "takt.node.claim", map[string]any{
		"run_id": started.RunID, "node_id": "delegated", "worker_id": "worker-1", "capabilities": []string{"tool_policy"}, "lease_ms": 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed := claimedValue.(*externalworker.Task)
	if claimed.ClaimToken == "" {
		t.Fatal("claim token is empty")
	}
	publicValue, err := server.executeTool(context.Background(), "takt.run.get", map[string]any{"run_id": started.RunID})
	if err != nil {
		t.Fatal(err)
	}
	public := publicValue.(*store.RunState)
	if got := public.Nodes["delegated"].External.ClaimToken; got != "" {
		t.Fatalf("public claim token leaked: %q", got)
	}
	requestedValue, err := server.executeTool(context.Background(), "takt.node.tool.request", map[string]any{
		"run_id": started.RunID, "node_id": "delegated", "claim_token": claimed.ClaimToken,
		"call_id": "call-1", "tool": "read", "input": map[string]any{"path": "README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	requested := requestedValue.(*store.ToolCallState)
	if requested.Status != "allowed" {
		t.Fatalf("requested tool = %#v", requested)
	}
	if _, err := server.executeTool(context.Background(), "takt.node.tool.start", map[string]any{
		"run_id": started.RunID, "node_id": "delegated", "claim_token": claimed.ClaimToken, "call_id": "call-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.executeTool(context.Background(), "takt.node.tool.complete", map[string]any{
		"run_id": started.RunID, "node_id": "delegated", "claim_token": claimed.ClaimToken, "call_id": "call-1",
		"output": map[string]any{"bytes": 12},
	}); err != nil {
		t.Fatal(err)
	}
	completedValue, err := server.executeTool(context.Background(), "takt.node.complete", map[string]any{
		"run_id": started.RunID, "node_id": "delegated", "claim_token": claimed.ClaimToken,
		"structured": map[string]any{"ok": true}, "stdout": "raw-provider-stream", "exit_code": 0,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 3, "cost": 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	completed := completedValue.(*store.RunState)
	if completed.Status != store.RunCompleted {
		t.Fatalf("completed state = %#v", completed)
	}
	if completed.Nodes["delegated"].Stdout != "raw-provider-stream" {
		t.Fatalf("stdout = %q", completed.Nodes["delegated"].Stdout)
	}
	if len(completed.Nodes["delegated"].Artifacts) != 1 || completed.Nodes["delegated"].Artifacts[0].Type != "result" {
		t.Fatalf("artifacts = %#v", completed.Nodes["delegated"].Artifacts)
	}
	eventsValue, err := server.executeTool(context.Background(), "takt.run.events", map[string]any{"run_id": started.RunID, "after_revision": 0})
	if err != nil {
		t.Fatal(err)
	}
	events := eventsValue.(*application.EventsResult).Events
	foundTool := false
	for _, event := range events {
		if event.Type == "assistant.tool.started" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("normalized tool event missing: %#v", events)
	}
}

func TestExternalNodeFailureRetriesWithFreshClaim(t *testing.T) {
	workspace := t.TempDir()
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	configPath := filepath.Join(workspace, "config.yaml")
	writeFile(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	writeFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: external-retry
defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: retry me
    executor: external
    attempts:
      max: 2
      retry_on: [exit]
`)
	service, err := appfixture.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)
	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{"selector": workflowPath, "config_path": configPath, "detached": false})
	if err != nil {
		t.Fatal(err)
	}
	runID := startedValue.(*application.StartResult).RunID
	firstValue, err := server.executeTool(context.Background(), "takt.node.claim", map[string]any{"run_id": runID, "node_id": "delegated", "worker_id": "worker-1", "lease_ms": 60000})
	if err != nil {
		t.Fatal(err)
	}
	first := firstValue.(*externalworker.Task)
	failedValue, err := server.executeTool(context.Background(), "takt.node.fail", map[string]any{"run_id": runID, "node_id": "delegated", "claim_token": first.ClaimToken, "exit_code": 7, "error_code": "exit", "error": "temporary provider failure"})
	if err != nil {
		t.Fatal(err)
	}
	failed := failedValue.(*store.RunState)
	if failed.Status != store.RunWaiting || failed.Nodes["delegated"].External.Attempt != 2 {
		t.Fatalf("retry state = %#v", failed)
	}
	secondValue, err := server.executeTool(context.Background(), "takt.node.claim", map[string]any{"run_id": runID, "node_id": "delegated", "worker_id": "worker-2", "lease_ms": 60000})
	if err != nil {
		t.Fatal(err)
	}
	second := secondValue.(*externalworker.Task)
	if second.ClaimToken == first.ClaimToken || second.Attempt != 2 {
		t.Fatalf("claim was not renewed: first=%#v second=%#v", first, second)
	}
	completedValue, err := server.executeTool(context.Background(), "takt.node.complete", map[string]any{"run_id": runID, "node_id": "delegated", "claim_token": second.ClaimToken, "output": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	completed := completedValue.(*store.RunState)
	if completed.Status != store.RunCompleted || completed.Nodes["delegated"].Attempts != 2 || len(completed.Nodes["delegated"].Executions) != 2 {
		t.Fatalf("completed retry state = %#v", completed)
	}
}

func TestExternalNodeExpiredLeaseCanBeReclaimed(t *testing.T) {
	workspace := t.TempDir()
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	configPath := filepath.Join(workspace, "config.yaml")
	writeFile(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	writeFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: external-lease
defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: lease
    executor: external
`)
	service, err := appfixture.New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)
	startedValue, err := server.executeTool(context.Background(), "takt.run.start", map[string]any{"selector": workflowPath, "config_path": configPath, "detached": false})
	if err != nil {
		t.Fatal(err)
	}
	runID := startedValue.(*application.StartResult).RunID
	firstValue, err := server.executeTool(context.Background(), "takt.node.claim", map[string]any{"run_id": runID, "node_id": "delegated", "worker_id": "worker-1", "lease_ms": 1})
	if err != nil {
		t.Fatal(err)
	}
	first := firstValue.(*externalworker.Task)
	time.Sleep(10 * time.Millisecond)
	secondValue, err := server.executeTool(context.Background(), "takt.node.claim", map[string]any{"run_id": runID, "node_id": "delegated", "worker_id": "worker-2", "lease_ms": 60000})
	if err != nil {
		t.Fatal(err)
	}
	second := secondValue.(*externalworker.Task)
	if second.ClaimToken == first.ClaimToken || second.ClaimedBy != "worker-2" {
		t.Fatalf("lease was not reclaimed: first=%#v second=%#v", first, second)
	}
}

func TestHostRunAndNotificationToolsThroughMCP(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := appfixture.New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(testDependencies(service), nil, nil, nil)
	candidate := dynamicplan.Plan{
		APIVersion: "takt/v1alpha1", Kind: "WorkflowPlan", Decision: "planned",
		Goal: "summarize repository", Reason: "single trusted phase",
		Budget: dynamicplan.Budget{MaxChildRuns: 4, MaxParallel: 1, MaxIterations: 2, MaxTokens: 10000},
		Phases: []dynamicplan.Phase{{ID: "summary", Uses: "synthesize", Objective: "Summarize the repository", Strategy: "task"}},
	}
	beginValue, err := server.executeTool(context.Background(), "takt.host.begin", map[string]any{
		"host": "pi", "host_session_id": "mcp-host-session", "goal": candidate.Goal,
		"profile": "code", "enforcement": "guarded",
		"capabilities": map[string]any{"command_interception": true, "input_interception": true, "tool_call_blocking": true, "session_recovery": true},
		"candidate":    candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	begun := beginValue.(*dynamicflow.HostBeginResult)
	if begun.Session.ID == "" || begun.Session.Status != "preview" {
		t.Fatalf("host begin = %#v", begun)
	}
	getValue, err := server.executeTool(context.Background(), "takt.host.get", map[string]any{"session_id": begun.Session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if getValue.(*dynamicflow.HostSessionView).Session.ID != begun.Session.ID {
		t.Fatalf("host get = %#v", getValue)
	}
	guardValue, err := server.executeTool(context.Background(), "takt.host.guard_tool", map[string]any{"session_id": begun.Session.ID, "tool": "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if guardValue.(*dynamicflow.HostGuardDecision).Allowed {
		t.Fatalf("edit tool escaped MCP guard: %#v", guardValue)
	}
	startedAt := time.Now()
	if _, err := server.executeTool(context.Background(), "takt.host.confirm", map[string]any{"session_id": begun.Session.ID, "confirm": true}); err != nil {
		t.Fatal(err)
	}
	if time.Since(startedAt) > 2*time.Second {
		t.Fatalf("host confirm blocked instead of starting detached")
	}
	if _, err := server.executeTool(context.Background(), "takt.run.list", map[string]any{"active_only": true}); err != nil {
		t.Fatal(err)
	}
	noticeValue, err := server.executeTool(context.Background(), "takt.notify.test", map[string]any{"message": "mcp notice"})
	if err != nil {
		t.Fatal(err)
	}
	notice := noticeValue.(*notification.Item)
	listedValue, err := server.executeTool(context.Background(), "takt.notify.list", map[string]any{"unread_only": true})
	if err != nil {
		t.Fatal(err)
	}
	listed := listedValue.([]notification.Item)
	if len(listed) == 0 || listed[0].ID != notice.ID {
		t.Fatalf("notification list = %#v", listed)
	}
	if _, err := server.executeTool(context.Background(), "takt.notify.ack", map[string]any{"id": notice.ID}); err != nil {
		t.Fatal(err)
	}
	// host.confirm is intentionally detached. This unit test has no daemon
	// monitor, so drive the same durable plan reconciler explicitly until the
	// detached Run reaches a stable plan boundary before TempDir cleanup.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := service.Dynamic.PlanService.AdvanceDynamicPlans(context.Background()); err != nil {
			t.Fatal(err)
		}
		planView, err := service.Dynamic.PlanService.GetPlan(begun.Session.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		if planView.Record.Status != "running" && planView.Record.Status != "pausing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("detached plan did not settle before cleanup: %#v", planView.Record)
		}
		time.Sleep(20 * time.Millisecond)
	}

}
