package gobenchmark

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"takt/internal/config"
	"takt/internal/spec"
	"takt/internal/workflow"
)

func TestBenchmarkDefaultOutputIsOutsideRepository(t *testing.T) {
	sandbox := t.TempDir()
	root := filepath.Join(sandbox, "repo")
	example := filepath.Join(root, "examples", "go-benchmark")
	bin := filepath.Join(root, "bin")
	fakeBin := filepath.Join(root, "fake-bin")
	tmp := filepath.Join(sandbox, "tmp")
	for _, dir := range []string{example, bin, fakeBin, tmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	runScript, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	runPath := filepath.Join(example, "run.sh")
	if err := os.WriteFile(runPath, runScript, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "takt-args")
	if err := os.WriteFile(filepath.Join(bin, "takt"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_PATH\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(runPath)
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+tmp,
		"TAKT_BENCH_OUTPUT=",
		"TAKT_BENCH_HOST=opencode",
		"CAPTURE_PATH="+capture,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run benchmark launcher: %v\n%s", err, output)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, "takt-go-benchmark", "evals", "opencode")
	if !strings.Contains(string(args), "--output\n"+want+"\n") {
		t.Fatalf("takt args = %q, want external output %q", args, want)
	}
}

func TestStrategiesUseIdenticalImplementationPrompt(t *testing.T) {
	baseline, err := workflow.Load("strategies/baseline-direct.yaml")
	if err != nil {
		t.Fatal(err)
	}
	repair, err := workflow.Load("strategies/feedback-repair.yaml")
	if err != nil {
		t.Fatal(err)
	}

	baselineNode := nodeByID(t, baseline.Nodes, "implement")
	repairNode := nodeByID(t, repair.Nodes, "implement")
	if baselineNode.Prompt != repairNode.Prompt {
		t.Fatal("direct and repair must use the same first-attempt prompt")
	}
}

func TestOpenCodeBenchmarkRunsPureWithoutSkills(t *testing.T) {
	cfg, err := config.Load("config.opencode.yaml")
	if err != nil {
		t.Fatal(err)
	}
	assistant := cfg.Assistants["opencode"]
	if !slices.Equal(assistant.Args, []string{"--pure"}) {
		t.Fatalf("OpenCode args = %v, want [--pure]", assistant.Args)
	}
	model := cfg.Models["go-model"]
	if model.Provider != "aihub-sbt" || model.ID != "Qwen/Qwen3-Coder-Next" {
		t.Fatalf("OpenCode model = %s/%s, want aihub-sbt/Qwen/Qwen3-Coder-Next", model.Provider, model.ID)
	}
	for _, path := range []string{"strategies/baseline-direct.yaml", "strategies/feedback-repair.yaml"} {
		wf, err := workflow.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		implement := nodeByID(t, wf.Nodes, "implement")
		if implement.Skills == nil || len(*implement.Skills) != 0 {
			t.Fatalf("%s implement skills = %#v, want explicit empty allowlist", path, implement.Skills)
		}
	}
}

func nodeByID(t *testing.T, nodes []spec.Node, id string) spec.Node {
	t.Helper()
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("node %q not found", id)
	return spec.Node{}
}
