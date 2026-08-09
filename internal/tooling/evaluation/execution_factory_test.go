package evaluation

import (
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/testsupport/runtimefixture"
)

func testExecutionFactory(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) (Execution, error) {
	runner := runtimefixture.New(wf, cfg, workflowPath, configPath, workspace)
	return Execution{Runner: runner, Store: store.FS{Workspace: workspace}}, nil
}
