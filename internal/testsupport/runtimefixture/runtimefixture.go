package runtimefixture

import (
	"takt/internal/assistant"
	"takt/internal/command"
	"takt/internal/domainadapter"
	"takt/internal/redact"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
)

// Dependencies returns the concrete in-process runtime dependencies used by
// tests. Production composition belongs to internal/bootstrap; keeping this
// helper under testsupport prevents runtime from silently constructing its own
// infrastructure.
func Dependencies(def runtime.Definition) runtime.Dependencies {
	return runtime.Dependencies{
		Commands:   runtime.NewCommandResolver(def.WorkflowPath, def.ControlWorkspace, def.ControlWorkspace),
		Store:      store.FS{Workspace: def.ControlWorkspace},
		Assistants: assistant.Factory{Config: def.Config},
		Adapters:   domainadapter.Factory{Config: def.Config},
		Redactor:   redact.NewFromConfig(def.Config),
	}
}

func Runner(def runtime.Definition, commands *command.Resolver) *runtime.Runner {
	deps := Dependencies(def)
	if commands != nil {
		deps.Commands = *commands
	}
	return runtime.NewWithDependencies(def, deps)
}

func New(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) *runtime.Runner {
	def := runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: workflowPath, ConfigPath: configPath, ControlWorkspace: workspace}
	return Runner(def, nil)
}
