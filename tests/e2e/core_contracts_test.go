package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatibilityContract(t *testing.T) {
	dir := t.TempDir()
	takt(t, nil, "compatibility", "matrix", "--json").RequireSuccess(t).Contains(t, "takt-compatibility/v1")
	takt(t, nil, "compatibility", "fields", "--json").RequireSuccess(t).Contains(t, `"field": "output_format"`).Contains(t, `"field": "native_hooks"`).Contains(t, `"decision": "defer"`)
	takt(t, nil, "compatibility", "schema", "--json").RequireSuccess(t).Contains(t, "takt-schema-subset/v1")
	current := writeFile(t, dir, "current.yaml", `apiVersion: takt/v1alpha1
kind: Config
assistants:
  coding:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [fake]
    capabilities: [tool_control]
`)
	takt(t, nil, "compatibility", "check", "--workspace", dir, "--config", current, "--strict", "--json").RequireSuccess(t).Contains(t, `"status": "ready"`)
	legacy := writeFile(t, dir, "legacy.yaml", `apiVersion: takt/v1alpha1
kind: Config
assistants:
  legacy:
    type: process
    protocol: takt-assistant/v1alpha1
    argv: [fake]
`)
	takt(t, nil, "compatibility", "check", "--workspace", dir, "--config", legacy, "--strict", "--json").RequireFailure(t).Contains(t, "deprecated")
}

