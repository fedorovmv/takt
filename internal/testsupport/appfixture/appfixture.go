package appfixture

import (
	"path/filepath"

	"takt/internal/application"
	"takt/internal/runtime"
	"takt/internal/store"
)

// New creates a repository-local application graph for tests without making
// production application code responsible for concrete infrastructure wiring.
func New(workspace, configPath string) (*application.Services, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return application.NewWithDependencies(abs, configPath, application.Dependencies{
		RunStore: store.FS{Workspace: abs},
		RunnerFactory: func(def runtime.Definition, options application.RunnerOptions) *runtime.Runner {
			deps := runtime.DefaultDependencies(def)
			if options.Commands != nil {
				deps.Commands = *options.Commands
			}
			return runtime.NewWithDependencies(def, deps)
		},
	})
}
