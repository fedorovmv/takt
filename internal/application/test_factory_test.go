package application

import (
	"path/filepath"

	"takt/internal/domainadapter"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/testsupport/runtimefixture"
)

func testAdapterFactory(cfg *spec.Config) domainadapter.Resolver {
	return domainadapter.Factory{Config: cfg}
}

func New(workspace, configPath string) (*Services, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return NewWithDependencies(abs, configPath, Dependencies{
		RunStore: store.FS{Workspace: abs},
		RunnerFactory: func(def runtime.Definition, options RunnerOptions) *runtime.Runner {
			return runtimefixture.Runner(def, options.Commands)
		},
	})
}
