package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"takt/internal/application"
	"takt/internal/bootstrap"
	"takt/internal/tooling"
)

type evaluationGateFlag map[string]tooling.FlowEvaluationGate

func (f *evaluationGateFlag) String() string { return "" }

func (f *evaluationGateFlag) Set(raw string) error {
	key, rawValue, ok := strings.Cut(raw, "=")
	metric, bound, hasBound := strings.Cut(key, ".")
	allowed := map[string]bool{"valid_rate": true, "false_accept_rate": true, "false_reject_rate": true, "flow_completion_rate": true, "validation_error_rate": true}
	value, err := strconv.ParseFloat(rawValue, 64)
	if !ok || !hasBound || !allowed[metric] || (bound != "min" && bound != "max") || err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fmt.Errorf("gate must be metric.min|max=value with a supported metric and value between 0 and 1")
	}
	if *f == nil {
		*f = evaluationGateFlag{}
	}
	if _, duplicate := (*f)[metric]; duplicate {
		return fmt.Errorf("gate %q is duplicated", metric)
	}
	gate := tooling.FlowEvaluationGate{}
	if bound == "min" {
		gate.Min = &value
	} else {
		gate.Max = &value
	}
	(*f)[metric] = gate
	return nil
}

func evalCmd(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt eval <flow|run|analyze|report|stats|status|inspect|benchmark|task-benchmark|compare> [flags]")
	}
	app, err := bootstrap.New(".", ".takt/config.yaml")
	if err != nil {
		return err
	}
	service := app.Tooling.Evaluation
	switch args[0] {
	case "analyze":
		fs := newFlagSet("eval analyze")
		configPath := fs.String("config", ".takt/config.yaml", "analyzer config path")
		modelPreset := fs.String("model-preset", "", "analyzer model preset")
		language := fs.String("language", tooling.DefaultEvaluationAnalysisLanguage, "analysis output language: en or ru")
		caseID := fs.String("case", "", "analyze one case")
		repeat := fs.Int("repeat", 0, "analyze one repeat of the selected case")
		trace := fs.Bool("trace", false, "write analysis progress to stderr")
		jsonOut := fs.Bool("json", false, "JSON output")
		values := map[string]bool{"--config": true, "--model-preset": true, "--language": true, "--case": true, "--repeat": true, "--trace": false, "--json": false}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if *repeat < 0 {
			return fmt.Errorf("repeat cannot be negative")
		}
		if flagPresent(args[1:], "--repeat") && *repeat == 0 {
			return fmt.Errorf("repeat must be positive")
		}
		if *repeat > 0 && strings.TrimSpace(*caseID) == "" {
			return fmt.Errorf("repeat requires --case")
		}
		if _, err := tooling.NormalizeEvaluationAnalysisLanguage(*language); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval analyze <evaluation-output-dir> [flags]")
		}
		var traceFn func(string)
		if *trace {
			traceFn = newEvalTrace(os.Stderr, time.Now)
		}
		result, err := service.Analyze(ctx, tooling.EvaluationAnalyzeRequest{OutputDir: fs.Arg(0), ConfigPath: *configPath, CaseID: *caseID, Repeat: *repeat, ModelPreset: *modelPreset, Language: *language, Trace: traceFn})
		if err != nil {
			if result != nil {
				if printErr := printResult(*jsonOut, result); printErr != nil {
					return printErr
				}
			}
			return err
		}
		return printResult(*jsonOut, result)
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
		target := fs.String("target", "", "workflow or profile selector evaluated for every case")
		configPath := fs.String("config", "", "evaluation config path")
		casesDir := fs.String("cases", "", "evaluation corpus directory")
		caseID := fs.String("case", "", "run one case")
		repeat := fs.Int("repeat", 1, "number of repetitions per case")
		outputDir := fs.String("output", "", "evaluation output directory")
		keepWorkspaces := fs.Bool("keep-workspaces", false, "retain case workspaces")
		modelPreset := fs.String("model-preset", "", "model preset from suite config")
		var modelOverrides modelOverrideFlag
		fs.Var(&modelOverrides, "model", "override model alias=provider/model; repeatable")
		var gates evaluationGateFlag
		fs.Var(&gates, "gate", "quality gate metric.min|max=value; repeatable")
		assistantIdleTimeout := fs.Duration("assistant-idle-timeout", 5*time.Minute, "fail an assistant node after this long without progress")
		trace := fs.Bool("trace", false, "write live durable progress to stderr")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{"--target": true, "--config": true, "--cases": true, "--case": true, "--repeat": true, "--output": true, "--model-preset": true, "--model": true, "--gate": true, "--assistant-idle-timeout": true, "--keep-workspaces": false, "--trace": false, "--json": false}
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
		report, err := service.Flow(ctx, tooling.FlowEvaluationRequest{SuitePath: fs.Arg(0), Target: *target, ConfigPath: *configPath, CasesDir: *casesDir, Gates: gates, CaseID: *caseID, OutputDir: *outputDir, InvocationWorkspace: invocation, Repeat: *repeat, KeepWorkspaces: *keepWorkspaces, ModelPreset: *modelPreset, ModelOverrides: overrides, Trace: traceFn, Deprecation: func(message string) { fmt.Fprintln(os.Stderr, "warning:", message) }, AssistantIdleTimeout: *assistantIdleTimeout})
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
		checkGates := fs.Bool("check-gates", false, "evaluate gates embedded in Run input")
		jsonOut := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--check-gates": false, "--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval stats <evaluation-output-dir> [--json]")
		}
		if exists, existsErr := runObservationExists(app, fs.Arg(0)); existsErr != nil {
			return existsErr
		} else if exists {
			stats, err := app.Core.RunService.Stats(application.RunStatsQuery{RunID: fs.Arg(0), CheckGates: *checkGates})
			if err != nil {
				return err
			}
			if err := printResult(*jsonOut, stats); err != nil {
				return err
			}
			return stats.GateFailure()
		}
		if *checkGates {
			return fmt.Errorf("--check-gates requires a Run ID")
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
		if exists, existsErr := runObservationExists(app, fs.Arg(0)); existsErr != nil {
			return existsErr
		} else if exists {
			status, err := app.Core.RunService.Status(fs.Arg(0))
			if err != nil {
				return err
			}
			return printResult(*jsonOut, status)
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
		if exists, existsErr := runObservationExists(app, fs.Arg(0)); existsErr != nil {
			return existsErr
		} else if exists {
			inspection, err := app.Core.RunService.Inspect(application.RunInspectQuery{RunID: fs.Arg(0), CaseID: *caseID, Repeat: *repeat})
			if err != nil {
				return err
			}
			return printResult(*jsonOut, inspection)
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
		return fmt.Errorf("usage: takt eval <flow|run|analyze|report|stats|status|inspect|benchmark|task-benchmark|compare> [flags]")
	}
}

func runObservationExists(app *bootstrap.App, value string) (bool, error) {
	if app == nil || app.Core == nil {
		return false, fmt.Errorf("application is not configured")
	}
	return app.Core.RunService.HasRun(value)
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
