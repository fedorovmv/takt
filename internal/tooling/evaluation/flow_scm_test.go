package evaluation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeGHFixtureContract(t *testing.T) {
	root := t.TempDir()
	bin, fixture, state := filepath.Join(root, "bin"), filepath.Join(root, "fixture"), filepath.Join(root, "state")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), FakeGHFixture(), 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"repo-view.json":  `{"nameWithOwner":"other/repo"}` + "\n",
		"issue-view.json": `{"number":9,"repository":{"nameWithOwner":"other/repo"}}` + "\n",
		"issue-number":    "9\n",
		"pr-view.json":    `{"number":7,"state":"OPEN","url":"https://example.test/other/repo/pull/7"}` + "\n",
		"pr-list.json":    `[{"number":7,"state":"OPEN"}]` + "\n",
		"pr-number":       "7\n",
		"pr-url-prefix":   "https://example.test/other/repo/pull/\n",
	} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "FAKE_GH_FIXTURE_DIR=" + fixture, "FAKE_GH_STATE_DIR=" + state}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(filepath.Join(bin, "gh"), args...)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, call := range [][]string{{"repo", "view", "--json", "nameWithOwner"}, {"issue", "view", "9", "--json", "number"}, {"pr", "view", "7", "--json", "number"}, {"pr", "list", "--json", "number"}} {
		got, err := run(call...)
		if err != nil || !strings.Contains(got, "other/repo") && call[0] != "pr" {
			t.Fatalf("gh %v = %q, %v", call, got, err)
		}
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"pr", "create", "--draft", "--title", "first title", "--body", "first body"}, "https://example.test/other/repo/pull/8\n"},
		{[]string{"pr", "create", "--title", "second title", "--body", "second body"}, "https://example.test/other/repo/pull/9\n"},
	} {
		got, err := run(test.args...)
		if err != nil || got != test.want {
			t.Fatalf("gh %v = %q, %v; want %q", test.args, got, err, test.want)
		}
	}
	if _, err := run("pr", "view", "1"); err == nil {
		t.Fatal("wrong PR number succeeded")
	}
	if _, err := run("api", "user"); err == nil {
		t.Fatal("unsupported command succeeded")
	}
	log, err := os.ReadFile(filepath.Join(state, "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "first\\ title") || !strings.Contains(string(log), "api user") {
		t.Fatalf("calls log=%q", log)
	}
}

func TestFakeGHFixtureRejectsUnsupportedArgv(t *testing.T) {
	root := t.TempDir()
	bin, fixture, state := filepath.Join(root, "bin"), filepath.Join(root, "fixture"), filepath.Join(root, "state")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), FakeGHFixture(), 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"repo-view.json": "{}\n", "issue-view.json": "{}\n", "issue-number": "1\n", "pr-view.json": "{}\n", "pr-list.json": "[]\n", "pr-number": "1\n", "pr-url-prefix": "https://example.test/acme/repo/pull/\n",
	} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"issue", "view", "1", "--repo", "acme/repo"},
		{"repo", "view", "--json"},
		{"pr", "view", "1", "--json", "number", "--extra"},
		{"pr", "list", "--state", "open"},
		{"pr", "create", "--title", "x"},
		{"pr", "create", "--title", "x", "--body", "y", "--repo", "acme/repo"},
	} {
		cmd := exec.Command(filepath.Join(bin, "gh"), args...)
		cmd.Env = append(os.Environ(), "FAKE_GH_FIXTURE_DIR="+fixture, "FAKE_GH_STATE_DIR="+state)
		if err := cmd.Run(); err == nil {
			t.Fatalf("gh %v succeeded", args)
		}
	}
}

func TestFakeGHFixtureIsolatesState(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	if err := os.MkdirAll(fixture, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"repo-view.json": "{}\n", "pr-list.json": "[]\n", "pr-url-prefix": "https://example.test/acme/repo/pull/\n"} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), FakeGHFixture(), 0755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{filepath.Join(root, "one"), filepath.Join(root, "two")} {
		cmd := exec.Command(filepath.Join(bin, "gh"), "pr", "create", "--title", "x", "--body", "y")
		cmd.Env = append(os.Environ(), "FAKE_GH_FIXTURE_DIR="+fixture, "FAKE_GH_STATE_DIR="+state)
		out, err := cmd.Output()
		if err != nil || string(out) != "https://example.test/acme/repo/pull/1\n" {
			t.Fatalf("state %q: %q %v", state, out, err)
		}
	}
}

