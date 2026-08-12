package evaluation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
