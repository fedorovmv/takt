package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareFlowRepeatCommitsProfileBeforeBaseline(t *testing.T) {
	suite, item := prepareFlowFixture(t, "code:feature-development", flowConfig("implementation", "review"), "# task\n")
	prepared, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	requireGitClean(t, prepared.ControlWorkspace)
	if branch := strings.TrimSpace(gitOutput(t, prepared.ControlWorkspace, "branch", "--show-current")); branch != "main" {
		t.Fatalf("branch = %q", branch)
	}
	if dates := gitOutput(t, prepared.ControlWorkspace, "show", "-s", "--format=%aI%n%cI", "HEAD"); dates != "2000-01-01T00:00:00+00:00\n2000-01-01T00:00:00+00:00\n" {
		t.Fatalf("commit dates = %q", dates)
	}
	requireGitTracked(t, prepared.ControlWorkspace, ".takt/profiles/code/workflows/feature-development.yaml")
	requireGitTracked(t, prepared.ControlWorkspace, ".takt/eval/input.md")
	requireFileContains(t, filepath.Join(prepared.ControlWorkspace, ".takt", "config.yaml"), "fake-implementation")
	if prepared.InputValue != filepath.Join(prepared.ControlWorkspace, ".takt", "eval", "input.md") {
		t.Fatalf("input value = %q", prepared.InputValue)
	}
	if prepared.HostPATHHash != sha256Hex("/host/bin") || prepared.ProfileFingerprint == "" {
		t.Fatalf("unexpected fingerprints: %+v", prepared)
	}
	requireTreeEqualWithoutGit(t, prepared.ControlWorkspace, prepared.BaselineWorkspace)
}

func TestPrepareFlowRepeatUsesCleanIndependentRepeat(t *testing.T) {
	suite, item := prepareFlowFixture(t, "code:feature-development", flowConfig("implementation", "review"), "# task\n")
	evidence := t.TempDir()
	first, err := PrepareFlowRepeat(context.Background(), suite, item, 1, evidence, "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.ControlWorkspace, "repeat-one.txt"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareFlowRepeat(context.Background(), suite, item, 2, evidence, "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second.ControlWorkspace, "repeat-one.txt")); !os.IsNotExist(err) {
		t.Fatalf("repeat two inherited repeat one change: %v", err)
	}
}

func TestPrepareFlowRepeatDoesNotInitializeExternalProfile(t *testing.T) {
	suite, item := prepareFlowFixture(t, "external:workflow", flowConfig(), "# task\n")
	if _, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin"); err == nil || !strings.Contains(err.Error(), "external") {
		t.Fatalf("expected missing external profile error, got %v", err)
	}
}

func TestPrepareFlowRepeatKeepsWorkflowPathExternalToProfiles(t *testing.T) {
	suite, item := prepareFlowFixture(t, "ignored", flowConfig(), "# task\n")
	workflow := filepath.Join(suite.SuiteDir, "workflow.yaml")
	if err := os.WriteFile(workflow, []byte("name: local\nnodes: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	suite.Workflow, suite.ResolvedWorkflow = "workflow.yaml", workflow
	prepared, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prepared.ControlWorkspace, ".takt", "profiles")); !os.IsNotExist(err) {
		t.Fatalf("workflow path initialized a profile: %v", err)
	}
}

