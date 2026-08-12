package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"takt/internal/yamlcodec"
)

type FlowSCMFixture struct {
	Repository  FlowSCMRepository
	PullRequest *FlowSCMPullRequest
	HeadPatch   string
}

type FlowSCMRepository struct {
	Repository string `json:"repository" yaml:"repository"`
	BaseBranch string `json:"base_branch" yaml:"base_branch"`
	HeadBranch string `json:"head_branch" yaml:"head_branch"`
}

type FlowSCMPullRequest struct {
	Number         int    `json:"number" yaml:"number"`
	Title          string `json:"title" yaml:"title"`
	Base           string `json:"base" yaml:"base"`
	Head           string `json:"head" yaml:"head"`
	State          string `json:"state" yaml:"state"`
	CIStatus       string `json:"ci_status" yaml:"ci_status"`
	FixesPermitted bool   `json:"fixes_permitted" yaml:"fixes_permitted"`
}

var flowSCMRepository = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

func LoadFlowSCMFixture(caseRoot string, require string) (*FlowSCMFixture, error) {
	if require != "repository" && require != "pull_request" {
		return nil, fmt.Errorf("unsupported SCM requirement %q", require)
	}
	scm := filepath.Join(caseRoot, "scm")
	repositoryPath := filepath.Join(scm, "repository.yaml")
	repositorySource, err := os.ReadFile(repositoryPath)
	if err != nil {
		return nil, fmt.Errorf("read repository.yaml: %w", err)
	}
	var repository FlowSCMRepository
	if err := yamlcodec.Unmarshal(repositorySource, &repository); err != nil {
		return nil, fmt.Errorf("repository.yaml: %w", err)
	}
	if !flowSCMRepository.MatchString(repository.Repository) {
		return nil, fmt.Errorf("repository must be owner/name")
	}
	if err := validateFlowSCMBranch("base_branch", repository.BaseBranch); err != nil {
		return nil, err
	}
	if err := validateFlowSCMBranch("head_branch", repository.HeadBranch); err != nil {
		return nil, err
	}
	if repository.BaseBranch == repository.HeadBranch {
		return nil, fmt.Errorf("base_branch and head_branch must differ")
	}
	fixture := &FlowSCMFixture{Repository: repository}
	if require == "repository" {
		return fixture, nil
	}
	pullRequestSource, err := os.ReadFile(filepath.Join(scm, "pull-request.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read pull-request.yaml: %w", err)
	}
	var pullRequest FlowSCMPullRequest
	if err := yamlcodec.Unmarshal(pullRequestSource, &pullRequest); err != nil {
		return nil, fmt.Errorf("pull-request.yaml: %w", err)
	}
	if err := requireFlowSCMPullRequestFields(pullRequestSource); err != nil {
		return nil, err
	}
	if pullRequest.Number <= 0 || strings.TrimSpace(pullRequest.Title) == "" {
		return nil, fmt.Errorf("pull request number and title are required")
	}
	if pullRequest.Base != repository.BaseBranch || pullRequest.Head != repository.HeadBranch {
		return nil, fmt.Errorf("pull request base/head must match repository branches")
	}
	if pullRequest.State != "OPEN" && pullRequest.State != "CLOSED" && pullRequest.State != "MERGED" {
		return nil, fmt.Errorf("unsupported pull request state %q", pullRequest.State)
	}
	if pullRequest.CIStatus != "passed" && pullRequest.CIStatus != "failed" && pullRequest.CIStatus != "pending" {
		return nil, fmt.Errorf("unsupported pull request ci_status %q", pullRequest.CIStatus)
	}
	patch, err := os.ReadFile(filepath.Join(scm, "head.patch"))
	if err != nil {
		return nil, fmt.Errorf("read head.patch: %w", err)
	}
	if err := validateFlowHeadPatch(patch); err != nil {
		return nil, err
	}
	fixture.PullRequest = &pullRequest
	fixture.HeadPatch = string(patch)
	return fixture, nil
}

func validateFlowSCMBranch(name, branch string) error {
	if branch == "" || strings.TrimSpace(branch) != branch || strings.ContainsAny(branch, " \t\n\r~^:?*[\\") || strings.Contains(branch, "..") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".lock") {
		return fmt.Errorf("%s is not a valid branch", name)
	}
	return nil
}

func requireFlowSCMPullRequestFields(source []byte) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(normalizeJSON(source), &values); err != nil {
		return err
	}
	for _, field := range []string{"number", "title", "base", "head", "state", "ci_status", "fixes_permitted"} {
		value, ok := values[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("pull-request.yaml %s is required", field)
		}
	}
	return nil
}

func validateFlowHeadPatch(patch []byte) error {
	if !utf8.Valid(patch) || strings.IndexByte(string(patch), 0) >= 0 {
		return fmt.Errorf("head.patch must be UTF-8 text without NUL")
	}
	text := string(patch)
	if text == "" {
		return fmt.Errorf("head.patch is empty")
	}
	for _, marker := range []string{"GIT binary patch", "Binary files", "--- /", "+++ /"} {
		if strings.Contains(text, marker) {
			return fmt.Errorf("head.patch contains forbidden %q", marker)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			fields := strings.Fields(line)
			if len(fields) != 4 || invalidFlowPatchPath(strings.TrimPrefix(fields[2], "a/")) || invalidFlowPatchPath(strings.TrimPrefix(fields[3], "b/")) || !strings.HasPrefix(fields[2], "a/") || !strings.HasPrefix(fields[3], "b/") {
				return fmt.Errorf("head.patch contains invalid path")
			}
		}
		if strings.HasPrefix(line, "--- a/") || strings.HasPrefix(line, "+++ b/") {
			if invalidFlowPatchPath(line[6:]) {
				return fmt.Errorf("head.patch contains invalid path")
			}
		}
	}
	return nil
}

func invalidFlowPatchPath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
