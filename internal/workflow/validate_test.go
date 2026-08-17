package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"takt/internal/spec"
	"testing"
)

func TestHookDecisionDirectJSONKeepsPresenceAndCompatibility(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantAction  string
		wantSession string
		wantPresent bool
	}{
		{name: "omitted", raw: `{"action":"continue"}`, wantAction: "continue"},
		{name: "empty", raw: `{"action":"retry","session":""}`, wantAction: "retry", wantPresent: true},
		{name: "null", raw: `{"action":"retry","session":null}`, wantAction: "retry", wantPresent: true},
		{name: "fresh", raw: `{"action":"retry","session":"fresh"}`, wantAction: "retry", wantSession: "fresh", wantPresent: true},
		{name: "resume", raw: `{"action":"retry","session":"resume"}`, wantAction: "retry", wantSession: "resume", wantPresent: true},
		{name: "valid with unknown", raw: `{"action":"retry","session":"fresh","future":"ignored"}`, wantAction: "retry", wantSession: "fresh", wantPresent: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var decision spec.HookDecision
			if err := json.Unmarshal([]byte(tc.raw), &decision); err != nil {
				t.Fatalf("direct JSON decode failed: %v", err)
			}
			check := func(label string, got spec.HookDecision) {
				t.Helper()
				if got.Action != tc.wantAction || got.Session != tc.wantSession || got.HasSession() != tc.wantPresent {
					t.Fatalf("%s decision=%+v, has_session=%v", label, got, got.HasSession())
				}
			}
			check("decoded", decision)

			encoded, err := json.Marshal(decision)
			if err != nil {
				t.Fatalf("direct JSON encode failed: %v", err)
			}
			var roundTrip spec.HookDecision
			if err := json.Unmarshal(encoded, &roundTrip); err != nil {
				t.Fatalf("direct JSON round-trip decode failed: %v", err)
			}
			check("round-trip", roundTrip)
		})
	}
}

