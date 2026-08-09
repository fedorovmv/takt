package runtime

import (
	"takt/internal/assistant"
	"takt/internal/domainadapter"
	assistantproviders "takt/internal/extensions/assistants"
	"takt/internal/redact"
	"takt/internal/spec"
	"takt/internal/store"
)

// New is a package-local test convenience. Production code must use explicit dependencies.
func New(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) *Runner {
	def := Definition{Workflow: wf, Config: cfg, WorkflowPath: workflowPath, ConfigPath: configPath, ControlWorkspace: workspace}
	return NewWithDependencies(def, DefaultDependencies(def))
}

func DefaultDependencies(def Definition) Dependencies {
	return Dependencies{
		Commands:   NewCommandResolver(def.WorkflowPath, def.ControlWorkspace, def.ControlWorkspace),
		Store:      store.FS{Workspace: def.ControlWorkspace},
		Assistants: assistant.Factory{Config: def.Config, Providers: assistantproviders.Factories()},
		Adapters:   domainadapter.Factory{Config: def.Config},
		Redactor:   redact.NewFromConfig(def.Config),
	}
}