func TestInstalledFakeGHIgnoresFixtureEnvironmentOverrides(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, ".takt", "eval", "bin")
	fixture := filepath.Join(root, ".takt", "eval", "scm-fixture")
	state := filepath.Join(root, ".takt", "evals", "scm")
	otherFixture := filepath.Join(root, "other-fixture")
	otherState := filepath.Join(root, "other-state")
	for _, dir := range []string{bin, fixture, otherFixture} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), FakeGHFixture(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "pr-url-prefix"), []byte("https://example.test/canonical/pull/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherFixture, "pr-url-prefix"), []byte("https://example.test/redirected/pull/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(bin, "gh"), "pr", "create", "--title", "x", "--body", "y")
	cmd.Env = append(os.Environ(), "FAKE_GH_FIXTURE_DIR="+otherFixture, "FAKE_GH_STATE_DIR="+otherState)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "https://example.test/canonical/pull/1\n"; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(state, "calls.log")); err != nil {
		t.Fatalf("canonical calls log: %v", err)
	}
	if _, err := os.Stat(filepath.Join(otherState, "calls.log")); !os.IsNotExist(err) {
		t.Fatalf("override state was used: %v", err)
	}
}

func TestFakeGHUsesExecutionWorkspaceWhenInvokedFromControlCopy(t *testing.T) {
	root := t.TempDir()
	controlBin := filepath.Join(root, "control", ".takt", "eval", "bin")
	execution := filepath.Join(root, "execution")
	fixture := filepath.Join(execution, ".takt", "eval", "scm-fixture")
	state := filepath.Join(execution, ".takt", "evals", "scm")
	otherFixture := filepath.Join(root, "other-fixture")
	otherState := filepath.Join(root, "other-state")
	for _, dir := range []string{controlBin, fixture, otherFixture} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(controlBin, "gh"), FakeGHFixture(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "pr-url-prefix"), []byte("https://example.test/execution/pull/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherFixture, "pr-url-prefix"), []byte("https://example.test/redirected/pull/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(controlBin, "gh"), "pr", "create", "--title", "x", "--body", "y")
	cmd.Env = append(os.Environ(), "TAKT_WORKSPACE="+execution, "FAKE_GH_FIXTURE_DIR="+otherFixture, "FAKE_GH_STATE_DIR="+otherState)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "https://example.test/execution/pull/1\n"; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(state, "calls.log")); err != nil {
		t.Fatalf("execution calls log: %v", err)
	}
	if _, err := os.Stat(filepath.Join(otherState, "calls.log")); !os.IsNotExist(err) {
		t.Fatalf("override state was used: %v", err)
	}
}

func TestLoadFlowSCMFixtureStrictContract(t *testing.T) {
	for _, test := range []struct {
		name, require, repository, pullRequest, patch string
		wantErr                                       string
	}{
		{"repository", "repository", "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n", "", "", ""},
		{"pull request", "pull_request", "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n", "number: 7\ntitle: x\nbase: main\nhead: feature/x\nstate: OPEN\nci_status: passed\nfixes_permitted: true\n", "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n", ""},
		{"repository rejects pr files missing only when required", "pull_request", "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n", "", "", "pull-request.yaml"},
		{"unknown repository field", "repository", "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\nextra: no\n", "", "", "unknown field"},
		{"invalid repository", "repository", "repository: acme/repo/extra\nbase_branch: main\nhead_branch: feature/x\n", "", "", "repository"},
		{"invalid ref", "repository", "repository: acme/repo\nbase_branch: main\nhead_branch: ../x\n", "", "", "head_branch"},
		{"patch rejects binary marker", "pull_request", "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n", "number: 7\ntitle: x\nbase: main\nhead: feature/x\nstate: OPEN\nci_status: passed\nfixes_permitted: true\n", "GIT binary patch\n", "head.patch"},
		{"patch rejects escaping path", "pull_request", "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n", "number: 7\ntitle: x\nbase: main\nhead: feature/x\nstate: OPEN\nci_status: passed\nfixes_permitted: true\n", "diff --git a/../x b/../x\n", "head.patch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeFlowSCMFixture(t, test.repository, test.pullRequest, test.patch)
			fixture, err := LoadFlowSCMFixture(root, test.require)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Repository.Repository != "acme/repo" || (test.require == "pull_request" && fixture.PullRequest == nil) {
				t.Fatalf("fixture = %+v", fixture)
			}
		})
	}
}

