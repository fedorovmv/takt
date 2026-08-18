package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthoringContract(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	cfg := writeFile(t, work, ".takt/config.yaml", `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  limited:
    type: process
    argv: [/bin/cat]
`)
	typo := writeFile(t, work, "typo.yaml", `name: typo
nodes:
  - id: work
    prompt: hello
    provider: limited
    model: demo
    idle_timout: 10s
`)
	takt(t, nil, "validate", typo, "--config", cfg, "--workspace", work).RequireFailure(t).Contains(t, `did you mean "idle_timeout"`)
	capability := writeFile(t, work, "capability.yaml", `name: capability
nodes:
  - id: work
    prompt: hello
    provider: limited
    model: demo
    denied_tools: [write]
`)
	takt(t, nil, "validate", capability, "--config", cfg, "--workspace", work).RequireFailure(t).Contains(t, "capability validation").Contains(t, "tool_policy")
	outputRef := writeFile(t, work, "output-ref.yaml", `name: output-reference
nodes:
  - id: produce
    script:
      runtime: python
      inline: |
        print('{"summary":"ok"}')
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
      additionalProperties: false
  - id: consume
    depends_on: [produce]
    bash: printf '%s' $produce.output.summry
`)
	takt(t, nil, "validate", outputRef, "--config", cfg, "--workspace", work).RequireFailure(t).Contains(t, `output path "summry" is not declared`)
	render := writeFile(t, work, "render.yaml", `name: strict-renderer
nodes:
  - id: produce
    script:
      runtime: python
      inline: |
        print('{"summary":"ok","empty":""}')
    output_format:
      type: object
      properties:
        summary:
          type: string
        empty:
          type: string
        missing:
          type: string
      required: [summary]
      additionalProperties: false
  - id: consume
    depends_on: [produce]
    bash: printf '%s|%s|%s' $produce.output.summary '$produce.output.missing?' '$produce.output.empty:-fallback'
`)
	takt(t, nil, "validate", render, "--config", cfg, "--workspace", work, "--json").RequireSuccess(t)
	state := resultObject(t, takt(t, nil, "run", render, "--config", cfg, "--workspace", work, "--json").RequireSuccess(t).JSON(t))
	if stringField(t, state, "status") != "completed" {
		t.Fatalf("state=%#v", state)
	}
	consume := state["nodes"].(map[string]any)["consume"].(map[string]any)
	if stringField(t, consume, "output") != "ok||fallback" {
		t.Fatalf("consume=%#v", consume)
	}
	warning := writeFile(t, work, "warning.yaml", `name: warning
nodes:
  - id: work
    script:
      runtime: python
      inline: |
        print('{"summary":"ok"}')
    output_format:
      type: object
      properties:
        summary:
          type: string
`)
	report := resultObject(t, takt(t, nil, "validate", warning, "--config", cfg, "--workspace", work, "--json").RequireSuccess(t).JSON(t))
	found := false
	for _, raw := range report["diagnostics"].([]any) {
		d := raw.(map[string]any)
		if d["code"] == "schema.open_object" && d["severity"] == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatalf("schema warning missing: %#v", report)
	}
	takt(t, nil, "validate", warning, "--config", cfg, "--workspace", work, "--warnings-as-errors", "--json").RequireFailure(t)
	always := writeFile(t, work, "always.yaml", `name: always-run
nodes:
  - id: fail
    bash: exit 9
  - id: cleanup
    depends_on: [fail]
    always_run: true
    bash: printf cleaned > cleanup.txt
`)
	takt(t, nil, "run", always, "--config", cfg, "--workspace", work, "--json").RequireFailure(t)
	requireFileContains(t, filepath.Join(work, "cleanup.txt"), "cleaned")
}