func TestValidateDetectsCycle(t *testing.T) {
	wf := &spec.Workflow{Name: "x", Nodes: []spec.Node{{ID: "a", Bash: "true", DependsOn: []string{"b"}}, {ID: "b", Bash: "true", DependsOn: []string{"a"}}}}
	if err := Validate(wf); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestRejectsInvalidTimeout(t *testing.T) {
	wf := &spec.Workflow{Name: "bad-timeout", Nodes: []spec.Node{{ID: "n", Bash: "true", Timeout: "never"}}}
	if err := Validate(wf); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestValidateRejectsNestedLoopGroups(t *testing.T) {
	zero := 0
	wf := &spec.Workflow{Name: "nested", Nodes: []spec.Node{{
		ID: "outer", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "inner", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{ID: "check", Bash: "true"}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
		}}, Until: spec.UntilSpec{Node: "inner", ExitCode: &zero}},
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "nested loop_group") {
		t.Fatalf("expected nested loop validation error, got %v", err)
	}
}

func TestValidateRejectsUnboundedLoopHistory(t *testing.T) {
	zero := 0
	wf := &spec.Workflow{Name: "bounded-loop", Nodes: []spec.Node{{
		ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 65, Nodes: []spec.Node{{ID: "check", Bash: "true"}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "max_iterations must be <= 64") {
		t.Fatalf("expected bounded loop history validation error, got %v", err)
	}
}

func TestValidateAcceptsGovernedWorkflowNode(t *testing.T) {
	wf := &spec.Workflow{Name: "parent", Nodes: []spec.Node{{
		ID: "child", WorkflowRun: &spec.WorkflowRunSpec{Path: "child.yaml", Input: "$ARGUMENTS", Isolation: "inherit"},
	}}}
	if err := Validate(wf); err != nil {
		t.Fatalf("governed workflow node was rejected: %v", err)
	}
}

func TestValidateRejectsGovernedWorkflowIsolation(t *testing.T) {
	wf := &spec.Workflow{Name: "parent", Nodes: []spec.Node{{
		ID: "child", WorkflowRun: &spec.WorkflowRunSpec{Path: "child.yaml", Isolation: "shared"},
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("expected governed workflow isolation error, got %v", err)
	}
}

func TestLoadRejectsGovernedWorkflowRecursion(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(first, []byte(`name: first
nodes:
  - id: child
    workflow:
      path: second.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(`name: second
nodes:
  - id: child
    workflow:
      path: first.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(first)
	if err == nil || !strings.Contains(err.Error(), "recursive governed child workflow reference") {
		t.Fatalf("governed recursion was not rejected during validation: %v", err)
	}
}

func TestValidateRejectsAssistantPolicyOnBashNode(t *testing.T) {
	allowedTools := []string{"read"}
	wf := &spec.Workflow{Name: "bad-policy", Nodes: []spec.Node{{
		ID: "shell", Bash: "true", AllowedTools: &allowedTools,
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "command or prompt") {
		t.Fatalf("expected policy placement error, got %v", err)
	}
}

func TestLoadRejectsMissingMCPPolicyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(`name: missing-mcp
nodes:
  - id: agent
    prompt: test
    mcp: missing.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "read MCP config") {
		t.Fatalf("missing MCP policy file was not rejected: %v", err)
	}
}

func TestLoadPreservesExplicitEmptyPolicyAllowlists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(`name: empty-policy
nodes:
  - id: classify
    prompt: classify
    allowed_tools: []
    skills: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	node := wf.Nodes[0]
	if node.AllowedTools == nil || len(*node.AllowedTools) != 0 {
		t.Fatalf("explicit empty allowed_tools was lost: %#v", node.AllowedTools)
	}
	if node.Skills == nil || len(*node.Skills) != 0 {
		t.Fatalf("explicit empty skills was lost: %#v", node.Skills)
	}
}

func TestValidateGovernedFanOutRequiresUpstreamArraySource(t *testing.T) {
	wf := &spec.Workflow{Name: "fanout", Nodes: []spec.Node{
		{ID: "discover", Bash: "printf '[]'"},
		{ID: "run", DependsOn: []string{"discover"}, WorkflowRun: &spec.WorkflowRunSpec{Path: "/tmp/child.yaml", FanOut: &spec.WorkflowFanOutSpec{ItemsFrom: "nodes.discover.output.items", MaxParallel: 4, Join: "all_done"}}},
	}}
	if err := Validate(wf); err != nil {
		t.Fatal(err)
	}
	wf.Nodes[1].DependsOn = nil
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "must be an upstream dependency") {
		t.Fatalf("expected upstream dependency error, got %v", err)
	}
	wf.Nodes[1].DependsOn = []string{"discover"}
	wf.Nodes[1].WorkflowRun.FanOut.ItemsFrom = "discover.items"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "items_from") {
		t.Fatalf("expected items_from path error, got %v", err)
	}
}

func TestValidateScriptAndTypedArtifactContracts(t *testing.T) {
	wf := &spec.Workflow{Name: "script", Nodes: []spec.Node{{
		ID: "run", Script: &spec.ScriptSpec{Runtime: "python", Inline: "print('ok')"}, OutputType: "result", OutputMIME: "text/plain",
	}}}
	if err := Validate(wf); err != nil {
		t.Fatal(err)
	}
	wf.Nodes[0].Script.Runtime = "ruby"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "runtime") {
		t.Fatalf("unsupported script runtime was accepted: %v", err)
	}
	wf.Nodes[0].Script.Runtime = "python"
	wf.Nodes[0].Script.Path = "script.py"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("path+inline script was accepted: %v", err)
	}
	wf.Nodes[0].Script.Path = ""
	wf.Nodes[0].OutputType = "bad/type"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "output_type") {
		t.Fatalf("invalid artifact type was accepted: %v", err)
	}
}

func TestValidateAllowsOutputFormatForScript(t *testing.T) {
	wf := &spec.Workflow{Name: "script-json", Nodes: []spec.Node{{
		ID: "run", Script: &spec.ScriptSpec{Runtime: "python", Inline: "print('{}')"}, OutputFormat: &spec.OutputFormat{Type: "object"},
	}}}
	if err := Validate(wf); err != nil {
		t.Fatal(err)
	}
}

func TestCommandDirsForDefinitionIncludesProfileAndTaktRoots(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".takt", "profiles", "code", "workflows", "blocks", "review.yaml")
	dirs := CommandDirsForDefinition(path)
	wantProfile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "commands")
	wantTakt := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path))))), "commands")
	contains := func(value string) bool {
		for _, dir := range dirs {
			if dir == value {
				return true
			}
		}
		return false
	}
	if !contains(wantProfile) || !contains(wantTakt) {
		t.Fatalf("command dirs %v do not include profile=%s and takt=%s", dirs, wantProfile, wantTakt)
	}
}