func TestPrepareFlowRepeatBuildsSCMHistoryAndLocalRemote(t *testing.T) {
	suite, item := prepareFlowFixture(t, "code:feature-development", flowConfig("implementation", "review"), "# task\n")
	suite.External.GitHub = &FlowGitHubSpec{Mode: "fixture", Require: "pull_request"}
	item.SCMPath = filepath.Join(item.Root, "scm")
	if err := os.MkdirAll(item.SCMPath, 0755); err != nil {
		t.Fatal(err)
	}
	writeFlowSCMFiles(t, item.SCMPath,
		"repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n",
		"number: 7\ntitle: x\nbase: main\nhead: feature/x\nstate: OPEN\nci_status: passed\nfixes_permitted: true\n",
		"diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-workspace\n+patched\n")
	prepared, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.BaseCommit == "" || prepared.HeadCommit == "" || prepared.BaseCommit == prepared.HeadCommit {
		t.Fatalf("commits = base %q head %q", prepared.BaseCommit, prepared.HeadCommit)
	}
	if branch := strings.TrimSpace(gitOutput(t, prepared.ControlWorkspace, "branch", "--show-current")); branch != "feature/x" {
		t.Fatalf("branch = %q", branch)
	}
	requireGitClean(t, prepared.ControlWorkspace)
	if _, err := os.Stat(filepath.Join(prepared.BareRemote, "HEAD")); err != nil {
		t.Fatalf("bare remote = %q: %v", prepared.BareRemote, err)
	}
	if got := gitOutput(t, prepared.BareRemote, "show-ref", "refs/heads/main", "refs/heads/feature/x"); !strings.Contains(got, prepared.BaseCommit) || !strings.Contains(got, prepared.HeadCommit) {
		t.Fatalf("remote refs = %q", got)
	}
	requireTreeEqualWithoutGit(t, prepared.ControlWorkspace, prepared.BaselineWorkspace)
}

func TestPrepareFlowRepeatRejectsUnapplicableHeadPatch(t *testing.T) {
	suite, item := prepareFlowFixture(t, "code:feature-development", flowConfig("implementation", "review"), "# task\n")
	suite.External.GitHub = &FlowGitHubSpec{Mode: "fixture", Require: "pull_request"}
	item.SCMPath = filepath.Join(item.Root, "scm")
	if err := os.MkdirAll(item.SCMPath, 0755); err != nil {
		t.Fatal(err)
	}
	writeFlowSCMFiles(t, item.SCMPath,
		"repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n",
		"number: 7\ntitle: x\nbase: main\nhead: feature/x\nstate: OPEN\nci_status: passed\nfixes_permitted: true\n",
		"diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-missing\n+patched\n")
	if _, err := PrepareFlowRepeat(context.Background(), suite, item, 1, t.TempDir(), "/host/bin"); err == nil || !strings.Contains(err.Error(), "apply") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareFlowRepeatUsesRepeatLocalRemote(t *testing.T) {
	suite, item := prepareFlowFixture(t, "code:feature-development", flowConfig("implementation", "review"), "# task\n")
	suite.External.GitHub = &FlowGitHubSpec{Mode: "fixture", Require: "repository"}
	item.SCMPath = filepath.Join(item.Root, "scm")
	if err := os.MkdirAll(item.SCMPath, 0755); err != nil {
		t.Fatal(err)
	}
	writeFlowSCMFiles(t, item.SCMPath, "repository: acme/repo\nbase_branch: main\nhead_branch: feature/x\n", "", "")
	evidence := t.TempDir()
	first, err := PrepareFlowRepeat(context.Background(), suite, item, 1, evidence, "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareFlowRepeat(context.Background(), suite, item, 2, evidence, "/host/bin")
	if err != nil {
		t.Fatal(err)
	}
	if first.BareRemote == second.BareRemote || !strings.HasSuffix(first.BareRemote, "repeat-001/origin.git") || !strings.HasSuffix(second.BareRemote, "repeat-002/origin.git") {
		t.Fatalf("remotes = %q, %q", first.BareRemote, second.BareRemote)
	}
}

func TestPreparedFlowIdentityIncludesCommitsButNotRemotePath(t *testing.T) {
	identity := flowPreparedIdentity(&PreparedFlowRepeat{BaseCommit: "base", HeadCommit: "head", BareRemote: "/private/repeat-001/origin.git"})
	if identity == "" || identity == flowPreparedIdentity(&PreparedFlowRepeat{BaseCommit: "base", HeadCommit: "other", BareRemote: "/private/repeat-001/origin.git"}) {
		t.Fatalf("prepared identity does not include SCM state: %q", identity)
	}
	if other := flowPreparedIdentity(&PreparedFlowRepeat{BaseCommit: "base", HeadCommit: "head", BareRemote: "/private/repeat-002/origin.git"}); identity != other {
		t.Fatalf("remote output path changed identity: %q != %q", identity, other)
	}
}

func writeFlowSCMFixture(t *testing.T, repository, pullRequest, patch string) string {
	t.Helper()
	root := t.TempDir()
	scm := filepath.Join(root, "scm")
	if err := os.MkdirAll(scm, 0755); err != nil {
		t.Fatal(err)
	}
	writeFlowSCMFiles(t, scm, repository, pullRequest, patch)
	return root
}

func writeFlowSCMFiles(t *testing.T, scm, repository, pullRequest, patch string) {
	t.Helper()
	if repository != "" {
		if err := os.WriteFile(filepath.Join(scm, "repository.yaml"), []byte(repository), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if pullRequest != "" {
		if err := os.WriteFile(filepath.Join(scm, "pull-request.yaml"), []byte(pullRequest), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if patch != "" {
		if err := os.WriteFile(filepath.Join(scm, "head.patch"), []byte(patch), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
