package application

import (
	"path/filepath"

	"takt/internal/runtime"
	"takt/internal/store"
)

func New(workspace, configPath string) (*Services, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return NewWithDependencies(abs, configPath, Dependencies{
		RunStore: store.FS{Workspace: abs},
		RunnerFactory: func(def runtime.Definition, options RunnerOptions) *runtime.Runner {
			deps := runtime.DefaultDependencies(def)
			if options.Commands != nil {
				deps.Commands = *options.Commands
			}
			return runtime.NewWithDependencies(def, deps)
		},
	})
}
