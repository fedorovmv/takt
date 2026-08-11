package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newCodeFixture(t *testing.T) (string, string) {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	takt(t, nil, "init", "code", "--dir", project, "--json").RequireSuccess(t)
	fake := binary(t, "takt-fake-code-agent")
	cfg := writeFile(t, project, ".takt/config.yaml", fmt.Sprintf(`apiVersion: takt/v1alpha1
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
`, fake))
	return project, cfg
}

func TestDynamicTaktContract(t *testing.T) {
	project, cfg := newCodeFixture(t)
	candidates := []string{
		"workflows/dynamic-plan.yaml", "workflows/dynamic-replan.yaml", "workflows/blocks/dynamic-discover.yaml", "workflows/blocks/dynamic-investigate.yaml", "workflows/blocks/dynamic-implement.yaml", "workflows/blocks/dynamic-validate.yaml", "workflows/blocks/dynamic-review.yaml", "workflows/blocks/dynamic-adversarial-verify.yaml", "workflows/blocks/dynamic-synthesize.yaml", "workflows/blocks/dynamic-repository-change.yaml", "workflows/blocks/dynamic-integration-verify.yaml",
	}
	base := filepath.Join(project, ".takt", "profiles", "code")
	for _, rel := range candidates {
		takt(t, nil, "validate", filepath.Join(base, rel), "--workspace", project, "--config", cfg, "--json").RequireSuccess(t)
	}
	catalog := resultObject(t, takt(t, nil, "block", "list", "--workspace", project, "--profile", "code", "--json").RequireSuccess(t).JSON(t))
	if len(catalog["blocks"].([]any)) != 11 || catalog["fingerprint"] == "" {
		t.Fatalf("catalog=%#v", catalog)
	}
	directPlan := resultObject(t, takt(t, nil, "plan", "fixture dynamic audit", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	id := stringField(t, directPlan, "plan_id")
	directExec := resultObject(t, takt(t, nil, "execute", id, "--confirm", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if directExec["status"] != "completed" {
		t.Fatalf("direct=%#v", directExec)
	}
	completed := directExec["completed_phases"].([]any)
	if len(completed) != 2 || completed[0] != "inventory" || completed[1] != "summary" {
		t.Fatalf("phases=%#v", completed)
	}

	t.Cleanup(func() { _ = takt(t, nil, "daemon", "stop", "--workspace", project, "--json").Err })
	takt(t, nil, "daemon", "start", "--workspace", project, "--json").RequireSuccess(t)
	plan := resultObject(t, takt(t, nil, "plan", "fixture dynamic audit", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	planID := stringField(t, plan, "plan_id")
	if plan["decision"] != "planned" || plan["requires_confirmation"] != true || !strings.Contains(plan["preview"].(string), "inventory") {
		t.Fatalf("plan=%#v", plan)
	}
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"takt.execute","arguments":{"plan_id":%q,"confirm":true}}}`+"\n", planID)
	rpc := decodeJSONLines(t, taktInput(t, nil, req, "mcp", "--daemon", "--surface", "all", "--workspace", project).RequireSuccess(t).Stdout)[0]
	rr := rpc["result"].(map[string]any)
	if rr["isError"] == true {
		t.Fatalf("rpc=%#v", rpc)
	}
	var view map[string]any
	requireEventually(t, 10*time.Second, func() bool {
		r := takt(t, nil, "plan", "get", planID, "--workspace", project, "--json")
		if r.Err != nil {
			return false
		}
		view = resultObject(t, r.JSON(t))
		record := view["record"].(map[string]any)
		return record["status"] == "completed"
	})
	record := view["record"].(map[string]any)
	if len(record["execution_run_ids"].([]any)) != 2 || view["artifact_count"].(float64) != 2 {
		t.Fatalf("view=%#v", view)
	}
	takt(t, nil, "plan", "promote", planID, "--name", "fixture-dynamic", "--workspace", project, "--json").RequireSuccess(t)
	generated := filepath.Join(project, ".takt", "workflows", "generated", "fixture-dynamic.yaml")
	requireFileContains(t, generated, "$ARGUMENTS")
	takt(t, nil, "validate", generated, "--workspace", project, "--config", cfg, "--json").RequireSuccess(t)
}

func TestSimpleReliableRouterContract(t *testing.T) {
	project, cfg := newCodeFixture(t)
	git(t, project, "init", "-q")
	git(t, project, "config", "user.email", "fixture@example.com")
	git(t, project, "config", "user.name", "Fixture")
	writeFile(t, project, "README.fixture", "fixture\n")
	// Enable one deterministic validation failure so repair behavior is exercised.
	configBytes, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(project, ".takt", "plans", ".dynamic-validation-fail-once")
	updated := strings.Replace(string(configBytes), "    argv: ["+binary(t, "takt-fake-code-agent")+"]\n", "    argv: ["+binary(t, "takt-fake-code-agent")+"]\n    env:\n      FAKE_DYNAMIC_VALIDATE_FAIL_ONCE_FILE: "+marker+"\n", 1)
	if err := os.WriteFile(cfg, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, project, "add", ".")
	git(t, project, "commit", "-qm", "fixture baseline")
	exclude := filepath.Join(project, ".git", "info", "exclude")
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(".takt/plans/\n.takt/runs/\n.takt/host-sessions/\n.takt/notifications/\n.takt/locks/\n")
	_ = f.Close()
	route := filepath.Join(project, ".takt", "profiles", "code", "workflows", "task-route.yaml")
	takt(t, nil, "validate", route, "--workspace", project, "--config", cfg, "--json").RequireSuccess(t)
	preview := resultObject(t, takt(t, nil, "task", "start", "Implement an ordinary repository change", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	planID := stringField(t, preview, "plan_id")
	if preview["kind"] != "plan" || preview["status"] != "draft" || preview["needs_input"] != true {
		t.Fatalf("preview=%#v", preview)
	}
	routeView := preview["route"].(map[string]any)
	if routeView["route"] != "template" || routeView["template"] != "simple-reliable" {
		t.Fatalf("route=%#v", routeView)
	}
	explain := resultObject(t, takt(t, nil, "task", "explain", planID, "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	phases := explain["plan"].(map[string]any)["phases"].([]any)
	want := []string{"investigate", "implement", "validate", "review"}
	for i, w := range want {
		if phases[i].(map[string]any)["uses"] != w {
			t.Fatalf("phases=%#v", phases)
		}
	}
	protected := resultObject(t, takt(t, nil, "task", "start", "Change the public API safely", "--go", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if protected["status"] != "completed" {
		t.Fatalf("protected=%#v", protected)
	}
	controls := protected["route"].(map[string]any)["controls"].(map[string]any)
	for _, k := range []string{"baseline", "independent_tests", "enhanced_review"} {
		if controls[k] != true {
			t.Fatalf("controls=%#v", controls)
		}
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	repair := resultObject(t, takt(t, nil, "task", "start", "Implement change with recoverable validation failure", "--go", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if repair["status"] != "completed" {
		t.Fatalf("repair=%#v", repair)
	}
	repairID := stringField(t, repair, "plan_id")
	detail := resultObject(t, takt(t, nil, "task", "explain", repairID, "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	record := detail["plan"].(map[string]any)["record"].(map[string]any)
	attempts := record["repair_attempts"].(map[string]any)
	if attempts["validate:deterministic-validation"].(float64) != 1 {
		t.Fatalf("attempts=%#v", attempts)
	}
	agent := decodeJSONLines(t, taktInput(t, nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`+"\n", "mcp", "--workspace", project).RequireSuccess(t).Stdout)[0]
	if len(agent["result"].(map[string]any)["tools"].([]any)) != 5 {
		t.Fatalf("agent=%#v", agent)
	}
	requireFileContains(t, filepath.Join(repoRoot, "schemas", "config.schema.json"), "default_assistant", "takt-assistant/v1alpha2")
	requireFileContains(t, filepath.Join(project, ".takt", "profiles", "code", "workflows", "task-route.yaml"), "provider: coding-agent")
}