func TestPrepareFlowRepeatModelSlots(t *testing.T) {
	for _, test := range []struct {
		name, selector string
		models         []string
		wantSlot       string
	}{
		{"feature does not require routing", "code:feature-development", []string{"implementation", "review"}, ""},
		{"feature requires implementation", "code:feature-development", []string{"review"}, "implementation"},
		{"feature requires review", "code:feature-development", []string{"implementation"}, "review"},
		{"comprehensive does not require routing", "code:comprehensive-pr-review", []string{"review"}, ""},
		{"comprehensive requires review", "code:comprehensive-pr-review", nil, "review"},
		{"architect requires implementation", "code:architect", []string{"review", "routing"}, "implementation"},
		{"architect requires review", "code:architect", []string{"implementation", "routing"}, "review"},
		{"architect requires routing", "code:architect", []string{"implementation", "review"}, "routing"},
		{"architect all slots", "code:architect", []string{"implementation", "review", "routing"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := "# task\n"
			if test.selector != "code:feature-development" {
				input = `{"repository":"acme/repo","pull_request":1,"fixes_permitted":false,"validation_commands":["go test ./..."]}`
			}
			suite, item := prepareFlowFixture(t, test.selector, flowConfig(test.models...), input)
			prepared, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin")
			if test.wantSlot != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantSlot) {
					t.Fatalf("expected %s error, got %v", test.wantSlot, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.selector != "code:feature-development" && prepared.InputValue != input {
				t.Fatalf("JSON input = %q", prepared.InputValue)
			}
		})
	}
}

func TestPrepareFlowRepeatRejectsNullFixesPermitted(t *testing.T) {
	for _, selector := range []string{"code:comprehensive-pr-review", "code:architect"} {
		t.Run(selector, func(t *testing.T) {
			input := `{"repository":"acme/repo","pull_request":1,"fixes_permitted":null,"validation_commands":["go test ./..."]}`
			models := []string{"review"}
			if selector == "code:architect" {
				models = []string{"implementation", "review", "routing"}
			}
			suite, item := prepareFlowFixture(t, selector, flowConfig(models...), input)
			if _, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin"); err == nil || !strings.Contains(err.Error(), "fixes_permitted") {
				t.Fatalf("expected fixes_permitted error, got %v", err)
			}
		})
	}
}

func TestPrepareFlowRepeatAcceptsEmptyRepository(t *testing.T) {
	input := `{"repository":"","pull_request":1,"fixes_permitted":false,"validation_commands":["go test ./..."]}`
	suite, item := prepareFlowFixture(t, "code:comprehensive-pr-review", flowConfig("review"), input)
	if _, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin"); err != nil {
		t.Fatalf("empty repository should remain a string: %v", err)
	}
}

func prepareFlowFixture(t *testing.T, workflow, configText, input string) (*FlowSuite, FlowCase) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configText), 0644); err != nil {
		t.Fatal(err)
	}
	caseRoot := filepath.Join(root, "cases", "case-a")
	if err := os.MkdirAll(filepath.Join(caseRoot, "workspace"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseRoot, "input.md"), []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseRoot, "expected.yaml"), []byte("oracle: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseRoot, "workspace", "README.md"), []byte("workspace\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return &FlowSuite{Workflow: workflow, ResolvedConfig: configPath, SuiteDir: root}, FlowCase{ID: "case-a", Root: caseRoot, InputPath: filepath.Join(caseRoot, "input.md"), WorkspacePath: filepath.Join(caseRoot, "workspace")}
}

func flowConfig(models ...string) string {
	var b strings.Builder
	b.WriteString("apiVersion: takt/v1alpha1\nkind: Config\ndefault_assistant: fake\nassistants:\n  fake:\n    type: mock\nmodels:\n")
	for _, model := range models {
		b.WriteString("  " + model + ":\n    provider: fake\n    id: fake-" + model + "\n")
	}
	return b.String()
}

func requireGitClean(t *testing.T, dir string) {
	t.Helper()
	if out := gitOutput(t, dir, "status", "--porcelain"); out != "" {
		t.Fatalf("git status = %q", out)
	}
}

func requireGitTracked(t *testing.T, dir, path string) {
	t.Helper()
	if out := gitOutput(t, dir, "ls-files", "--error-unmatch", path); out != path+"\n" {
		t.Fatalf("git did not track %q: %q", path, out)
	}
}

func requireFileContains(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), want) {
		t.Fatalf("%s does not contain %q: %v", path, want, err)
	}
}

func requireTreeEqualWithoutGit(t *testing.T, control, baseline string) {
	t.Helper()
	controlHash, err := hashPathWithoutGit(control)
	if err != nil {
		t.Fatal(err)
	}
	baselineHash, err := hashPath(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if controlHash != baselineHash {
		t.Fatalf("control and baseline differ: %s != %s", controlHash, baselineHash)
	}
}

func hashPathWithoutGit(root string) (string, error) {
	tmp := filepath.Join(filepath.Dir(root), "snapshot")
	if err := archiveFlowGit(context.Background(), root, tmp); err != nil {
		return "", err
	}
	return hashPath(tmp)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, b)
	}
	return string(b)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
