package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/assistant"
	"takt/internal/store"
)

func TestExternalToolApprovalBlocksAndLinksArtifact(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	mustWriteControlTest(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	mustWriteControlTest(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: tool-approval
defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: execute safely
    executor: external
    allowed_tools: [read]
    tool_approval:
      mode: required
      tools: [read]
      message: Allow ${tool}?
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath})
	if err != nil {
		t.Fatal(err)
	}
	declaration := assistant.CapabilityDeclaration{
		Protocol:     assistant.EventProtocolV2,
		Capabilities: []string{"tool_policy", assistant.CapabilityToolControl, assistant.CapabilityAgentEventsV2, assistant.CapabilityToolEvents},
		EventTypes:   assistant.EventTypes(), SessionEvents: true, ToolEvents: true, ToolControl: true, ArtifactEvents: true, UsageEvents: true,
	}
	claim, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "delegated", WorkerID: "worker", Declaration: declaration})
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan *store.ToolCallState, 1)
	errCh := make(chan error, 1)
	go func() {
		call, callErr := service.RequestExternalTool(context.Background(), ExternalToolRequest{
			RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken,
			CallID: "call-1", Tool: "read", Input: json.RawMessage(`{"path":"evidence.txt"}`), Wait: 3 * time.Second,
		})
		result <- call
		errCh <- callErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		call, getErr := service.GetExternalTool(started.RunID, "delegated", "call-1")
		if getErr == nil && call.Status == "waiting_approval" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tool did not reach waiting_approval: %v", getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := service.DecideExternalTool(ExternalToolDecisionRequest{RunID: started.RunID, NodeID: "delegated", CallID: "call-1", Decision: "allow", Reason: "reviewed"}); err != nil {
		t.Fatal(err)
	}
	call := <-result
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if call.Status != "allowed" {
		t.Fatalf("tool decision = %#v", call)
	}
	if _, err := service.StartExternalTool(ExternalToolUpdate{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "call-1"}); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(claim.Workspace, "evidence.txt")
	if err := os.WriteFile(artifactPath, []byte("evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := service.DeclareExternalArtifact(ExternalArtifactRequest{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "call-1", Type: "evidence", MIME: "text/plain", Path: artifactPath})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.CallID != "call-1" {
		t.Fatalf("artifact call_id = %q", artifact.CallID)
	}
	if _, err := service.CompleteExternalTool(ExternalToolUpdate{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "call-1", Output: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestExternalToolPolicyDeniesBeforeStartAndRunningCallCanBeCancelled(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	mustWriteControlTest(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	mustWriteControlTest(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: tool-policy
defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: execute safely
    executor: external
    allowed_tools: [read]
`)
	service, _ := New(workspace, configPath)
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath})
	if err != nil {
		t.Fatal(err)
	}
	declaration := assistant.CapabilityDeclaration{Protocol: assistant.EventProtocolV2, Capabilities: []string{"tool_policy"}, ToolEvents: true, ToolControl: true}
	claim, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "delegated", WorkerID: "worker", Declaration: declaration})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := service.RequestExternalTool(context.Background(), ExternalToolRequest{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "write-1", Tool: "write"})
	if err != nil {
		t.Fatal(err)
	}
	if denied.Status != "denied" {
		t.Fatalf("denied = %#v", denied)
	}
	if _, err := service.StartExternalTool(ExternalToolUpdate{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "write-1"}); err == nil {
		t.Fatal("denied tool started")
	}
	allowed, err := service.RequestExternalTool(context.Background(), ExternalToolRequest{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "read-1", Tool: "read"})
	if err != nil || allowed.Status != "allowed" {
		t.Fatalf("allowed=%#v err=%v", allowed, err)
	}
	if _, err := service.StartExternalTool(ExternalToolUpdate{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "read-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteExternal(context.Background(), ExternalSubmission{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, Output: "premature"}); err == nil {
		t.Fatal("external node completed with a running tool call")
	}
	cancelled, err := service.CancelExternalTool(started.RunID, "delegated", "read-1", "unsafe result")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancel_requested" || !cancelled.CancelRequested {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	if _, err := service.CompleteExternal(context.Background(), ExternalSubmission{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, Output: "premature"}); err == nil {
		t.Fatal("external node completed with a cancellation still pending")
	}
	finished, err := service.CompleteExternalTool(ExternalToolUpdate{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, CallID: "read-1"})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "cancelled" {
		t.Fatalf("finished = %#v", finished)
	}
	state, err := service.CompleteExternal(context.Background(), ExternalSubmission{RunID: started.RunID, NodeID: "delegated", ClaimToken: claim.ClaimToken, Output: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted {
		t.Fatalf("run status = %s", state.Status)
	}
}

func mustWriteControlTest(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExternalIdleTimeoutFailsClaimedNode(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	mustWriteControlTest(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	mustWriteControlTest(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: external-idle

defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: wait forever
    executor: external
    idle_timeout: 40ms
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "delegated", WorkerID: "worker", Declaration: assistant.CapabilityDeclaration{Protocol: assistant.EventProtocolV2}})
	if err != nil {
		t.Fatal(err)
	}
	if claim.ClaimToken == "" {
		t.Fatal("claim token is empty")
	}
	expired, err := service.ExpireIdleExternal(context.Background(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 {
		t.Fatalf("expired = %#v", expired)
	}
	state, err := service.GetRun(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunFailed || state.ErrorCode != "timed_out" {
		t.Fatalf("state = %#v", state)
	}
}

func TestExternalSideEffectRequiresReconciliationBeforeExpiredClaimReplay(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	mustWriteControlTest(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	mustWriteControlTest(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: reconcile-side-effect
defaults:
  assistant: worker
  model: demo
nodes:
  - id: publish
    prompt: publish externally
    executor: external
    side_effect:
      mode: reconcile
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "publish", WorkerID: "worker", Declaration: assistant.CapabilityDeclaration{Protocol: assistant.EventProtocolV2}, Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if claim.SideEffectMode != "reconcile" || claim.IdempotencyKey == "" {
		t.Fatalf("claim=%#v", claim)
	}
	fs := store.FS{Workspace: workspace}
	state, err := fs.Load(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state.Nodes["publish"].External.LeaseExpiresAt = time.Now().Add(-time.Minute)
	if err := fs.Save(state); err != nil {
		t.Fatal(err)
	}
	pending, err := service.PendingExternal(started.RunID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Status != "reconcile_required" {
		t.Fatalf("pending=%#v", pending)
	}
	if _, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "publish", WorkerID: "worker-2", Declaration: assistant.CapabilityDeclaration{Protocol: assistant.EventProtocolV2}}); err == nil {
		t.Fatal("expired non-idempotent claim replayed without reconciliation")
	}
	state, err = fs.Load(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Nodes["publish"].External.Status != "reconcile_required" || state.Waiting == nil || state.Waiting.Kind != "external_reconcile" {
		t.Fatalf("state=%#v", state.Nodes["publish"].External)
	}
	if _, err := service.ReconcileExternal(context.Background(), ExternalReconcileRequest{RunID: started.RunID, NodeID: "publish", Outcome: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcileExternal(context.Background(), ExternalReconcileRequest{RunID: started.RunID, NodeID: "publish", Outcome: "not_applied", Receipt: "lookup:no-record"}); err != nil {
		t.Fatal(err)
	}
	claim2, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "publish", WorkerID: "worker-3", Declaration: assistant.CapabilityDeclaration{Protocol: assistant.EventProtocolV2}})
	if err != nil {
		t.Fatalf("claim after not_applied reconcile: %v", err)
	}
	state, err = fs.Load(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state.Nodes["publish"].External.LeaseExpiresAt = time.Now().Add(-time.Minute)
	if err := fs.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClaimExternal(ExternalClaimRequest{RunID: started.RunID, NodeID: "publish", WorkerID: "worker-4", Declaration: assistant.CapabilityDeclaration{Protocol: assistant.EventProtocolV2}}); err == nil {
		t.Fatal("second expired claim replayed without reconciliation")
	}
	if _, err := service.ReconcileExternal(context.Background(), ExternalReconcileRequest{RunID: started.RunID, NodeID: "publish", Outcome: "applied"}); err == nil {
		t.Fatal("applied reconciliation without receipt was accepted")
	}
	final, err := service.ReconcileExternal(context.Background(), ExternalReconcileRequest{
		RunID: started.RunID, NodeID: "publish", Outcome: "applied", Receipt: "remote:42",
		Submission: ExternalSubmission{Output: "published", Structured: json.RawMessage(`{"published":true}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != store.RunCompleted || final.Nodes["publish"].External.Receipt != "remote:42" || final.Nodes["publish"].Output != "published" {
		t.Fatalf("reconciled final state = %#v (prior claim=%#v)", final, claim2)
	}
}
