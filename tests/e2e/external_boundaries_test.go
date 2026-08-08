package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPackageDistributionBoundary(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	pkg := filepath.Join(tmp, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + filepath.Join(tmp, "home")}
	if err := os.MkdirAll(filepath.Join(tmp, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, pkg, "workflow.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: portable-extra
nodes:
  - id: result
    prompt: Return JSON summary.
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`)
	writeFile(t, pkg, "package.yaml", `apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: portable-extra
  version: 1.0.0
  scope: project
blocks:
  portable-extra:
    workflow: workflow.yaml
    output_paths: [summary]
requirements:
  takt: ">=0.1.42"
  adapters:
    - name: scm
      domain: scm
      operations: [change.get]
      level: preferred
`)
	takt(t, env, "init", "code", "--dir", project, "--json").RequireSuccess(t)
	takt(t, env, "package", "install", pkg, "--scope", "project", "--workspace", project, "--json").RequireSuccess(t).Contains(t, "portable-extra")
	if _, err := os.Stat(filepath.Join(project, ".takt", "takt.lock.json")); err != nil {
		t.Fatal(err)
	}
	takt(t, env, "package", "list", "--workspace", project, "--json").RequireSuccess(t).Contains(t, "portable-extra")
	doctor := resultObject(t, takt(t, env, "package", "doctor", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if doctor["status"] != "ready" {
		t.Fatalf("doctor=%#v", doctor)
	}
	preflight, ok := doctor["adapter_preflight"].([]any)
	if !ok || len(preflight) == 0 || preflight[0].(map[string]any)["available"] != false {
		t.Fatalf("adapter_preflight=%#v", doctor["adapter_preflight"])
	}
	takt(t, env, "block", "list", "--profile", "code", "--workspace", project, "--json").RequireSuccess(t).Contains(t, "portable-extra")

	installed := filepath.Join(project, ".takt", "packages", "project", "portable-extra", "1.0.0", "workflow.yaml")
	if err := os.WriteFile(installed, []byte("corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	takt(t, env, "package", "doctor", "--workspace", project, "--json").RequireFailure(t)
	takt(t, env, "package", "sync", "--workspace", project, "--json").RequireSuccess(t).Contains(t, `"status": "ready"`)
	takt(t, env, "package", "doctor", "--workspace", project, "--json").RequireSuccess(t)
	takt(t, env, "package", "uninstall", "portable-extra", "--scope", "project", "--workspace", project, "--json").RequireSuccess(t)
	if strings.Contains(takt(t, env, "block", "list", "--profile", "code", "--workspace", project, "--json").RequireSuccess(t).Stdout, "portable-extra") {
		t.Fatal("uninstalled package remains in profile catalog")
	}

	fakeAssistant := binary(t, "takt-fake-assistant")
	writeFile(t, project, ".takt/config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: local
    id: routing
  implementation:
    provider: local
    id: implementation
  review:
    provider: local
    id: review
assistants:
  fixture:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [%s, --case, portable-package]
    capabilities: [skills, mcp]
`, fakeAssistant))
	takt(t, env, "package", "install", filepath.Join(repoRoot, "examples", "portable-package"), "--scope", "project", "--workspace", project, "--json").RequireSuccess(t)
	exampleRoot := filepath.Join(project, ".takt", "packages", "project", "portable-review", "1.0.0")
	info, err := os.Stat(filepath.Join(exampleRoot, "scripts", "inventory.sh"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("portable script is not executable: %v mode=%v", err, info)
	}
	for _, rel := range []string{"commands/package-review.md", "skills/review/SKILL.md", "mcp.json"} {
		if _, err := os.Stat(filepath.Join(exampleRoot, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	run := resultObject(t, takt(t, env, "run", filepath.Join(exampleRoot, "workflow.yaml"), "--workspace", project, "--config", filepath.Join(project, ".takt", "config.yaml"), "--json").RequireSuccess(t).JSON(t))
	if run["status"] != "completed" || !strings.Contains(fmt.Sprint(run), "portable package review passed") {
		t.Fatalf("portable package run=%#v", run)
	}
}

func TestReferenceAdaptersBoundary(t *testing.T) {
	tmp := t.TempDir()
	qwenWork := filepath.Join(tmp, "qwen-work")
	scmWork := filepath.Join(tmp, "scm-work")
	for _, dir := range []string{qwenWork, scmWork} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	qwenAdapter := binary(t, "qwen-takt-adapter")
	githubAdapter := binary(t, "takt-github-scm-adapter")
	qwenArgs := filepath.Join(tmp, "qwen-args.txt")
	qwen := writeFile(t, tmp, "qwen", `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$QWEN_FIXTURE_ARGS"
session=qwen-session-1
prev=""
for arg in "$@"; do
  if [ "$prev" = "--resume" ]; then session="$arg"; fi
  prev="$arg"
done
printf '{"type":"system","subtype":"session_start","session_id":"%s","model":"qwen-reference"}\n' "$session"
printf '{"type":"assistant","session_id":"%s","message":{"content":[{"type":"text","text":"reference wrapper completed"}]}}\n' "$session"
printf '{"type":"result","subtype":"success","session_id":"%s","is_error":false,"result":"reference wrapper completed","usage":{"input_tokens":11,"output_tokens":5}}\n' "$session"
`)
	if err := os.Chmod(qwen, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, qwenWork, "config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
models:
  default:
    provider: qwen
    id: qwen3-coder
assistants:
  qwen-reference:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [%s]
    env:
      QWEN_TAKT_QWEN_BINARY: %s
      QWEN_FIXTURE_ARGS: %s
    capabilities: [agent_events_v2, session_events, usage_events]
`, qwenAdapter, qwen, qwenArgs))
	writeFile(t, qwenWork, "workflow.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: qwen-reference-adapter
defaults:
  assistant: qwen-reference
  model: default
  session: fresh
nodes:
  - id: execute
    prompt: Return a short result.
`)
	qwenRun := takt(t, nil, "run", filepath.Join(qwenWork, "workflow.yaml"), "--config", filepath.Join(qwenWork, "config.yaml"), "--workspace", qwenWork, "--json").RequireSuccess(t)
	qwenRun.Contains(t, `"status": "completed"`).Contains(t, "reference wrapper completed")
	requireFileContains(t, qwenArgs, "--safe-mode", "--output-format")

	git(t, scmWork, "init", "-q")
	git(t, scmWork, "remote", "add", "origin", "git@github.com:acme/reference.git")
	ghLog, ghBody := filepath.Join(tmp, "gh.log"), filepath.Join(tmp, "gh-body.txt")
	gh := writeFile(t, tmp, "gh", `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_FIXTURE_LOG"
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "--body" ]; then printf '%s' "$arg" > "$GH_FIXTURE_BODY"; fi
    prev="$arg"
  done
  echo 'transport lost after mutation' >&2
  exit 1
fi
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  body=$(cat "$GH_FIXTURE_BODY")
  printf '[{"number":17,"url":"https://github.com/acme/reference/pull/17","body":"%s"}]\n' "$body"
  exit 0
fi
printf '{}\n'
`)
	if err := os.Chmod(gh, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, scmWork, "config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
adapters:
  scm:
    domain: scm
    transport: process
    argv: [%s]
    env:
      TAKT_GITHUB_GH_BINARY: %s
      GH_FIXTURE_LOG: %s
      GH_FIXTURE_BODY: %s
    timeout: 10s
`, githubAdapter, gh, ghLog, ghBody))
	writeFile(t, scmWork, "workflow.yaml", `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: github-reference-reconcile
nodes:
  - id: publish
    adapter:
      name: scm
      operation: change.create
      input: '{"title":"Reference change","head":"takt/reference"}'
    side_effect:
      mode: reconcile
      idempotency_key: reference-change-1
`)
	takt(t, nil, "adapter", "doctor", "scm", "--workspace", scmWork, "--config", filepath.Join(scmWork, "config.yaml"), "--json").RequireSuccess(t).Contains(t, "change.create")
	scmRun := takt(t, nil, "run", filepath.Join(scmWork, "workflow.yaml"), "--config", filepath.Join(scmWork, "config.yaml"), "--workspace", scmWork, "--json").RequireSuccess(t)
	scmRun.Contains(t, `"status": "completed"`).Contains(t, `"reconcile_status": "applied"`).Contains(t, "https://github.com/acme/reference/pull/17")
	logBytes, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	create, list := 0, 0
	for _, line := range lines {
		if strings.HasPrefix(line, "pr create") {
			create++
		}
		if strings.HasPrefix(line, "pr list") {
			list++
		}
	}
	if create != 1 || list != 1 {
		t.Fatalf("gh calls create=%d list=%d: %s", create, list, logBytes)
	}
	body, err := os.ReadFile(ghBody)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "reference-change-1") || !strings.Contains(string(body), "takt-idempotency:") {
		t.Fatalf("unexpected GitHub idempotency marker body: %s", body)
	}
}

func TestHostControlBoundary(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	fakeAgent := binary(t, "takt-fake-code-agent")
	takt(t, nil, "init", "code", "--dir", project, "--json").RequireSuccess(t)
	writeFile(t, project, ".takt/config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: opencode
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
  opencode:
    type: process
    argv: [%s]
    capabilities: [tool_policy, skills, sandbox_filesystem]
`, fakeAgent))
	t.Cleanup(func() { _ = takt(t, nil, "daemon", "stop", "--workspace", project, "--json").Err })
	takt(t, nil, "daemon", "start", "--workspace", project, "--json").RequireSuccess(t)
	takt(t, nil, "host", "begin", "fixture dynamic audit", "--host", "pi", "--host-session", "incomplete", "--enforcement", "strict", "--tool-call-blocking", "--workspace", project, "--daemon", "--json").RequireFailure(t)

	begin := resultObject(t, takt(t, nil, "host", "begin", "fixture dynamic audit", "--host", "pi", "--host-session", "pi-session-1", "--enforcement", "strict", "--command-interception", "--input-interception", "--tool-call-blocking", "--completion-blocking", "--session-recovery", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	session := begin["session"].(map[string]any)
	if session["enforcement"] != "strict" || session["status"] != "preview" || !strings.Contains(begin["plan"].(map[string]any)["preview"].(string), "Budget:") {
		t.Fatalf("begin=%#v", begin)
	}
	sessionID := stringField(t, session, "id")
	takt(t, nil, "daemon", "stop", "--workspace", project, "--json").RequireSuccess(t)
	takt(t, nil, "daemon", "start", "--workspace", project, "--json").RequireSuccess(t)
	found := resultObject(t, takt(t, nil, "host", "find", "--host", "pi", "--host-session", "pi-session-1", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	if found["session"].(map[string]any)["id"] != sessionID {
		t.Fatalf("find=%#v", found)
	}
	edit := resultObject(t, takt(t, nil, "host", "guard-tool", sessionID, "--tool", "edit", "--read-only", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	grep := resultObject(t, takt(t, nil, "host", "guard-tool", sessionID, "--tool", "grep", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	final := resultObject(t, takt(t, nil, "host", "guard-completion", sessionID, "--kind", "final", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	if edit["allowed"] != false || grep["allowed"] != true || final["allowed"] != false {
		t.Fatalf("guards edit=%#v grep=%#v final=%#v", edit, grep, final)
	}
	takt(t, nil, "host", "confirm", sessionID, "--confirm", "--workspace", project, "--daemon", "--json").RequireSuccess(t)
	var status map[string]any
	requireEventually(t, 30*time.Second, func() bool {
		result := takt(t, nil, "host", "status", sessionID, "--workspace", project, "--daemon", "--json")
		if result.Err != nil {
			return false
		}
		status = resultObject(t, result.JSON(t))
		value := status["session"].(map[string]any)["status"]
		return value == "completed" || value == "failed" || value == "released"
	})
	if status["session"].(map[string]any)["status"] != "completed" {
		t.Fatalf("status=%#v", status)
	}
	done := resultObject(t, takt(t, nil, "host", "guard-completion", sessionID, "--kind", "final", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	if done["allowed"] != true {
		t.Fatalf("done=%#v", done)
	}
	again := resultObject(t, takt(t, nil, "host", "begin", "fixture dynamic audit again", "--host", "pi", "--host-session", "pi-session-1", "--enforcement", "guarded", "--command-interception", "--input-interception", "--tool-call-blocking", "--session-recovery", "--workspace", project, "--daemon", "--json").RequireSuccess(t).JSON(t))
	againSession := again["session"].(map[string]any)
	if againSession["status"] != "preview" || againSession["id"] == sessionID {
		t.Fatalf("again=%#v", again)
	}
}

func TestDeepCodeWorkflowBoundary(t *testing.T) {
	tmp := t.TempDir()
	project, remote := filepath.Join(tmp, "project"), filepath.Join(tmp, "remote.git")
	fakeBin, ghState := filepath.Join(tmp, "bin"), filepath.Join(tmp, "gh-state")
	for _, dir := range []string{project, fakeBin, ghState} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(t, filepath.Join(repoRoot, "scripts", "fixtures", "fake-gh"), filepath.Join(fakeBin, "gh"), 0o755)
	env := []string{"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"), "FAKE_GH_STATE_DIR=" + ghState}
	run(t, tmp, nil, nil, "git", "init", "--bare", remote).RequireSuccess(t)
	run(t, tmp, nil, nil, "git", "init", "-b", "main", project).RequireSuccess(t)
	git(t, project, "config", "user.name", "Takt Fixture")
	git(t, project, "config", "user.email", "takt@example.test")
	writeFile(t, project, "app.txt", "initial\n")
	git(t, project, "add", "app.txt")
	git(t, project, "commit", "-m", "initial")
	git(t, project, "remote", "add", "origin", remote)
	git(t, project, "push", "-u", "origin", "main")
	takt(t, env, "init", "code", "--dir", project, "--json").RequireSuccess(t)
	writeFile(t, project, ".takt/config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: opencode
models:
  routing:
    provider: fixture
    id: routing
  review:
    provider: fixture
    id: review
  implementation:
    provider: fixture
    id: implementation
assistants:
  opencode:
    type: process
    argv: [%s]
    capabilities: [tool_policy, skills, sandbox_filesystem]
`, binary(t, "takt-fake-code-agent")))
	git(t, project, "add", ".takt")
	git(t, project, "commit", "-m", "add Takt code profile")
	git(t, project, "push", "origin", "main")
	input := writeFile(t, tmp, "fix.json", `{"repository":"acme/repo","issue_number":1,"base_branch":"main","draft_pr":true,"validation_commands":["test -s app.txt"],"scope_limits":["Only app.txt"]}`)
	result := takt(t, env, "run", "code:fix-github-issue", "--workspace", project, "--input", input, "--json").RequireSuccess(t)
	for {
		state := resultObject(t, result.JSON(t))
		if state["status"] != "waiting" {
			break
		}
		waiting, _ := state["waiting"].(map[string]any)
		nodeID, _ := waiting["node_id"].(string)
		if nodeID == "" {
			t.Fatalf("waiting Run has no node_id: %#v", state)
		}
		result = takt(t, env, "answer", stringField(t, state, "id"), nodeID, "--workspace", project, "--value", "ready", "--json").RequireSuccess(t)
	}
	state := resultObject(t, result.JSON(t))
	if state["status"] != "completed" {
		t.Fatalf("run=%#v", state)
	}
	expected := map[string]bool{"issue-intake": true, "investigation": true, "reproduction": true, "fix-plan": true, "implementation-report": true, "validation-report": true, "pr-metadata": true, "workflow-summary": true}
	for _, raw := range state["artifacts"].([]any) {
		if typ, ok := raw.(map[string]any)["type"].(string); ok {
			delete(expected, typ)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing artifacts=%v", expected)
	}
	requireFileContains(t, filepath.Join(ghState, "pr-count"), "1")
	if !strings.Contains(git(t, project, "--git-dir="+remote, "show-ref", "--heads"), "refs/heads/takt/") {
		t.Fatal("managed takt branch missing")
	}
	invalid := writeFile(t, tmp, "invalid.json", `{"repository":"acme/repo","issue_number":1}`)
	before, err := os.ReadFile(filepath.Join(ghState, "pr-count"))
	if err != nil {
		t.Fatal(err)
	}
	takt(t, env, "run", "code:fix-github-issue", "--workspace", project, "--input", invalid, "--json").RequireFailure(t).Contains(t, "workflow input")
	after, err := os.ReadFile(filepath.Join(ghState, "pr-count"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("invalid input caused side effect: before=%q after=%q", before, after)
	}
}

func TestHostIntegrationSourceContract(t *testing.T) {
	piPath := filepath.Join(repoRoot, "integrations", "coding-agent-host-control", "pi", "index.ts")
	opencodePath := filepath.Join(repoRoot, "integrations", "coding-agent-host-control", "opencode", "index.ts")
	piBytes, err := os.ReadFile(piPath)
	if err != nil {
		t.Fatal(err)
	}
	opencodeBytes, err := os.ReadFile(opencodePath)
	if err != nil {
		t.Fatal(err)
	}
	pi, opencode := string(piBytes), string(opencodeBytes)
	for _, needle := range []string{`envelope.result`, `pi.registerCommand("takt"`, `pi.on("input"`, `pi.on("tool_call"`, `return { action: "handled" as const }`, `["host", "status", cached.id]`, `"--enforcement", "guarded"`} {
		if !strings.Contains(pi, needle) {
			t.Fatalf("Pi integration missing %q", needle)
		}
	}
	if strings.Contains(pi, "before_agent_start") || strings.Contains(pi, "completion-blocking") {
		t.Fatal("Pi integration advertises unsupported lifecycle hook")
	}
	for _, needle := range []string{`envelope.result`, `ctx.session.hook("context"`, `ctx.tool.hook("execute.before"`, `ctx.shell.hook("create.before"`, `The main LLM was not invoked`, `["host", "status", cached.id]`, `"--enforcement", "guarded"`} {
		if !strings.Contains(opencode, needle) {
			t.Fatalf("OpenCode integration missing %q", needle)
		}
	}
	if strings.Contains(opencode, "Plugin.define") || strings.Contains(opencode, `from "@opencode-ai/plugin"`) {
		t.Fatal("OpenCode integration must not import a runtime Plugin value or use Plugin.define")
	}
	for _, rel := range []string{"opencode/package.json", "pi/package.json"} {
		path := filepath.Join(repoRoot, "integrations", "coding-agent-host-control", filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		for _, section := range []string{"dependencies", "devDependencies"} {
			deps, _ := manifest[section].(map[string]any)
			for name, raw := range deps {
				version, _ := raw.(string)
				if version == "next" || version == "*" {
					t.Fatalf("%s %s uses floating version %q", rel, name, version)
				}
			}
		}
	}
	opManifest := filepath.Join(repoRoot, "integrations", "coding-agent-host-control", "opencode", "package.json")
	opData, _ := os.ReadFile(opManifest)
	if !strings.Contains(string(opData), `"verified": false`) || !strings.Contains(string(opData), `"enforcement": "guarded"`) {
		t.Fatal("OpenCode package metadata must declare guarded unverified integration")
	}
}