func TestValidateExternalSideEffectContract(t *testing.T) {
	valid := &spec.Workflow{Name: "side-effect", Nodes: []spec.Node{{ID: "publish", Prompt: "publish", Executor: "external", SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}}}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid reconcile side effect rejected: %v", err)
	}
	invalidMode := &spec.Workflow{Name: "side-effect", Nodes: []spec.Node{{ID: "publish", Prompt: "publish", Executor: "external", SideEffect: &spec.SideEffectSpec{Mode: "maybe"}}}}
	if err := Validate(invalidMode); err == nil || !strings.Contains(err.Error(), "side_effect.mode") {
		t.Fatalf("invalid mode error = %v", err)
	}
	local := &spec.Workflow{Name: "side-effect", Nodes: []spec.Node{{ID: "publish", Bash: "true", SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}}}}
	if err := Validate(local); err == nil || !strings.Contains(err.Error(), "executor: external") {
		t.Fatalf("local side effect error = %v", err)
	}
}

func TestValidateDomainAdapterNode(t *testing.T) {
	valid := &spec.Workflow{Name: "adapter", Nodes: []spec.Node{{
		ID: "publish", Adapter: &spec.AdapterCallSpec{Name: "scm", Operation: "change.create", Input: `{"title":"change"}`}, SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}, OutputFormat: &spec.OutputFormat{Type: "object"},
	}}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid adapter node rejected: %v", err)
	}
	valid.Nodes[0].Adapter.Operation = "Change.Create"
	if err := Validate(valid); err == nil || !strings.Contains(err.Error(), "adapter.operation") {
		t.Fatalf("invalid operation error = %v", err)
	}
	valid.Nodes[0].Adapter.Operation = "change.create"
	valid.Nodes[0].Bash = "true"
	if err := Validate(valid); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("multiple actions error = %v", err)
	}
}

func TestValidateRetryBackoffAndTimeoutRetryKind(t *testing.T) {
	wf := &spec.Workflow{Name: "backoff", Nodes: []spec.Node{{
		ID: "work", Bash: "true", Attempts: spec.AttemptsSpec{Max: 3, RetryOn: []string{"timed_out"}, Backoff: &spec.BackoffSpec{Initial: "100ms", Multiplier: 2, Max: "1s", Jitter: true}},
	}}}
	if err := Validate(wf); err != nil {
		t.Fatalf("valid backoff rejected: %v", err)
	}
	wf.Nodes[0].Attempts.Backoff.Initial = "0s"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "backoff.initial") {
		t.Fatalf("invalid initial backoff accepted: %v", err)
	}
	wf.Nodes[0].Attempts.Backoff.Initial = "2s"
	wf.Nodes[0].Attempts.Backoff.Max = "1s"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "backoff.max") {
		t.Fatalf("max below initial accepted: %v", err)
	}
	wf.Nodes[0].Attempts.Backoff.Initial = "100ms"
	wf.Nodes[0].Attempts.Backoff.Max = "1s"
	wf.Nodes[0].Attempts.Backoff.Multiplier = 0.5
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "backoff.multiplier") {
		t.Fatalf("invalid multiplier accepted: %v", err)
	}
}

