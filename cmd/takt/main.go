package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"takt/internal/command"
	cfgpkg "takt/internal/config"
	"takt/internal/definition"
	"takt/internal/evaluation"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func main() {
	args := os.Args[1:]
	if err := run(args); err != nil {
		if wantsJSON(args) {
			_ = printErrorJSON(err)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "validate":
		return validateCmd(args[1:])
	case "run":
		return runCmd(args[1:])
	case "answer":
		return answerCmd(args[1:])
	case "resume":
		return resumeCmd(args[1:])
	case "status":
		return statusCmd(args[1:])
	case "command":
		return commandCmd(args[1:])
	case "eval":
		return evalCmd(args[1:])
	case "version":
		fmt.Println("takt v0.1.13-alpha")
		return nil
	default:
		return usage()
	}
}

func validateCmd(args []string) error {
	fs := newFlagSet("validate")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--config": true, "--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt validate <workflow> [--config path]")
	}
	wfPath, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	cfgPath, err := filepath.Abs(*configPath)
	if err != nil {
		return err
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	wf, err := workflow.Load(wfPath)
	if err != nil {
		return err
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return err
	}
	resolver := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace).Commands
	if err := validateReferences(wf.Nodes, wf.Defaults, cfg, resolver); err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"valid": true, "workflow": wf.Metadata.Name})
}

func runCmd(args []string) error {
	fs := newFlagSet("run")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	input := fs.String("input", "", "input text or file")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--config": true, "--workspace": true, "--input": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run <workflow> [flags]")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	wfPath, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	cfgPath, err := filepath.Abs(*configPath)
	if err != nil {
		return err
	}
	wf, err := workflow.Load(wfPath)
	if err != nil {
		return err
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return err
	}
	resolver := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace).Commands
	if err := validateReferences(wf.Nodes, wf.Defaults, cfg, resolver); err != nil {
		return err
	}
	inputValue, err := readInput(*input)
	if err != nil {
		return err
	}
	runner := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace)
	state, runErr := runner.Start(context.Background(), inputValue)
	if errors.Is(runErr, runtime.ErrWaiting) {
		return printResult(*jsonOut, state)
	}
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

func answerCmd(args []string) error {
	fs := newFlagSet("answer")
	workspace := fs.String("workspace", ".", "workspace")
	value := fs.String("value", "", "answer value")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--value": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: takt answer <run-id> <node-id> --value text")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: absWorkspace}
	release, err := st.AcquireLock(fs.Arg(0))
	if err != nil {
		return err
	}
	defer release()
	state, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	runner, err := runnerForState(state)
	if err != nil {
		return err
	}
	if err := runner.VerifyDefinitions(state); err != nil {
		return err
	}
	nodeID := fs.Arg(1)
	if state.Waiting == nil || state.Waiting.NodeID != nodeID {
		return fmt.Errorf("run is not waiting for approval node %q", nodeID)
	}
	if state.Approvals == nil {
		state.Approvals = map[string]string{}
	}
	state.Approvals[nodeID] = *value
	if ns := state.Nodes[nodeID]; ns != nil {
		ns.Status = store.NodePending
	}
	state.Status = store.RunRunning
	state.Waiting = nil
	if err := st.Commit(state, store.Event{Type: "approval.answered", NodeID: nodeID, Data: map[string]any{"value_captured": true}}); err != nil {
		return err
	}
	state, runErr := runner.Resume(context.Background(), state)
	if errors.Is(runErr, runtime.ErrWaiting) {
		return printResult(*jsonOut, state)
	}
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

func resumeCmd(args []string) error {
	fs := newFlagSet("resume")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt resume <run-id>")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: absWorkspace}
	release, err := st.AcquireLock(fs.Arg(0))
	if err != nil {
		return err
	}
	defer release()
	state, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	runner, err := runnerForState(state)
	if err != nil {
		return err
	}
	state, runErr := runner.Resume(context.Background(), state)
	if errors.Is(runErr, runtime.ErrWaiting) {
		return printResult(*jsonOut, state)
	}
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

func runnerForState(state *store.RunState) (*runtime.Runner, error) {
	wf, err := workflow.Load(state.WorkflowPath)
	if err != nil {
		return nil, err
	}
	cfg, err := cfgpkg.Load(state.ConfigPath)
	if err != nil {
		return nil, err
	}
	runner := runtime.New(wf, cfg, state.WorkflowPath, state.ConfigPath, state.Workspace)
	if err := validateReferences(wf.Nodes, wf.Defaults, cfg, runner.Commands); err != nil {
		return nil, err
	}
	return runner, nil
}

func statusCmd(args []string) error {
	fs := newFlagSet("status")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt status <run-id>")
	}
	abs, _ := filepath.Abs(*workspace)
	state, err := (store.FS{Workspace: abs}).Load(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}

func commandCmd(args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return fmt.Errorf("usage: takt command run <name> [flags]")
	}
	fs := newFlagSet("command run")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	input := fs.String("input", "", "input text or file")
	assistantName := fs.String("assistant", "", "override assistant")
	modelName := fs.String("model", "", "override model")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args[1:], map[string]bool{"--config": true, "--workspace": true, "--input": true, "--assistant": true, "--model": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt command run <name>")
	}
	abs, _ := filepath.Abs(*workspace)
	cfgPath, _ := filepath.Abs(*configPath)
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return err
	}
	dirs := []string{filepath.Join(abs, ".takt", "commands"), filepath.Join(abs, "commands")}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".takt", "commands"))
	}
	resolver := command.Resolver{Dirs: dirs}
	cmd, err := resolver.Resolve(fs.Arg(0))
	if err != nil {
		return err
	}
	a := *assistantName
	if a == "" {
		a = cmd.Assistant
	}
	m := *modelName
	if m == "" {
		m = cmd.Model
	}
	if a == "" || m == "" {
		return fmt.Errorf("command must resolve assistant and model")
	}
	inputValue, err := readInput(*input)
	if err != nil {
		return err
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "command-" + fs.Arg(0)}, Defaults: spec.Defaults{Assistant: a, Model: m}, Nodes: []spec.Node{{ID: "command", Command: fs.Arg(0)}}}
	runner := runtime.New(wf, cfg, "<command>", cfgPath, abs)
	runner.Commands = resolver
	state, runErr := runner.Start(context.Background(), inputValue)
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

func evalCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt eval <run|report> [flags]")
	}
	switch args[0] {
	case "run":
		fs := newFlagSet("eval run")
		configPath := fs.String("config", ".takt/config.yaml", "config path")
		casesDir := fs.String("cases", "", "directory containing Markdown cases")
		templateDir := fs.String("workspace-template", "", "workspace template directory")
		outputDir := fs.String("output", ".takt/evals/latest", "evaluation output directory")
		repeat := fs.Int("repeat", 1, "number of repetitions per case")
		answer := fs.String("answer", "", "automatic approval answer")
		replace := fs.Bool("replace", false, "replace existing case workspaces")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{
			"--config": true, "--cases": true, "--workspace-template": true, "--output": true,
			"--repeat": true, "--answer": true, "--replace": false, "--json": false,
		}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval run <workflow> --config path --cases dir --workspace-template dir [flags]")
		}
		report, err := evaluation.Run(context.Background(), evaluation.RunOptions{
			WorkflowPath: fs.Arg(0), ConfigPath: *configPath, CasesDir: *casesDir,
			WorkspaceTemplate: *templateDir, OutputDir: *outputDir, Repeat: *repeat,
			ApprovalAnswer: *answer, Replace: *replace,
		})
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
		report, err := evaluation.LoadReport(fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	default:
		return fmt.Errorf("usage: takt eval <run|report> [flags]")
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func interspersed(args []string, takesValue map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := arg
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			name = name[:idx]
			continue
		}
		if takesValue[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func validateReferences(nodes []spec.Node, defaults spec.Defaults, cfg *spec.Config, resolver command.Resolver) error {
	for _, n := range nodes {
		assistantName, modelName := n.Assistant, n.Model
		if n.Command != "" {
			cmd, err := resolver.Resolve(n.Command)
			if err != nil {
				return fmt.Errorf("node %q: %w", n.ID, err)
			}
			if assistantName == "" {
				assistantName = cmd.Assistant
			}
			if modelName == "" {
				modelName = cmd.Model
			}
		}
		if n.Command != "" || n.Prompt != "" {
			if assistantName == "" {
				assistantName = defaults.Assistant
			}
			if modelName == "" {
				modelName = defaults.Model
			}
			if _, ok := cfg.Assistants[assistantName]; !ok {
				return fmt.Errorf("node %q references unknown assistant %q", n.ID, assistantName)
			}
			if _, ok := cfg.Models[modelName]; !ok {
				return fmt.Errorf("node %q references unknown model %q", n.ID, modelName)
			}
		}
		if n.LoopGroup != nil {
			if err := validateReferences(n.LoopGroup.Nodes, defaults, cfg, resolver); err != nil {
				return fmt.Errorf("loop_group %q: %w", n.ID, err)
			}
		}
	}
	return nil
}

func readInput(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if b, err := os.ReadFile(v); err == nil {
		return string(b), nil
	}
	return v, nil
}
func printResult(jsonOut bool, value any) error {
	if jsonOut {
		b, err := json.MarshalIndent(map[string]any{"ok": true, "result": value}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("%+v\n", value)
	return nil
}

func wantsJSON(args []string) bool {
	value := false
	if len(args) > 0 {
		switch args[0] {
		case "run", "answer", "resume", "status", "eval":
			value = true
		case "command":
			value = len(args) > 1 && args[1] == "run"
		}
	}
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			value = true
		}
		if arg == "--json=false" {
			value = false
		}
	}
	return value
}

func printErrorJSON(err error) error {
	code := "internal_error"
	details := map[string]any{}
	retryable := false
	var runErr *runtime.RunFailedError
	if errors.As(err, &runErr) {
		code = runErr.Code
		if code == "" {
			code = "run_failed"
		}
		details["run_id"] = runErr.RunID
		details["node_id"] = runErr.NodeID
	}
	var changed *definition.ChangedError
	if errors.As(err, &changed) {
		code = "definition_changed"
		details["definition"] = changed.Kind
	}
	var inconsistent *store.InconsistentError
	if errors.As(err, &inconsistent) {
		code = "store_inconsistent"
		details["run_id"] = inconsistent.RunID
	}
	payload := map[string]any{"ok": false, "error": map[string]any{
		"code": code, "message": err.Error(), "retryable": retryable, "details": details,
	}}
	b, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Fprintln(os.Stderr, string(b))
	return nil
}

func usage() error {
	return fmt.Errorf("usage: takt <validate|run|answer|resume|status|command|eval|version>")
}
