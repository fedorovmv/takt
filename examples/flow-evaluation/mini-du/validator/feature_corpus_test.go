package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"takt/internal/tooling/evaluation"
)

func TestFeatureCorpusManifest(t *testing.T) {
	root := filepath.Join("..", "feature-development")
	suite := corpusSuite(root, "code:feature-development", "repository")
	if suite.Workflow != "code:feature-development" || suite.External.GitHub == nil || suite.External.GitHub.Require != "repository" {
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
		var oracle struct {
			AllowedPaths []string `json:"allowed_paths"`
			Scenarios    []string `json:"scenarios"`
			Artifacts    []string `json:"required_artifacts"`
			RequirePR    bool     `json:"require_pr"`
			RequirePush  bool     `json:"require_push"`
		}
		if err := json.Unmarshal(c.Expectation.Oracle, &oracle); err != nil {
			t.Fatal(err)
		}
		if len(oracle.AllowedPaths) != 5 || len(oracle.Scenarios) == 0 || len(oracle.Artifacts) != 5 || !oracle.RequirePR || !oracle.RequirePush {
			t.Fatalf("case %s oracle=%+v", c.ID, oracle)
		}
		input, err := os.ReadFile(c.InputPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, contract := range []string{"Usage: mini-du [-s] [-k|-H] [--] [PATH...]", "1536 bytes as 1.5KiB", "-sH", "Without `-k`, numeric output is also integer kibibytes"} {
			if !strings.Contains(string(input), contract) {
				t.Fatalf("case %s input omits contract %q", c.ID, contract)
			}
		}
		for _, scenario := range []string{"summary", "kibibytes", "humanized", "help_short", "help_long", "double_dash", "combined_flags", "invalid_option"} {
			if !containsString(oracle.Scenarios, scenario) {
				t.Fatalf("case %s omits public flag scenario %s: %v", c.ID, scenario, oracle.Scenarios)
			}
		}
		if c.ID == "implement-basic" && !containsString(oracle.Scenarios, "double_dash_default") {
			t.Fatalf("case %s omits validator-v3 scenario double_dash_default: %v", c.ID, oracle.Scenarios)
		}
		if c.ID == "implement-symlink-and-hardlink" && !containsString(oracle.Scenarios, "hardlink_multiple") {
			t.Fatalf("case %s omits validator-v3 scenario hardlink_multiple: %v", c.ID, oracle.Scenarios)
		}
		if c.ID == "implement-basic" {
			for _, flag := range []string{"-s", "-k", "-H", "-h", "--help", "--"} {
				if !strings.Contains(string(input), flag) {
					t.Fatalf("basic input omits public flag %s: %s", flag, input)
				}
			}
			wantScenarios := []string{"empty", "nested", "summary", "kibibytes", "humanized", "help_short", "help_long", "double_dash", "combined_flags", "invalid_option", "missing", "double_dash_default"}
			if !reflect.DeepEqual(oracle.Scenarios, wantScenarios) {
				t.Fatalf("basic scenarios=%v want=%v", oracle.Scenarios, wantScenarios)
			}
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

func corpusSuite(root, workflow, scmRequirement string) *evaluation.FlowSuite {
	absolute, _ := filepath.Abs(root)
	suite := &evaluation.FlowSuite{
		Workflow: workflow, Config: "../config.yaml", Cases: evaluation.FlowCasesSpec{Directory: "cases"},
		SuitePath: filepath.Join(absolute, "evaluation.yaml"), SuiteDir: absolute,
		ResolvedConfig: filepath.Join(filepath.Dir(absolute), "config.yaml"), ResolvedCases: filepath.Join(absolute, "cases"),
	}
	if scmRequirement != "" {
		suite.External.GitHub = &evaluation.FlowGitHubSpec{Mode: "fixture", Require: scmRequirement}
	}
	return suite
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