func TestValidateOSSandboxEnforcementOnlyForDeterministicLocalNodes(t *testing.T) {
	for _, node := range []spec.Node{
		{ID: "bash", Bash: "true", Sandbox: &spec.SandboxSpec{Enforcement: "required", Network: "deny"}},
		{ID: "script", Script: &spec.ScriptSpec{Runtime: "command", Path: "tool.sh"}, Sandbox: &spec.SandboxSpec{Enforcement: "optional", Filesystem: "read_only"}},
	} {
		wf := &spec.Workflow{Name: "sandbox", Nodes: []spec.Node{node}}
		if err := Validate(wf); err != nil {
			t.Fatalf("valid sandbox node %s rejected: %v", node.ID, err)
		}
	}
	assistantNode := &spec.Workflow{Name: "sandbox", Nodes: []spec.Node{{ID: "agent", Prompt: "work", Sandbox: &spec.SandboxSpec{Enforcement: "required"}}}}
	if err := Validate(assistantNode); err == nil || !strings.Contains(err.Error(), "OS sandbox enforcement") {
		t.Fatalf("assistant OS enforcement should be rejected until host wrapping exists: %v", err)
	}
	invalid := &spec.Workflow{Name: "sandbox", Nodes: []spec.Node{{ID: "bash", Bash: "true", Sandbox: &spec.SandboxSpec{Enforcement: "maybe"}}}}
	if err := Validate(invalid); err == nil || !strings.Contains(err.Error(), "sandbox.enforcement") {
		t.Fatalf("invalid enforcement accepted: %v", err)
	}
}

func TestValidateRepositoryChildRunRules(t *testing.T) {
	valid := &spec.Workflow{Name: "repo-child", Nodes: []spec.Node{{ID: "api", WorkflowRun: &spec.WorkflowRunSpec{Path: "child.yaml", Repository: "repos/api", Isolation: "worktree"}}}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid repository child rejected: %v", err)
	}
	absolute := *valid
	absolute.Nodes = append([]spec.Node(nil), valid.Nodes...)
	copyRun := *valid.Nodes[0].WorkflowRun
	copyRun.Repository = filepath.Join(string(filepath.Separator), "tmp", "repo")
	absolute.Nodes[0].WorkflowRun = &copyRun
	if err := Validate(&absolute); err == nil || !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("absolute repository path accepted: %v", err)
	}
	inherit := *valid
	inherit.Nodes = append([]spec.Node(nil), valid.Nodes...)
	copyRun = *valid.Nodes[0].WorkflowRun
	copyRun.Isolation = "inherit"
	inherit.Nodes[0].WorkflowRun = &copyRun
	if err := Validate(&inherit); err == nil || !strings.Contains(err.Error(), "cannot use isolation inherit") {
		t.Fatalf("repository inherit accepted: %v", err)
	}
	fanout := *valid
	fanout.Nodes = append([]spec.Node(nil), valid.Nodes...)
	copyRun = *valid.Nodes[0].WorkflowRun
	copyRun.FanOut = &spec.WorkflowFanOutSpec{ItemsFrom: "nodes.source.output"}
	fanout.Nodes[0].WorkflowRun = &copyRun
	if err := Validate(&fanout); err == nil || !strings.Contains(err.Error(), "does not support fan_out") {
		t.Fatalf("repository fan_out accepted: %v", err)
	}
}

