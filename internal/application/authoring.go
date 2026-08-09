package application

import (
	"fmt"

	"takt/internal/profile"

	"takt/internal/authoring"
	cfgpkg "takt/internal/config"
	"takt/internal/runtime"
	"takt/internal/workflow"
)

// ValidationResult is the transport-neutral result of validating an authored workflow.
type ValidationResult struct {
	Valid       bool                   `json:"valid"`
	Workflow    string                 `json:"workflow"`
	Diagnostics []authoring.Diagnostic `json:"diagnostics,omitempty"`
}

// AuthoringService owns validation semantics for workflow definitions. Transports
// should not assemble a Runner just to reproduce reference/capability checks.
type AuthoringService struct {
	workspace     string
	runnerFactory RunnerFactory
}

func (s *AuthoringService) ValidateWorkflow(selector, configOverride string, warningsAsErrors bool) (*ValidationResult, error) {
	wfPath, cfgPath, _, err := resolveWorkflow(s.workspace, "", selector, configOverride)
	if err != nil {
		return nil, err
	}
	wf, err := workflow.Load(wfPath)
	if err != nil {
		return nil, err
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	runner := s.runnerFactory(runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: wfPath, ConfigPath: cfgPath, ControlWorkspace: s.workspace}, RunnerOptions{})
	if err := workflow.ValidateReferences(wf, cfg, runner.CommandResolver()); err != nil {
		return nil, err
	}
	if err := runtime.ValidateCapabilities(wf, cfg, wfPath, runner.CommandResolver(), runner.AssistantResolver()); err != nil {
		return nil, fmt.Errorf("capability validation: %w", err)
	}
	diagnostics := authoring.Analyze(wf, runner.CommandResolver())
	if warningsAsErrors {
		for index := range diagnostics {
			if diagnostics[index].Severity == "warning" {
				diagnostics[index].Severity = "error"
				diagnostics[index].Code = "warning_as_error." + diagnostics[index].Code
			}
		}
	}
	if authoring.HasErrors(diagnostics) {
		return nil, &authoring.Error{Diagnostics: diagnostics}
	}
	return &ValidationResult{Valid: true, Workflow: wf.Metadata.Name, Diagnostics: diagnostics}, nil
}

func (s *AuthoringService) InitProfile(name string, force bool) (string, error) {
	return profile.Init(name, s.workspace, force)
}