func TestCompositionContract(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join(repoRoot, "examples", "composition"), dir)
	takt(t, nil, "validate", filepath.Join(dir, "workflow.yaml"), "--config", filepath.Join(dir, "config.yaml"), "--workspace", dir, "--json").RequireSuccess(t)
	out := takt(t, nil, "run", filepath.Join(dir, "workflow.yaml"), "--config", filepath.Join(dir, "config.yaml"), "--workspace", dir, "--json").RequireSuccess(t)
	out.Contains(t, `"status": "completed"`).Contains(t, `"prepare"`).Contains(t, `"batch"`).Contains(t, `\"second\",\"third\"`)
	if strings.Contains(out.Stdout, "prepare__") || strings.Contains(out.Stdout, "batch__") {
		t.Fatal("public Run state exposes expanded node IDs")
	}
	data, err := os.ReadFile(filepath.Join(dir, "order.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\nthird\n" {
		t.Fatalf("unexpected order: %q", data)
	}
}

func TestWorktreeContract(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.email", "takt@example.invalid")
	git(t, repo, "config", "user.name", "Takt Contract")
	wf := writeFile(t, repo, "workflow.yaml", `name: isolated-contract
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: change
    bash: printf 'isolated\n' > generated.txt
`)
	cfg := writeFile(t, repo, "config.yaml", "apiVersion: takt/v1alpha1\nkind: Config\n")
	git(t, repo, "add", "workflow.yaml", "config.yaml")
	git(t, repo, "commit", "-q", "-m", "definitions")
	env := resultObject(t, takt(t, nil, "run", wf, "--config", cfg, "--workspace", repo, "--json").RequireSuccess(t).JSON(t))
	runID := stringField(t, env, "id")
	wt := env["worktree"].(map[string]any)
	path := stringField(t, wt, "path")
	branch := stringField(t, wt, "branch")
	if stringField(t, wt, "retained_reason") != "uncommitted_changes" {
		t.Fatalf("unexpected worktree: %#v", wt)
	}
	if _, err := os.Stat(filepath.Join(repo, "generated.txt")); !os.IsNotExist(err) {
		t.Fatalf("main workspace mutated: %v", err)
	}
	requireFileContains(t, filepath.Join(path, "generated.txt"), "isolated")
	if !strings.Contains(git(t, repo, "branch", "--list", branch), branch) {
		t.Fatal("managed branch missing")
	}
	takt(t, nil, "worktree", "list", "--workspace", repo, "--json").RequireSuccess(t).Contains(t, runID)
	takt(t, nil, "worktree", "remove", runID, "--workspace", repo, "--force", "--json").RequireSuccess(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
	takt(t, nil, "worktree", "prune", "--workspace", repo, "--json").RequireSuccess(t)
}

func TestBlockPackageContract(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	takt(t, nil, "init", "code", "--dir", project, "--json").RequireSuccess(t)
	builtin := filepath.Join(project, ".takt", "profiles", "code", "workflows", "blocks", "package.yaml")
	takt(t, nil, "block", "validate", builtin, "--json").RequireSuccess(t)
	list := resultObject(t, takt(t, nil, "block", "list", "--profile", "code", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	blocks := list["blocks"].([]any)
	if len(blocks) != 11 {
		t.Fatalf("blocks=%d", len(blocks))
	}
	if stringField(t, list, "fingerprint") == "" {
		t.Fatal("empty catalog fingerprint")
	}
	desc := resultObject(t, takt(t, nil, "block", "describe", "adversarial-verify", "--profile", "code", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if !strings.HasSuffix(stringField(t, desc, "workflow_path"), "dynamic-adversarial-verify.yaml") {
		t.Fatalf("unexpected block: %#v", desc)
	}
	corp := resultObject(t, takt(t, nil, "block", "validate", filepath.Join(repoRoot, "examples", "corporate-block-package", "package.yaml"), "--json").RequireSuccess(t).JSON(t))
	governance := corp["governance"].(map[string]any)
	req := governance["required_blocks"].([]any)
	if len(req) != 1 || req[0] != "corp-validate" {
		t.Fatalf("governance=%#v", governance)
	}
	packageDir := filepath.Join(project, ".takt", "packages", "corporate-engineering")
	copyTree(t, filepath.Join(repoRoot, "examples", "corporate-block-package"), packageDir)
	profilePath := filepath.Join(project, ".takt", "profiles", "code", "profile.yaml")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "block_packages:\n  - workflows/blocks/package.yaml\n", "block_packages:\n  - workflows/blocks/package.yaml\n  - ../../packages/corporate-engineering/package.yaml\n", 1)
	if err := os.WriteFile(profilePath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	merged := resultObject(t, takt(t, nil, "block", "list", "--profile", "code", "--workspace", project, "--json").RequireSuccess(t).JSON(t))
	if len(merged["blocks"].([]any)) != 14 {
		t.Fatalf("merged catalog=%#v", merged)
	}
}

func TestTaktSkillContract(t *testing.T) {
	skill := filepath.Join(repoRoot, "skills", "takt", "SKILL.md")
	requireFileContains(t, skill, "name: takt", "takt validate", "references/patterns.md", "opencode")
	skillVersionBytes, _ := os.ReadFile(filepath.Join(repoRoot, "skills", "takt", "VERSION"))
	taktVersionBytes, _ := os.ReadFile(filepath.Join(repoRoot, "VERSION"))
	requireFileContains(t, filepath.Join(repoRoot, "skills", "takt", "README.md"), "Версия скилла — `"+strings.TrimSpace(string(skillVersionBytes))+"`.", "Takt `v"+strings.TrimSpace(string(taktVersionBytes))+"`")
	profile := filepath.Join(repoRoot, "skills", "takt", "assets", "validated-agent-profile")
	for _, name := range []string{"basic.yaml", "validated.yaml", "opencode.yaml", "composition.yaml"} {
		takt(t, nil, "validate", filepath.Join(profile, ".takt", "workflows", name), "--config", filepath.Join(profile, ".takt", "config.yaml"), "--workspace", profile, "--json").RequireSuccess(t)
	}
}

func TestCodeProfileCatalogContract(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, project, "PLAN.md", "# Development plan\n\n- [ ] Implement the requested change.\n- [ ] Run project validation.\n")
	takt(t, nil, "init", "code", "--dir", project, "--json").RequireSuccess(t)
	takt(t, nil, "validate", "code", "--workspace", project, "--json").RequireSuccess(t)
	base := filepath.Join(project, ".takt", "profiles", "code")
	version, err := os.ReadFile(filepath.Join(base, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "0.19.2" {
		t.Fatalf("profile version=%q", version)
	}
	requireFileContains(t, filepath.Join(base, "profile.yaml"), "router:", "block_packages:", "format: markdown", "preserve_path: true")
	requireFileContains(t, filepath.Join(base, "workflow.yaml"), "name: code-router", "allowed_tools: []", "output_format:", "workflow:", "retry_on:", "protocol")
	requireFileContains(t, filepath.Join(base, "workflows", "plan-to-pr.yaml"), "allowed_paths:", "scope-check", "PR_RESULT_ACCEPTED", "WORKFLOW_ACCEPTED")
	requireFileContains(t, filepath.Join(base, "commands", "route-workflow.md"), "Never infer `allowed_paths`", "otherwise select `assist`")
	requireFileContains(t, filepath.Join(base, "README.md"), "`allowed_paths`", "WORKFLOW_ACCEPTED")
	featureWorkflowPath := filepath.Join(base, "workflows", "feature-development.yaml")
	requireFileContains(t, featureWorkflowPath,
		"- id: initial-verdict", "- id: repair", "- id: revalidate-agent",
		"- id: revalidation-verdict", "when: $initial-verdict.output == \"REPAIR\"",
		"require-verdict", "require-artifacts", "- id: pr-effect-gate", "require-pr", "allow_failure: true")
	featureWorkflow, err := os.ReadFile(featureWorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"repair", "revalidate-agent", "revalidation-verdict", "pr-effect-gate"} {
		if got := strings.Count(string(featureWorkflow), "- id: "+id); got != 1 {
			t.Fatalf("feature workflow node %s count=%d", id, got)
		}
	}
	workflows := []string{"assist", "fix-github-issue", "create-issue", "issue-review-full", "piv-loop", "idea-to-pr", "plan-to-pr", "feature-development", "adversarial-dev", "smart-pr-review", "comprehensive-pr-review", "validate-pr", "architect", "refactor-safely", "interactive-prd", "ralph-dag", "workflow-builder", "remotion-generate", "resolve-conflicts"}
	for _, name := range workflows {
		takt(t, nil, "validate", "code:"+name, "--workspace", project, "--json").RequireSuccess(t)
	}
	list := takt(t, nil, "workflow", "list", "code", "--workspace", project, "--json").RequireSuccess(t)
	for _, name := range workflows {
		list.Contains(t, "code:"+name)
	}
	takt(t, nil, "workflow", "describe", "code:piv-loop", "--workspace", project, "--json").RequireSuccess(t).Contains(t, "Plan-Implement-Validate")
}

func TestLearningLoopContract(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work")
	for _, id := range []string{"run-learning-a", "run-learning-b"} {
		state := map[string]any{"id": id, "status": "failed", "workflow_path": "workflow.yaml", "config_path": "config.yaml", "workspace": work, "nodes": map[string]any{"validate": map[string]any{"status": "failed", "diagnostic": map[string]any{"code": "VALIDATION", "kind": "quality", "message": "same durable validation failure", "fingerprint": "sha256:learning-repeat"}}}, "approvals": map[string]any{}, "revision": 0}
		b, _ := json.MarshalIndent(state, "", "  ")
		writeFile(t, work, filepath.Join(".takt", "runs", id, "state.json"), string(b))
	}
	takt(t, nil, "learn", "scan", "--workspace", work, "--min-runs", "2", "--json").RequireSuccess(t).Contains(t, "sha256:learning-repeat")
	proposal := resultObject(t, takt(t, nil, "learn", "propose", "--workspace", work, "--pattern", "diagnostic:sha256:learning-repeat", "--kind", "skill", "--name", "repeated-validation", "--benefit", "prevent recurrence of the observed validation failure", "--json").RequireSuccess(t).JSON(t))
	id := stringField(t, proposal, "id")
	if stringField(t, proposal, "status") != "pending_review" {
		t.Fatalf("proposal=%#v", proposal)
	}
	takt(t, nil, "learn", "stage", id, "--workspace", work, "--json").RequireFailure(t)
	takt(t, nil, "learn", "review", id, "--workspace", work, "--decision", "accept", "--reason", "candidate is reusable", "--json").RequireSuccess(t)
	takt(t, nil, "learn", "stage", id, "--workspace", work, "--json").RequireFailure(t)
	report := writeFile(t, work, "evaluation.json", `{"report_version":"takt-evaluation-matrix/v1alpha1","matrix_fingerprint":"sha256:learning-matrix-fixture","benchmark_id":"learning-regression","passed":true,"gates":[{"strategy":"candidate","passed":true,"message":"no regression"}]}`)
	takt(t, nil, "learn", "evaluate", id, "--workspace", work, "--report", report, "--json").RequireSuccess(t)
	staged := resultObject(t, takt(t, nil, "learn", "stage", id, "--workspace", work, "--json").RequireSuccess(t).JSON(t))
	if stringField(t, staged, "status") != "ready" {
		t.Fatalf("stage=%#v", staged)
	}
	ready := filepath.Join(work, ".takt", "learning", "ready", id, "SKILL.md")
	requireFileContains(t, ready, "diagnostic:sha256:learning-repeat")
	if _, err := os.Stat(filepath.Join(work, ".takt", "packages", "repeated-validation")); !os.IsNotExist(err) {
		t.Fatalf("learning loop installed trusted package: %v", err)
	}
}

func TestAdapterPlatformContract(t *testing.T) {
	tmp := t.TempDir()
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(repoRoot, "examples", "adapter-platform", "config.yaml"), filepath.Join(work, "config.yaml"), 0o644)
	copyFile(t, filepath.Join(repoRoot, "examples", "adapter-platform", "workflow.yaml"), filepath.Join(work, "workflow.yaml"), 0o644)
	fake := binary(t, "takt-fake-domain-adapter")
	env := []string{"PATH=" + filepath.Dir(fake) + string(os.PathListSeparator) + os.Getenv("PATH"), "TAKT_FAKE_ADAPTER_STATE=" + filepath.Join(tmp, "adapter-state")}
	takt(t, env, "adapter", "doctor", "tracker", "--workspace", work, "--config", "config.yaml", "--json").RequireSuccess(t).Contains(t, "item.get")
	takt(t, env, "adapter", "doctor", "scm", "--workspace", work, "--config", "config.yaml", "--json").RequireSuccess(t).Contains(t, "change.create")
	original, err := os.ReadFile(filepath.Join(work, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(original), "      change.review: scm.change.review.reconcile", "      change.review: scm.change.review.reconcile\n      repository.get: scm.repository.get.reconcile", 1)
	writeFile(t, work, "bad-config.yaml", bad)
	takt(t, env, "adapter", "doctor", "scm", "--workspace", work, "--config", "bad-config.yaml", "--json").RequireFailure(t).Contains(t, `"status": "error"`).Contains(t, "configured reconcile operation not declared: repository.get")
	takt(t, env, "validate", filepath.Join(work, "workflow.yaml"), "--config", filepath.Join(work, "config.yaml"), "--workspace", work).RequireSuccess(t)
	takt(t, env, "run", filepath.Join(work, "workflow.yaml"), "--config", filepath.Join(work, "config.yaml"), "--workspace", work, "--json").RequireSuccess(t).Contains(t, `"status": "completed"`).Contains(t, "domain_operation").Contains(t, `"reconcile_status": "applied"`)
}
