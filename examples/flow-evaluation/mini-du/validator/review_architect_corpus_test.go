package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"takt/internal/tooling/evaluation"
)

func TestReviewCorpusManifest(t *testing.T) {
	assertCorpus(t, "review", "code:comprehensive-pr-review", []string{"review-hardlink-bug", "review-path-with-spaces", "review-unrelated-change"}, []int{17, 18, 19})
}

func TestArchitectCorpusManifest(t *testing.T) {
	assertCorpus(t, "architect", "code:architect", []string{"collapse-redundant-layers", "preserve-behavior-during-simplification", "remove-single-implementation-factories"}, []int{28, 29, 27})
}

func TestReviewAndArchitectPreparationUsesPatchedHead(t *testing.T) {
	copy := filepath.Join(t.TempDir(), "mini-du")
	if err := evaluation.CopyFlowTree("..", copy); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(copy, "config.opencode.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copy, "config.yaml"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"review", "architect"} {
		workflow := "code:comprehensive-pr-review"
		if name == "architect" {
			workflow = "code:architect"
		}
		suite := corpusSuite(filepath.Join(copy, name), workflow, "pull_request")
		cases, err := evaluation.DiscoverFlowCases(suite.SuitePath, suite, "")
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := evaluation.PrepareFlowRepeat(context.Background(), suite, cases[0], 1, t.TempDir(), os.Getenv("PATH"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if prepared.BaseCommit == prepared.HeadCommit {
			t.Fatalf("%s did not apply its PR head", name)
		}
	}
}

func assertCorpus(t *testing.T, name, workflow string, wantIDs []string, wantPRs []int) {
	t.Helper()
	root := filepath.Join("..", name)
	suite := corpusSuite(root, workflow, "pull_request")
	if suite.Workflow != workflow || suite.External.GitHub == nil || suite.External.GitHub.Require != "pull_request" {
		t.Fatalf("suite=%+v", suite)
	}
	cases, err := evaluation.DiscoverFlowCases(suite.SuitePath, suite, "")
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]string, len(cases))
	for i, item := range cases {
		gotIDs[i] = item.ID
		if item.SCMPath == "" {
			t.Fatalf("case %s missing SCM fixture", item.ID)
		}
		var input struct {
			Repository     string `json:"repository"`
			PullRequest    int    `json:"pull_request"`
			FixesPermitted bool   `json:"fixes_permitted"`
		}
		data, err := os.ReadFile(filepath.Join(item.Root, "input.md"))
		if err != nil || json.Unmarshal(data, &input) != nil || input.Repository != "example/mini-du" || input.PullRequest <= 0 || !input.FixesPermitted {
			t.Fatalf("case %s input=%s err=%v", item.ID, data, err)
		}
		var oracle struct {
			AllowedPaths []string `json:"allowed_paths"`
			Artifacts    []string `json:"required_artifacts"`
			ForbiddenIDs []string `json:"forbidden_identifiers"`
			ForbiddenPkg []string `json:"forbidden_packages"`
		}
		if err := json.Unmarshal(item.Expectation.Oracle, &oracle); err != nil || len(oracle.AllowedPaths) == 0 || len(oracle.Artifacts) == 0 {
			t.Fatalf("case %s oracle=%s err=%v", item.ID, item.Expectation.Oracle, err)
		}
		if name == "architect" && len(oracle.ForbiddenIDs)+len(oracle.ForbiddenPkg) == 0 {
			t.Fatalf("case %s has no bounded smell", item.ID)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("cases=%v want=%v", gotIDs, wantIDs)
	}
	for i, id := range wantIDs {
		data, err := os.ReadFile(filepath.Join(root, "cases", id, "scm", "pull-request.yaml"))
		if err != nil || !bytes.Contains(data, []byte(fmt.Sprintf("number: %d", wantPRs[i]))) {
			t.Fatalf("case %s PR=%s err=%v", id, data, err)
		}
	}
}
