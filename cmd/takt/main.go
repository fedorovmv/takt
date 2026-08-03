package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"takt/internal/command"
	cfgpkg "takt/internal/config"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
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
	case "status":
		return statusCmd(args[1:])
	case "command":
		return commandCmd(args[1:])
	case "version":
		fmt.Println("takt v0.1.1-alpha")
		return nil
	default:
		return usage()
	}
}

func validateCmd(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
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
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
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
	_ = printResult(*jsonOut, state)
	if errors.Is(runErr, runtime.ErrWaiting) {
		return nil
	}
	return runErr
}

func answerCmd(args []string) error {
	fs := flag.NewFlagSet("answer", flag.ContinueOnError)
	workspace := fs.String("workspace", ".", "workspace")
	value := fs.String("value", "", "answer value")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--value": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: takt answer <run-id> <node-id> --value text")
	}
	absWorkspace, _ := filepath.Abs(*workspace)
	st := store.FS{Workspace: absWorkspace}
	state, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	nodeID := fs.Arg(1)
	if state.Waiting == nil || state.Waiting.NodeID != nodeID {
		return fmt.Errorf("run is not waiting for approval node %q", nodeID)
	}
	state.Approvals[nodeID] = *value
	if ns := state.Nodes[nodeID]; ns != nil {
		ns.Status = "pending"
	}
	state.Status = "running"
	state.Waiting = nil
	if err := st.Save(state); err != nil {
		return err
	}
	wf, err := workflow.Load(state.WorkflowPath)
	if err != nil {
		return err
	}
	cfg, err := cfgpkg.Load(state.ConfigPath)
	if err != nil {
		return err
	}
	runner := runtime.New(wf, cfg, state.WorkflowPath, state.ConfigPath, state.Workspace)
	state, runErr := runner.Resume(context.Background(), state)
	_ = printResult(*jsonOut, state)
	if errors.Is(runErr, runtime.ErrWaiting) {
		return nil
	}
	return runErr
}

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
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
	fs := flag.NewFlagSet("command run", flag.ContinueOnError)
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
	resolver := command.Resolver{Dirs: []string{filepath.Join(abs, ".takt", "commands"), filepath.Join(abs, "commands")}}
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
	_ = printResult(*jsonOut, state)
	return runErr
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
func printResult(jsonOut bool, v any) error {
	if jsonOut {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("%+v\n", v)
	return nil
}
func usage() error { return fmt.Errorf("usage: takt <validate|run|answer|status|command|version>") }
