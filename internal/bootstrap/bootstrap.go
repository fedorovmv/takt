package bootstrap

import (
	"path/filepath"

	"takt/internal/appapi"
	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/domainadapter"
	"takt/internal/redact"
	"takt/internal/runtime"
	"takt/internal/store"
)

// App is Takt's process composition root. Concrete runtime dependencies,
// application services and the canonical local API are created here;
// transports consume only the pieces they need.
type App struct {
	Workspace  string
	ConfigPath string
	Services   *application.Services
	API        *appapi.Registry
}

func New(workspace, configPath string) (*App, error) {
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	services, err := application.NewWithDependencies(absWorkspace, configPath, application.Dependencies{
		RunStore:         store.FS{Workspace: absWorkspace},
		EvaluationEngine: evaluationEngine{},
		RunnerFactory: func(def runtime.Definition, options application.RunnerOptions) *runtime.Runner {
			deps := runtime.Dependencies{
				Commands:   runtime.NewCommandResolver(def.WorkflowPath, def.ControlWorkspace, def.ControlWorkspace),
				Store:      store.FS{Workspace: def.ControlWorkspace},
				Assistants: assistant.Factory{Config: def.Config},
				Adapters:   domainadapter.Factory{Config: def.Config},
				Redactor:   redact.NewFromConfig(def.Config),
			}
			if options.Commands != nil {
				deps.Commands = *options.Commands
			}
			return runtime.NewWithDependencies(def, deps)
		},
	})
	if err != nil {
		return nil, err
	}
	return &App{
		Workspace: services.Workspace, ConfigPath: services.ConfigPath,
		Services: services, API: appapi.New(services),
	}, nil
}
