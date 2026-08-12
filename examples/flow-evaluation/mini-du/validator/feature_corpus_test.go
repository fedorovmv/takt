package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"takt/internal/tooling/evaluation"
)

func TestFeatureCorpusManifest(t *testing.T) {
	root := filepath.Join("..", "feature-development")
	suite, err := evaluation.LoadFlowSuite(filepath.Join(root, "suite.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if suite.Workflow != "code:feature-development" || suite.Config != "../config.yaml" || suite.External.GitHub == nil || suite.External.GitHub.Require != "repository" {
		t.Fatalf("suite=%+v", suite)
	}
	cases, err := evaluation.DiscoverFlowCases(suite.SuitePath, suite, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"implement-basic", "implement-multiple-paths", "implement-symlink-and-hardlink"}
	got := make([]string, len(cases))
	for i, c := range cases {
		got[i] = c.ID
		if c.Expectation == nil || c.SCMPath == "" {
			t.Fatalf("case %s missing expectation/scm", c.ID)
		}
		cmd := exec.Command("go", "test", "./...")
		cmd.Dir = filepath.Join(c.WorkspacePath)
		if err := cmd.Run(); err != nil {
			t.Fatalf("case %s skeleton does not build: %v", c.ID, err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cases=%v want=%v", got, want)
	}
}
