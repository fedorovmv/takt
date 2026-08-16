package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"takt/internal/bootstrap"
	"takt/internal/tooling"
)

func evalCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt eval <flow|run|report|stats|status|inspect|benchmark|task-benchmark|compare> [flags]")
	}
	app, err := bootstrap.New(".", ".takt/config.yaml")
	if err != nil {
		return err
	}
	service := app.Tooling.Evaluation
	switch args[0] {
	case "flow":
		if len(args) > 1 && args[1] == "init" {
			fs := newFlagSet("eval flow init")
			output := fs.String("output", "", "directory for the new flow suite")
			jsonOut := fs.Bool("json", true, "JSON output")
			if err := fs.Parse(interspersed(args[2:], map[string]bool{"--output": true, "--json": false})); err != nil {
				return err
			}
			if fs.NArg() != 1 || *output == "" {
				return fmt.Errorf("usage: takt eval flow init <workflow-selector> --output <directory>")
			}
			result, err := service.FlowInit(ctx, fs.Arg(0), *output)
			if err != nil {
				return err
			}
			if *jsonOut {
				return printResult(true, result)
			}
			fmt.Printf("created %s; add config.yaml, implement ./validator, and replace the example case before running takt eval flow %s/suite.yaml\n", result.(map[string]any)["output"], result.(map[string]any)["output"])
			return nil
		}
		fs := newFlagSet("eval flow")
		caseID := fs.String("case", "", "run one case")
		repeat := fs.Int("repeat", 1, "number of repetitions per case")
		outputDir := fs.String("output", "", "evaluation output directory")
		keepWorkspaces := fs.Bool("keep-workspaces", false, "retain case workspaces")
		modelPreset := fs.String("model-preset", "", "model preset from suite config")
		var modelOverrides modelOverrideFlag
		fs.Var(&modelOverrides, "model", "override model alias=provider/model; repeatable")
		assistantIdleTimeout := fs.Duration("assistant-idle-timeout", 5*time.Minute, "fail an assistant node after this long without progress")
		trace := fs.Bool("trace", false, "write live durable progress to stderr")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{"--case": true, "--repeat": true, "--output": true, "--model-preset": true, "--model": true, "--assistant-idle-timeout": true, "--keep-workspaces": false, "--trace": false, "--json": false}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval flow <suite.yaml> [--case ID] [--repeat N] [--output DIR] [--assistant-idle-timeout DURATION] [--keep-workspaces] [--trace] [--json]")
		}
		if *repeat <= 0 {
			return fmt.Errorf("repeat must be positive")
		}
		if *assistantIdleTimeout <= 0 {
			return fmt.Errorf("assistant-idle-timeout must be positive")
		}
		invocation, err := filepath.Abs(".")
		if err != nil {
			return err
		}
		var traceFn func(string)
		if *trace {
			traceFn = newEvalTrace(os.Stderr, time.Now)
		}
		environmentOverrides, err := currentEnvironmentModelOverrides()
		if err != nil {
			return err
		}
		overrides := mergeModelOverrides(environmentOverrides, modelOverrides)
		report, err := service.Flow(ctx, tooling.FlowEvaluationRequest{SuitePath: fs.Arg(0), CaseID: *caseID, OutputDir: *outputDir, InvocationWorkspace: invocation, Repeat: *repeat, KeepWorkspaces: *keepWorkspaces, ModelPreset: *modelPreset, ModelOverrides: overrides, Trace: traceFn, AssistantIdleTimeout: *assistantIdleTimeout})
		if err != nil {
			if report != nil {
				if printErr := printResult(*jsonOut, report); printErr != nil {
					return printErr
				}
			}
			return err
		}
		return printResult(*jsonOut, report)
	case "run":
		fs := newFlagSet("eval run")
		configPath := fs.String("config", ".takt/config.yaml", "config path")
		modelPreset := fs.String("model-preset", "", "model preset")
		var modelOverrides modelOverrideFlag
		fs.Var(&modelOverrides, "model", "override model alias=provider/model; repeatable")
		casesDir := fs.String("cases", "", "directory containing Markdown cases")
		caseManifest := fs.String("case-manifest", "", "optional YAML metadata for benchmark cases")
		templateDir := fs.String("workspace-template", "", "workspace template directory")
		outputDir := fs.String("output", ".takt/evals/latest", "evaluation output directory")
		repeat := fs.Int("repeat", 1, "number of repetitions per case")
		answer := fs.String("answer", "", "automatic approval answer")
		replace := fs.Bool("replace", false, "replace existing case workspaces")
		strategyID := fs.String("strategy-id", "", "stable strategy identifier")
		benchmarkID := fs.String("benchmark-id", "", "stable benchmark identifier")
		qualityNode := fs.String("quality-node", "", "node that emits takt-validation/v1alpha1")
		generationNode := fs.String("generation-node", "", "generation node used for success@1")
		validatorID := fs.String("validator-id", "", "validator identifier")
		validatorVersion := fs.String("validator-version", "", "validator version")
		validatorPath := fs.String("validator-path", "", "validator file or directory to fingerprint")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{
			"--config": true, "--model-preset": true, "--model": true, "--cases": true, "--case-manifest": true, "--workspace-template": true, "--output": true,
			"--repeat": true, "--answer": true, "--replace": false, "--json": false,
			"--strategy-id": true, "--benchmark-id": true, "--quality-node": true,
			"--generation-node": true, "--validator-id": true, "--validator-version": true,
			"--validator-path": true,
		}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval run <workflow> --config path --cases dir --workspace-template dir [flags]")
		}
		environmentOverrides, err := currentEnvironmentModelOverrides()
		if err != nil {
			return err
		}
		overrides := mergeModelOverrides(environmentOverrides, modelOverrides)
		report, err := service.Run(ctx, tooling.EvaluationRunRequest{
			WorkflowPath: fs.Arg(0), ConfigPath: *configPath, CasesDir: *casesDir,
			WorkspaceTemplate: *templateDir, OutputDir: *outputDir, Repeat: *repeat,
			ApprovalAnswer: *answer, Replace: *replace,
			StrategyID: *strategyID, BenchmarkID: *benchmarkID,
			QualityNode: *qualityNode, GenerationNode: *generationNode,
			ValidatorID: *validatorID, ValidatorVersion: *validatorVersion, ValidatorPath: *validatorPath,
			CaseManifestPath: *caseManifest, ModelPreset: *modelPreset, ModelOverrides: overrides,
		})
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	case "benchmark":
		fs := newFlagSet("eval benchmark")
		outputDir := fs.String("output", "", "benchmark output directory")
		repeat := fs.Int("repeat", 0, "override repetitions per case from matrix")
		replace := fs.Bool("replace", false, "replace existing benchmark output")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{"--output": true, "--repeat": true, "--replace": false, "--json": false}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval benchmark <matrix.yaml> [--output dir] [--repeat N] [--replace]")
		}
		report, err := service.Benchmark(ctx, tooling.EvaluationBenchmarkRequest{MatrixPath: fs.Arg(0), OutputDir: *outputDir, Repeat: *repeat, Replace: *replace})
		if err != nil {
			if report != nil {
				if printErr := printResult(*jsonOut, report); printErr != nil {
					return printErr
				}
			}
			return err
		}
		return printResult(*jsonOut, report)
	case "task-benchmark":
		fs := newFlagSet("eval task-benchmark")
		outputDir := fs.String("output", "", "task benchmark output directory")
		repeat := fs.Int("repeat", 0, "override repetitions per case from matrix")
		replace := fs.Bool("replace", false, "replace existing task benchmark output")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{"--output": true, "--repeat": true, "--replace": false, "--json": false}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval task-benchmark <matrix.yaml> [--output dir] [--repeat N] [--replace]")
		}
		report, err := service.TaskBenchmark(ctx, tooling.EvaluationBenchmarkRequest{MatrixPath: fs.Arg(0), OutputDir: *outputDir, Repeat: *repeat, Replace: *replace})
		if err != nil {
			if report != nil {
				if printErr := printResult(*jsonOut, report); printErr != nil {
					return printErr
				}
			}
			return err
		}
		return printResult(*jsonOut, report)
	case "compare":
		fs := newFlagSet("eval compare")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return fmt.Errorf("usage: takt eval compare <baseline-output-dir> <candidate-output-dir>")
		}
		report, err := service.Compare(ctx, fs.Arg(0), fs.Arg(1))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	case "stats":
		fs := newFlagSet("eval stats")
		jsonOut := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval stats <evaluation-output-dir> [--json]")
		}
		stats, err := service.Stats(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, stats)
	case "status":
		fs := newFlagSet("eval status")
		jsonOut := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval status <evaluation-output-dir> [--json]")
		}
		status, err := service.Status(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, status)
	case "inspect":
		fs := newFlagSet("eval inspect")
		caseID := fs.String("case", "", "inspect one case")
		repeat := fs.Int("repeat", 0, "inspect one repeat of the selected case")
		jsonOut := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--case": true, "--repeat": true, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval inspect <evaluation-output-dir> [--case ID] [--repeat N] [--json]")
		}
		if *repeat < 0 {
			return fmt.Errorf("repeat cannot be negative")
		}
		if *repeat > 0 && *caseID == "" {
			return fmt.Errorf("repeat requires --case")
		}
		inspection, err := service.Inspect(ctx, tooling.EvaluationInspectRequest{OutputDir: fs.Arg(0), CaseID: *caseID, Repeat: *repeat})
		if err != nil {
			return err
		}
		return printResult(*jsonOut, inspection)
	case "report":
		fs := newFlagSet("eval report")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval report <evaluation-output-dir>")
		}
		report, err := service.Report(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	default:
		return fmt.Errorf("usage: takt eval <flow|run|report|stats|status|inspect|benchmark|task-benchmark|compare> [flags]")
	}
}

func newEvalTrace(writer io.Writer, now func() time.Time) func(string) {
	started := now()
	var mu sync.Mutex
	return func(line string) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(writer, "[%s] %s\n", now().Sub(started).Truncate(time.Second), line)
	}
}