func TestLoadRejectsUnsupportedSchemaKeywordsForInputAndOutput(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "input-ref", want: "$ref", yaml: `name: bad-input
input:
  format: json
  schema:
    type: object
    $ref: '#/$defs/input'
nodes:
  - id: done
    bash: 'true'
`},
		{name: "output-oneof", want: "oneOf", yaml: `name: bad-output
nodes:
  - id: done
    bash: 'printf x'
    output_format:
      type: string
      oneOf:
        - type: string
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected fail-closed error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateRejectsWhenExpressionCreepAtLoadTime(t *testing.T) {
	cases := []string{
		`nodes.a.output > 3`,
		`(nodes.a.output == "ok")`,
		`contains(nodes.a.output, "ok") == true`,
		`nodes.a.output + 1 == 2`,
	}
	for _, when := range cases {
		t.Run(when, func(t *testing.T) {
			wf := &spec.Workflow{Name: "when-constitution", Nodes: []spec.Node{
				{ID: "a", Bash: "echo ok"},
				{ID: "b", Bash: "true", DependsOn: []string{"a"}, When: when},
			}}
			err := Validate(wf)
			if err == nil || !strings.Contains(err.Error(), "intentionally limited to ==, !=, && and ||") {
				t.Fatalf("expected workflow language constitution error for %q, got %v", when, err)
			}
		})
	}
}

func TestValidateAcceptsConstitutionalWhenGate(t *testing.T) {
	wf := &spec.Workflow{Name: "when-constitution", Nodes: []spec.Node{
		{ID: "classify-v2", Bash: "echo ready"},
		{ID: "b", Bash: "true", DependsOn: []string{"classify-v2"}, When: `$classify-v2.output == "ready" && $INPUTS.input != "dry-run"`},
	}}
	if err := Validate(wf); err != nil {
		t.Fatalf("small declarative when gate rejected: %v", err)
	}
}

func TestValidateHookFailureSession(t *testing.T) {
	hookLists := map[string]func(*spec.HookSet, spec.HookSpec){
		"before_node":     func(h *spec.HookSet, hook spec.HookSpec) { h.BeforeNode = []spec.HookSpec{hook} },
		"after_node":      func(h *spec.HookSet, hook spec.HookSpec) { h.AfterNode = []spec.HookSpec{hook} },
		"before_complete": func(h *spec.HookSet, hook spec.HookSpec) { h.BeforeComplete = []spec.HookSpec{hook} },
		"on_failure":      func(h *spec.HookSet, hook spec.HookSpec) { h.OnFailure = []spec.HookSpec{hook} },
	}
	cases := []struct {
		name       string
		action     string
		session    string
		want       string
		wantAccept bool
	}{
		{name: "retry fresh", action: "retry", session: "fresh", wantAccept: true},
		{name: "retry resume", action: "retry", session: "resume", wantAccept: true},
		{name: "retry unknown session", action: "retry", session: "reuse", want: "must be fresh or resume"},
		{name: "continue fresh", action: "continue", session: "fresh", want: "requires action retry"},
		{name: "fail resume", action: "fail", session: "resume", want: "requires action retry"},
	}
	for listName, setHooks := range hookLists {
		for _, tc := range cases {
			t.Run(listName+"/"+tc.name, func(t *testing.T) {
				hooks := spec.HookSet{}
				setHooks(&hooks, spec.HookSpec{Bash: "true", OnFailure: spec.HookDecision{Action: tc.action, Session: tc.session}})
				wf := &spec.Workflow{Name: "hook-session", Nodes: []spec.Node{{ID: "run", Bash: "true", Hooks: hooks}}}
				err := Validate(wf)
				if tc.wantAccept {
					if err != nil {
						t.Fatalf("valid hook session rejected: %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("expected error containing %q, got %v", tc.want, err)
				}
			})
		}
	}
}

func TestValidateRejectsInvalidWorkflowHookFailureSession(t *testing.T) {
	wf := &spec.Workflow{
		Name: "workflow-hook-session",
		Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{
			Bash:      "true",
			OnFailure: spec.HookDecision{Action: "retry", Session: "reuse"},
		}}},
		Nodes: []spec.Node{{ID: "run", Bash: "true"}},
	}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "must be fresh or resume") {
		t.Fatalf("invalid workflow hook session was accepted: %v", err)
	}
}

func TestLoadRejectsExplicitEmptyHookFailureSession(t *testing.T) {
	for _, session := range []string{"\"\"", "null"} {
		t.Run(session, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "workflow.yaml")
			source := fmt.Sprintf(`name: hook-session
nodes:
  - id: run
    bash: 'true'
    hooks:
      after_node:
        - bash: 'true'
          on_failure:
            action: continue
            session: %s
`, session)
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			wf, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "session") {
				var hasSession bool
				if wf != nil {
					hasSession = wf.Nodes[0].Hooks.AfterNode[0].OnFailure.HasSession()
				}
				t.Fatalf("explicit session %s was accepted: %v (has session: %v)", session, err, hasSession)
			}
		})
	}
}

func TestLoadAcceptsOmittedHookFailureSession(t *testing.T) {
	for _, action := range []string{"continue", "retry"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "workflow.yaml")
			source := fmt.Sprintf(`name: hook-session
nodes:
  - id: run
    bash: 'true'
    hooks:
      after_node:
        - bash: 'true'
          on_failure:
            action: %s
`, action)
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err != nil {
				t.Fatalf("omitted session with action %s rejected: %v", action, err)
			}
		})
	}
}
