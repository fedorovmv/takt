package domainadapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"takt/internal/spec"
	"takt/internal/version"
)

func TestProcessTransportDescribeInvokeAndReconcile(t *testing.T) {
	argv := []string{os.Args[0], "-test.run=TestDomainAdapterHelper"}
	adapter := &Process{Spec: spec.DomainAdapterSpec{Domain: "scm", Transport: "process", Argv: argv, Env: map[string]string{"TAKT_DOMAIN_HELPER": "process"}}}
	dec, err := adapter.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !HasCapability(dec, "change.create") || !SupportsReconcile(dec, "change.create") {
		t.Fatalf("declaration=%#v", dec)
	}
	workspace := t.TempDir()
	result, err := adapter.Invoke(context.Background(), InvokeRequest{RunID: "run-1", NodeID: "publish", Attempt: 1, Workspace: workspace, Domain: "scm", Operation: "change.create", Input: json.RawMessage(`{"title":"x"}`), IdempotencyKey: "k1", SideEffectMode: "reconcile"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Receipt != "r:k1" || !strings.Contains(string(result.Output), workspace) {
		t.Fatalf("result=%#v", result)
	}
	rec, err := adapter.Reconcile(context.Background(), ReconcileRequest{RunID: "run-1", NodeID: "publish", Workspace: workspace, Domain: "scm", Operation: "change.create", IdempotencyKey: "k1", Receipt: result.Receipt})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome != "applied" || rec.Receipt != "r:k1" {
		t.Fatalf("reconcile=%#v", rec)
	}
}

func TestMCPTransportDiscoversMappedCapabilitiesAndCallsTool(t *testing.T) {
	argv := []string{os.Args[0], "-test.run=TestDomainAdapterHelper"}
	workspace := t.TempDir()
	adapter := &MCP{Spec: spec.DomainAdapterSpec{Domain: "tracker", Transport: "mcp", Argv: argv, Env: map[string]string{"TAKT_DOMAIN_HELPER": "mcp", "TAKT_EXPECT_CLIENT_VERSION": version.Value}, Operations: map[string]string{"item.get": "corp_item_get"}, ReconcileOperations: map[string]string{"item.get": "corp_item_get_reconcile"}}}
	dec, err := adapter.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !HasCapability(dec, "item.get") || !SupportsReconcile(dec, "item.get") {
		t.Fatalf("declaration=%#v", dec)
	}
	result, err := adapter.Invoke(context.Background(), InvokeRequest{RunID: "run-2", NodeID: "get-item", Attempt: 1, Workspace: workspace, Domain: "tracker", Operation: "item.get", Input: json.RawMessage(`{"id":"ABC-1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || !strings.Contains(string(result.Output), "ABC-1") || !strings.Contains(string(result.Output), workspace) {
		t.Fatalf("result=%#v", result)
	}
	rec, err := adapter.Reconcile(context.Background(), ReconcileRequest{RunID: "run-2", NodeID: "get-item", Workspace: workspace, Domain: "tracker", Operation: "item.get", Input: json.RawMessage(`{}`), IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Outcome != "applied" {
		t.Fatalf("reconcile=%#v", rec)
	}
}

func TestMCPTransportResolvesSecretEnv(t *testing.T) {
	t.Setenv("DOMAIN_MCP_TOKEN", "super-secret-value")
	argv := []string{os.Args[0], "-test.run=TestDomainAdapterHelper"}
	adapter := &MCP{Spec: spec.DomainAdapterSpec{Domain: "tracker", Transport: "mcp", Argv: argv, Env: map[string]string{"TAKT_DOMAIN_HELPER": "mcp-secret", "TOKEN": "secret://DOMAIN_MCP_TOKEN"}, Operations: map[string]string{"item.get": "corp_item_get"}}}
	if _, err := adapter.Describe(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMCPTransportMissingSecretFailsClosed(t *testing.T) {
	_ = os.Unsetenv("TAKT_MISSING_DOMAIN_MCP_SECRET")
	argv := []string{os.Args[0], "-test.run=TestDomainAdapterHelper"}
	adapter := &MCP{Spec: spec.DomainAdapterSpec{Domain: "tracker", Transport: "mcp", Argv: argv, Env: map[string]string{"TOKEN": "secret://TAKT_MISSING_DOMAIN_MCP_SECRET"}}}
	if _, err := adapter.Describe(context.Background()); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessTransportEnforcesMaxOutputBytes(t *testing.T) {
	argv := []string{os.Args[0], "-test.run=TestDomainAdapterHelper"}
	adapter := &Process{Spec: spec.DomainAdapterSpec{Domain: "scm", Transport: "process", Argv: argv, Env: map[string]string{"TAKT_DOMAIN_HELPER": "process-overflow"}, MaxOutputBytes: 128}}
	if _, err := adapter.Describe(context.Background()); err == nil || !strings.Contains(err.Error(), "output exceeds") {
		t.Fatalf("expected max_output_bytes error, got %v", err)
	}
}

func TestDomainAdapterHelper(t *testing.T) {
	mode := os.Getenv("TAKT_DOMAIN_HELPER")
	if mode == "" {
		return
	}
	if mode == "process" {
		helperProcess()
		return
	}
	if mode == "mcp" || mode == "mcp-secret" {
		if mode == "mcp-secret" && os.Getenv("TOKEN") != "super-secret-value" {
			os.Exit(7)
		}
		helperMCP()
		return
	}
	if mode == "process-overflow" {
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", 1024)))
		os.Exit(0)
	}
	os.Exit(2)
}

func helperProcess() {
	var req processEnvelope
	if json.NewDecoder(os.Stdin).Decode(&req) != nil {
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	switch req.Kind {
	case "DescribeRequest":
		_ = enc.Encode(processResponse{APIVersion: ProtocolV1Alpha1, Kind: "DescribeResponse", Declaration: &Declaration{APIVersion: ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "scm", Capabilities: []string{"change.create"}, Reconcile: []string{"change.create"}}})
	case "InvokeRequest":
		cwd, _ := os.Getwd()
		out, _ := json.Marshal(map[string]string{"change": "1", "cwd": cwd})
		_ = enc.Encode(processResponse{APIVersion: ProtocolV1Alpha1, Kind: "InvokeResponse", Result: &Result{Status: "completed", Output: out, Receipt: "r:" + req.Request.IdempotencyKey}})
	case "ReconcileRequest":
		_ = enc.Encode(processResponse{APIVersion: ProtocolV1Alpha1, Kind: "ReconcileResponse", Reconcile: &ReconcileResult{Outcome: "applied", Output: json.RawMessage(`{"change":"1"}`), Receipt: "r:" + req.Reconcile.IdempotencyKey}})
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func helperMCP() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var msg map[string]any
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		id, ok := msg["id"]
		if !ok {
			continue
		}
		method, _ := msg["method"].(string)
		var result any
		switch method {
		case "initialize":
			if expected := os.Getenv("TAKT_EXPECT_CLIENT_VERSION"); expected != "" {
				params, _ := msg["params"].(map[string]any)
				client, _ := params["clientInfo"].(map[string]any)
				if fmt.Sprint(client["version"]) != expected {
					os.Exit(8)
				}
			}
			result = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "helper", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "corp_item_get", "inputSchema": map[string]any{"type": "object"}}, {"name": "corp_item_get_reconcile", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			params, _ := msg["params"].(map[string]any)
			name, _ := params["name"].(string)
			args, _ := params["arguments"].(map[string]any)
			if name == "corp_item_get_reconcile" {
				result = map[string]any{"structuredContent": map[string]any{"outcome": "applied", "receipt": "r:k"}, "content": []any{}}
			} else {
				cwd, _ := os.Getwd()
				result = map[string]any{"structuredContent": map[string]any{"status": "completed", "output": map[string]any{"id": fmt.Sprint(args["id"]), "cwd": cwd}}, "content": []any{}}
			}
		default:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "not found"}})
			continue
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	os.Exit(0)
}

func TestProcessTimeoutDefaultsAndRejectsInvalid(t *testing.T) {
	got, err := processTimeout("")
	if err != nil || got != 10*time.Second {
		t.Fatalf("default timeout=%s err=%v", got, err)
	}
	got, err = processTimeout("250ms")
	if err != nil || got != 250*time.Millisecond {
		t.Fatalf("explicit timeout=%s err=%v", got, err)
	}
	for _, raw := range []string{"0s", "-1s", "wat"} {
		if _, err := processTimeout(raw); err == nil {
			t.Fatalf("timeout %q should fail", raw)
		}
	}
}

func TestMCPTransportRejectsNonPositiveTimeoutBeforeStartingServer(t *testing.T) {
	m := MCP{Spec: spec.DomainAdapterSpec{Domain: "tracker", Transport: "mcp", Argv: []string{"definitely-not-started"}, Timeout: "0s"}}
	if _, err := m.start(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("unexpected error: %v", err)
	}
}
