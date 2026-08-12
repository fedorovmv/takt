package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"takt/internal/bootstrap"
	"takt/internal/tooling"
)

func evalCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt eval <flow|run|report|benchmark|task-benchmark|compare> [flags]")
	}
	app, err := bootstrap.New(".", ".takt/config.yaml")
	if err != nil {
		return err
	}
	service := app.Tooling.Evaluation
	switch args[0] {
	case "flow":
		if len(args) == 2 && args[1] == "init" {
			return fmt.Errorf("eval flow init is not available until the authoring slice is installed")
		}
		fs := newFlagSet("eval flow")
		caseID := fs.String("case", "", "run one case")
		repeat := fs.Int("repeat", 1, "number of repetitions per case")
		outputDir := fs.String("output", ".takt/evals/latest", "evaluation output directory")
		keepWorkspaces := fs.Bool("keep-workspaces", false, "retain case workspaces")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{"--case": true, "--repeat": true, "--output": true, "--keep-workspaces": false, "--json": false}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval flow <suite.yaml> [--case ID] [--repeat N] [--output DIR] [--keep-workspaces] [--json]")
		}
		if *repeat <= 0 {
			return fmt.Errorf("repeat must be positive")
		}
		invocation, err := filepath.Abs(".")
		if err != nil {
			return err
		}
		report, err := service.Flow(ctx, tooling.FlowEvaluationRequest{SuitePath: fs.Arg(0), CaseID: *caseID, OutputDir: *outputDir, InvocationWorkspace: invocation, Repeat: *repeat, KeepWorkspaces: *keepWorkspaces})
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
			"--config": true, "--cases": true, "--case-manifest": true, "--workspace-template": true, "--output": true,
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
		report, err := service.Run(ctx, tooling.EvaluationRunRequest{
			WorkflowPath: fs.Arg(0), ConfigPath: *configPath, CasesDir: *casesDir,
			WorkspaceTemplate: *templateDir, OutputDir: *outputDir, Repeat: *repeat,
			ApprovalAnswer: *answer, Replace: *replace,
			StrategyID: *strategyID, BenchmarkID: *benchmarkID,
			QualityNode: *qualityNode, GenerationNode: *generationNode,
			ValidatorID: *validatorID, ValidatorVersion: *validatorVersion, ValidatorPath: *validatorPath,
			CaseManifestPath: *caseManifest,
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
		return fmt.Errorf("usage: takt eval <flow|run|report|benchmark|task-benchmark|compare> [flags]")
	}
}