func TestMCPContract(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	cfg := writeFile(t, work, "config.yaml", "apiVersion: takt/v1alpha1\nkind: Config\n")
	wf := writeFile(t, work, "workflow.yaml", `name: mcp-contract
nodes:
  - id: complete
    bash: printf 'mcp-ok'
`)
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "contract", "version": "1"}}},
		{"jsonrpc": "2.0", "id": 2, "method": "server/discover", "params": map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientInfo": map[string]any{"name": "contract", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/list", "params": map[string]any{}},
		{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "takt.run.start", "arguments": map[string]any{"selector": wf, "config_path": cfg, "detached": false}}},
	}
	var input strings.Builder
	for _, msg := range messages {
		b, _ := json.Marshal(msg)
		input.Write(b)
		input.WriteByte('\n')
	}
	output := taktInput(t, nil, input.String(), "mcp", "--surface", "all", "--workspace", work, "--config", cfg).RequireSuccess(t)
	responses := decodeJSONLines(t, output.Stdout)
	byID := map[float64]map[string]any{}
	for _, v := range responses {
		if id, ok := v["id"].(float64); ok {
			byID[id] = v
		}
	}
	if byID[1]["result"].(map[string]any)["protocolVersion"] != "2025-11-25" {
		t.Fatalf("initialize=%#v", byID[1])
	}
	if byID[2]["result"].(map[string]any)["protocolVersion"] != "2026-07-28" {
		t.Fatalf("discover=%#v", byID[2])
	}
	tools := byID[3]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 54 {
		t.Fatalf("all surface tools=%d", len(tools))
	}
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	if names[0] != "takt.task.start" || names[len(names)-1] != "takt.node.fail" {
		t.Fatalf("unexpected tool surface: %v", names)
	}
	call := byID[4]["result"].(map[string]any)
	if call["isError"] != false {
		t.Fatalf("call=%#v", call)
	}
	state := call["structuredContent"].(map[string]any)["state"].(map[string]any)
	if state["status"] != "completed" || state["nodes"].(map[string]any)["complete"].(map[string]any)["output"] != "mcp-ok" {
		t.Fatalf("state=%#v", state)
	}
	agent := taktInput(t, nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`+"\n", "mcp", "--workspace", work, "--config", cfg).RequireSuccess(t)
	agentTools := decodeJSONLines(t, agent.Stdout)[0]["result"].(map[string]any)["tools"].([]any)
	if len(agentTools) != 5 {
		t.Fatalf("agent tools=%d", len(agentTools))
	}
}

func TestDaemonContract(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	cfg := writeFile(t, work, ".takt/config.yaml", "apiVersion: takt/v1alpha1\nkind: Config\n")
	wf := writeFile(t, work, "workflow.yaml", `name: daemon-contract
nodes:
  - id: background
    bash: |
      sleep 1.5
      printf daemon-completed
`)
	t.Cleanup(func() { _ = takt(t, nil, "daemon", "stop", "--workspace", work, "--json").Err })
	start := resultObject(t, takt(t, nil, "daemon", "start", "--workspace", work, "--json").RequireSuccess(t).JSON(t))
	if !boolField(t, start, "started") {
		t.Fatalf("start=%#v", start)
	}
	pid := start["daemon"].(map[string]any)["pid"]
	for i := 0; i < 2; i++ {
		status := resultObject(t, takt(t, nil, "daemon", "status", "--workspace", work, "--json").RequireSuccess(t).JSON(t))
		if !boolField(t, status, "running") || status["daemon"].(map[string]any)["pid"] != pid {
			t.Fatalf("status=%#v", status)
		}
	}
	started := time.Now()
	runResult := resultObject(t, takt(t, nil, "run", wf, "--config", cfg, "--workspace", work, "--daemon", "--json").RequireSuccess(t).JSON(t))
	if time.Since(started) >= time.Second {
		t.Fatalf("detached daemon start took %s", time.Since(started))
	}
	runID := stringField(t, runResult, "run_id")
	if runID == "" {
		t.Fatal("empty run id")
	}
	events := takt(t, nil, "events", runID, "--workspace", work, "--daemon", "--follow", "--json").RequireSuccess(t)
	foundCompleted := false
	for _, event := range decodeJSONLines(t, events.Stdout) {
		if event["type"] == "run.completed" {
			foundCompleted = true
		}
	}
	if !foundCompleted {
		t.Fatalf("run.completed missing: %s", events.Stdout)
	}
	state := resultObject(t, takt(t, nil, "status", runID, "--workspace", work, "--json").RequireSuccess(t).JSON(t))
	if state["status"] != "completed" || state["output"] != "daemon-completed" {
		t.Fatalf("state=%#v", state)
	}
	mcp := taktInput(t, nil, `{"jsonrpc":"2.0","id":"daemon-tools","method":"tools/list","params":{}}`+"\n", "mcp", "--daemon", "--surface", "all", "--workspace", work).RequireSuccess(t)
	if len(decodeJSONLines(t, mcp.Stdout)[0]["result"].(map[string]any)["tools"].([]any)) != 54 {
		t.Fatalf("mcp=%s", mcp.Stdout)
	}
	takt(t, nil, "daemon", "stop", "--workspace", work, "--json").RequireSuccess(t)
	takt(t, nil, "daemon", "status", "--workspace", work, "--json").RequireFailure(t)
}

func TestTaskSourcesContract(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	fakebin := filepath.Join(tmp, "fakebin")
	if err := os.MkdirAll(fakebin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeAgent := binary(t, "takt-fake-code-agent")
	sourceBin := binary(t, "takt-github-task-source")
	gh := writeFile(t, fakebin, "gh", `#!/bin/sh
set -eu
printf '%s\n' '{"number":42,"title":"Implement ordinary repository change","body":"Update behavior.\n- [ ] tests pass\n- [ ] docs updated","url":"https://github.com/acme/app/issues/42","labels":[{"name":"backend"}],"state":"OPEN","updatedAt":"2026-08-08T12:00:00Z"}'
`)
	if err := os.Chmod(gh, 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	takt(t, nil, "init", "code", "--dir", work, "--json").RequireSuccess(t)
	cfg := writeFile(t, work, ".takt/config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: fixture
    id: routing
  implementation:
    provider: fixture
    id: implementation
  review:
    provider: fixture
    id: review
assistants:
  fixture:
    type: process
    argv: [%s]
    capabilities: [tool_policy, skills, mcp, sandbox_filesystem]
task_sources:
  github:
    transport: process
    argv: [%s]
    timeout: 5s
`, fakeAgent, sourceBin))
	git(t, work, "init", "-q")
	git(t, work, "config", "user.email", "fixture@example.com")
	git(t, work, "config", "user.name", "Fixture")
	writeFile(t, work, "base.txt", "base\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-qm", "base")
	env := []string{"PATH=" + fakebin + string(os.PathListSeparator) + os.Getenv("PATH")}
	start := resultObject(t, takt(t, env, "task", "start", "--workspace", work, "--source", "github", "--source-ref", "acme/app#42", "--profile", "code", "--json").RequireSuccess(t).JSON(t))
	assertTaskSource(t, start)
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"takt.task.start","arguments":{"source":"github","source_ref":"acme/app#42","profile":"code"}}}` + "\n"
	rpc := decodeJSONLines(t, taktInput(t, env, request, "mcp", "--workspace", work, "--config", cfg).RequireSuccess(t).Stdout)[0]
	result := rpc["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("rpc=%#v", rpc)
	}
	assertTaskSource(t, result["structuredContent"].(map[string]any))
}

func assertTaskSource(t *testing.T, value map[string]any) {
	t.Helper()
	source, ok := value["task_source"].(map[string]any)
	if !ok {
		t.Fatalf("task_source missing: %#v", value)
	}
	provenance := source["source"].(map[string]any)
	if provenance["adapter"] != "github" || provenance["reference"] != "acme/app#42" || provenance["revision"] == "" {
		t.Fatalf("source=%#v", source)
	}
	if len(source["acceptance"].([]any)) != 2 {
		t.Fatalf("acceptance=%#v", source["acceptance"])
	}
}

func decodeJSONLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(text))
	var result []map[string]any
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &v); err != nil {
			t.Fatalf("decode JSONL: %v\n%s", err, text)
		}
		result = append(result, v)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
