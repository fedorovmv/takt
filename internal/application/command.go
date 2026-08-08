package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"takt/internal/command"
	cfgpkg "takt/internal/config"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
)

type CommandRunRequest struct {
	Name       string
	Input      string
	Assistant  string
	Model      string
	ConfigPath string
}

// CommandService runs a named Takt command through the same durable runtime as
// workflows. CLI parsing stays outside; command resolution and runtime setup do not.
type CommandService struct{ *Context }

func (s *CommandService) Run(ctx context.Context, request CommandRunRequest) (*store.RunState, error) {
	if request.Name == "" {
		return nil, fmt.Errorf("command name is required")
	}
	cfgPath := s.ConfigPath
	if request.ConfigPath != "" {
		var err error
		cfgPath, err = s.absoluteFromWorkspace(request.ConfigPath)
		if err != nil {
			return nil, err
		}
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	dirs := []string{filepath.Join(s.Workspace, ".takt", "commands"), filepath.Join(s.Workspace, "commands")}
	if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".takt", "commands"))
	}
	resolver := command.Resolver{Dirs: dirs}
	definition, err := resolver.Resolve(request.Name)
	if err != nil {
		return nil, err
	}
	assistantName := request.Assistant
	if assistantName == "" {
		assistantName = definition.Assistant
	}
	modelName := request.Model
	if modelName == "" {
		modelName = definition.Model
	}
	if assistantName == "" || modelName == "" {
		return nil, fmt.Errorf("command must resolve assistant and model")
	}
	input := request.Input
	if input != "" {
		candidate := input
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(s.Workspace, candidate)
		}
		if raw, readErr := os.ReadFile(candidate); readErr == nil {
			input = string(raw)
		}
	}
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "command-" + request.Name},
		Defaults: spec.Defaults{Assistant: assistantName, Model: modelName},
		Nodes:    []spec.Node{{ID: "command", Command: request.Name}},
	}
	runner := s.runnerFactory(runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: "<command>", ConfigPath: cfgPath, ControlWorkspace: s.Workspace}, RunnerOptions{Commands: &resolver})
	state, runErr := runner.Start(ctx, input)
	if runErr != nil {
		return nil, runErr
	}
	return durablePublicRun(s.runStore, state)
}
