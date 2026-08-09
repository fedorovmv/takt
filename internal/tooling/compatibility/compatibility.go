package compatibility

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"takt/internal/assistant"
	"takt/internal/domainadapter"
	"takt/internal/schemasubset"
	"takt/internal/spec"
	"takt/internal/version"
	tasksource "takt/sdk/tasksource"
)

const MatrixVersion = "takt-compatibility/v1"

type Matrix struct {
	APIVersion     string                `json:"apiVersion"`
	Kind           string                `json:"kind"`
	TaktVersion    string                `json:"takt_version"`
	SchemaSubset   schemasubset.Contract `json:"schema_subset"`
	Assistants     []AssistantPolicy     `json:"assistants"`
	Hosts          []HostPolicy          `json:"hosts"`
	DomainAdapters []DomainAdapterPolicy `json:"domain_adapters"`
	TaskSources    []TaskSourcePolicy    `json:"task_sources"`
	MCPSurfaces    []MCPSurfacePolicy    `json:"mcp_surfaces"`
}

type AssistantPolicy struct {
	Type                string   `json:"type"`
	Protocol            string   `json:"protocol,omitempty"`
	Support             string   `json:"support"`
	Verification        string   `json:"verification"`
	ContractFixture     string   `json:"contract_fixture,omitempty"`
	Capabilities        []string `json:"capabilities,omitempty"`
	LiveVersionRequired bool     `json:"live_version_required,omitempty"`
	Notes               []string `json:"notes,omitempty"`
}

type HostPolicy struct {
	Host             string   `json:"host"`
	Integration      string   `json:"integration"`
	TargetContract   string   `json:"target_contract"`
	Enforcement      string   `json:"enforcement"`
	LiveVerified     bool     `json:"live_verified"`
	StrictAllowed    bool     `json:"strict_allowed"`
	Capabilities     []string `json:"capabilities,omitempty"`
	MissingForStrict []string `json:"missing_for_strict,omitempty"`
	Notes            []string `json:"notes,omitempty"`
}

type DomainAdapterPolicy struct {
	Transport    string   `json:"transport"`
	Protocol     string   `json:"protocol"`
	Support      string   `json:"support"`
	Verification string   `json:"verification"`
	Domains      []string `json:"domains"`
	Notes        []string `json:"notes,omitempty"`
}

type TaskSourcePolicy struct {
	Transport    string   `json:"transport"`
	Protocol     string   `json:"protocol"`
	Support      string   `json:"support"`
	Verification string   `json:"verification"`
	Notes        []string `json:"notes,omitempty"`
}

type MCPSurfacePolicy struct {
	Surface string `json:"surface"`
	Support string `json:"support"`
	Notes   string `json:"notes,omitempty"`
}

func CurrentMatrix() Matrix {
	return Matrix{
		APIVersion:   MatrixVersion,
		Kind:         "CompatibilityMatrix",
		TaktVersion:  version.Value,
		SchemaSubset: schemasubset.Description(),
		Assistants: []AssistantPolicy{
			{Type: "process", Protocol: assistant.ProtocolV1Alpha2, Support: "supported-alpha", Verification: "public-conformance-kit+reference-wrapper", Capabilities: []string{"declared-by-wrapper"}, Notes: []string{"New external wrappers should target v1alpha2.", "Bundled reference: cmd/qwen-takt-adapter, implemented only through sdk/agentadapter.", "Protocol version alone does not imply tool_control; configured and stream-declared capabilities are checked separately.", "OS exit-code consistency is enforced by the process host, not transcript-only conformance."}},
			{Type: "process", Protocol: assistant.ProtocolV1Alpha1, Support: "deprecated", Verification: "legacy-contract-tests", Notes: []string{"Readable for compatibility; do not use for new wrappers."}},
			{Type: "pi", Support: "supported-alpha", Verification: "built-in-contract-fixture", ContractFixture: "0.83.0", Capabilities: []string{assistant.CapabilitySandboxFilesystem, assistant.CapabilitySkills, assistant.CapabilityToolPolicy}, LiveVersionRequired: true, Notes: []string{"Fixture version is not a claim of live Pi compatibility; pin and smoke the deployed CLI."}},
			{Type: "opencode", Support: "supported-alpha", Verification: "built-in-contract-fixture", ContractFixture: "1.2.3-test", Capabilities: []string{assistant.CapabilityMCP, assistant.CapabilitySandboxFilesystem, assistant.CapabilitySkills, assistant.CapabilityToolPolicy}, LiveVersionRequired: true, Notes: []string{"Synthetic fixture proves parser/transport semantics only; pin and smoke the deployed OpenCode build."}},
			{Type: "mock", Support: "internal", Verification: "test-only", Notes: []string{"Not a production assistant contract."}},
		},
		Hosts: []HostPolicy{
			{Host: "pi", Integration: "integrations/coding-agent-host-control/pi", TargetContract: "0.73.1", Enforcement: "guarded", LiveVerified: false, StrictAllowed: false, Capabilities: []string{"command_interception", "input_interception", "tool_call_blocking", "session_recovery"}, MissingForStrict: []string{"completion_blocking"}, Notes: []string{"Bundled extension is intentionally guarded until live conformance on the exact deployed Pi version."}},
			{Host: "opencode", Integration: "integrations/coding-agent-host-control/opencode", TargetContract: "V2 beta", Enforcement: "guarded", LiveVerified: false, StrictAllowed: false, Capabilities: []string{"command_interception", "input_interception", "tool_call_blocking", "session_recovery"}, MissingForStrict: []string{"verified_completion_blocking"}, Notes: []string{"Context-abort/model-dispatch behavior requires live verification on the pinned OpenCode build."}},
		},
		DomainAdapters: []DomainAdapterPolicy{
			{Transport: "process", Protocol: domainadapter.ProtocolV1Alpha1, Support: "supported-alpha", Verification: "public-sdk+reference-scm-e2e", Domains: []string{domainadapter.DomainSCM, domainadapter.DomainTracker, domainadapter.DomainCI}, Notes: []string{"Bundled reference: cmd/takt-github-scm-adapter for the neutral SCM contract.", "Corporate providers should implement sdk/domainadapter without changing workflow definitions.", "Invoke/reconcile receive the execution workspace so adapters can resolve the correct repository/worktree."}},
			{Transport: "mcp", Protocol: "MCP stdio", Support: "supported-alpha", Verification: "fake-e2e", Domains: []string{domainadapter.DomainSCM, domainadapter.DomainTracker, domainadapter.DomainCI}, Notes: []string{"Operation mapping remains adapter configuration; workflows use only neutral operations."}},
		},
		TaskSources: []TaskSourcePolicy{
			{Transport: "process", Protocol: tasksource.ProtocolV1Alpha1, Support: "supported-alpha", Verification: "public-sdk+github-issue-reference-e2e", Notes: []string{"Task source resolution happens before Router/Dynamic Takt and is distinct from domain operations inside workflows."}},
		},
		MCPSurfaces: []MCPSurfacePolicy{
			{Surface: "agent", Support: "experimental", Notes: "Five public takt.task.* operations remain available as the compact Dynamic Flow coding-agent surface; they do not define stable core compatibility."},
			{Surface: "host", Support: "experimental", Notes: "Host-control integration surface; subject to live host conformance and allowed to evolve independently of stable core."},
			{Surface: "worker", Support: "supported-alpha", Notes: "External executor/tool lifecycle surface."},
			{Surface: "operator", Support: "supported-alpha", Notes: "Advanced local operations; not the default coding-agent surface."},
		},
	}
}

type CheckOptions struct {
	Workspace string
	Live      bool
	Providers assistant.Registry
}

type CheckReport struct {
	APIVersion  string           `json:"apiVersion"`
	Kind        string           `json:"kind"`
	TaktVersion string           `json:"takt_version"`
	Status      string           `json:"status"`
	Schema      SchemaCheck      `json:"schema"`
	Assistants  []ComponentCheck `json:"assistants,omitempty"`
	Adapters    []ComponentCheck `json:"adapters,omitempty"`
	TaskSources []ComponentCheck `json:"task_sources,omitempty"`
	Hosts       []HostPolicy     `json:"bundled_hosts,omitempty"`
	Problems    []string         `json:"problems,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type SchemaCheck struct {
	Version  string `json:"version"`
	Status   string `json:"status"`
	Contract string `json:"contract"`
}

type ComponentCheck struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Type         string   `json:"type"`
	Protocol     string   `json:"protocol,omitempty"`
	Support      string   `json:"support"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities,omitempty"`
	Version      string   `json:"version,omitempty"`
	Problems     []string `json:"problems,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

func Check(ctx context.Context, cfg *spec.Config, options CheckOptions) CheckReport {
	matrix := CurrentMatrix()
	report := CheckReport{
		APIVersion: MatrixVersion, Kind: "CompatibilityCheck", TaktVersion: version.Value,
		Status: "ready", Schema: SchemaCheck{Version: schemasubset.Version, Status: "ready", Contract: "workflow input.schema and node output_format"},
		Hosts: append([]HostPolicy(nil), matrix.Hosts...),
	}
	assistantPolicies := map[string]AssistantPolicy{}
	for _, value := range matrix.Assistants {
		key := value.Type + "|" + value.Protocol
		assistantPolicies[key] = value
		if value.Protocol == "" {
			assistantPolicies[value.Type+"|"] = value
		}
	}
	assistantNames := make([]string, 0, len(cfg.Assistants))
	for name := range cfg.Assistants {
		assistantNames = append(assistantNames, name)
	}
	sort.Strings(assistantNames)
	for _, name := range assistantNames {
		value := cfg.Assistants[name]
		protocol := value.Protocol
		if value.Type == "process" && protocol == "" {
			protocol = "raw-stdout"
		}
		policy, ok := assistantPolicies[value.Type+"|"+value.Protocol]
		if !ok {
			policy, ok = assistantPolicies[value.Type+"|"]
		}
		check := ComponentCheck{Name: name, Kind: "assistant", Type: value.Type, Protocol: protocol, Support: "unknown", Status: "ready"}
		if ok {
			check.Support = policy.Support
			check.Capabilities = append([]string(nil), policy.Capabilities...)
		}
		resolved, err := (assistant.Factory{Config: cfg, Providers: options.Providers}).Resolve(name)
		if err != nil {
			check.Problems = append(check.Problems, err.Error())
		} else {
			check.Capabilities = resolved.Capabilities()
		}
		if value.Type == "process" && value.Protocol == "" {
			check.Warnings = append(check.Warnings, "raw process stdout mode has no normalized public assistant protocol; prefer takt-assistant/v1alpha2 for new integrations")
			check.Support = "legacy"
		}
		if check.Support == "deprecated" {
			check.Warnings = append(check.Warnings, "configured component uses a deprecated compatibility contract")
		}
		if options.Live {
			if registration, exists := options.Providers.Registration(value.Type); exists && registration.ProbeVersion != nil {
				versionText, probeErr := registration.ProbeVersion(ctx, value, options.Workspace)
				if probeErr != nil {
					check.Problems = append(check.Problems, "version probe: "+probeErr.Error())
				} else {
					check.Version = strings.TrimSpace(versionText)
					check.Warnings = append(check.Warnings, "binary version detected; live agent/host conformance is still required before claiming strict compatibility")
				}
			}
		}
		finalizeComponent(&check)
		report.Assistants = append(report.Assistants, check)
	}
	adapterNames := make([]string, 0, len(cfg.Adapters))
	for name := range cfg.Adapters {
		adapterNames = append(adapterNames, name)
	}
	sort.Strings(adapterNames)
	for _, name := range adapterNames {
		value := cfg.Adapters[name]
		check := ComponentCheck{Name: name, Kind: "domain_adapter", Type: value.Transport, Protocol: domainAdapterProtocol(value.Transport), Support: "supported-alpha", Status: "ready"}
		if options.Live {
			adapter, err := (domainadapter.Factory{Config: cfg}).Resolve(name)
			if err != nil {
				check.Problems = append(check.Problems, err.Error())
			} else if declaration, describeErr := adapter.Describe(ctx); describeErr != nil {
				check.Problems = append(check.Problems, "describe: "+describeErr.Error())
			} else if validateErr := domainadapter.ValidateDeclaration(declaration); validateErr != nil {
				check.Problems = append(check.Problems, "declaration: "+validateErr.Error())
			} else {
				check.Capabilities = append([]string(nil), declaration.Capabilities...)
				sort.Strings(check.Capabilities)
				for operation := range value.Operations {
					if !contains(declaration.Capabilities, operation) {
						check.Problems = append(check.Problems, "configured operation not declared: "+operation)
					}
				}
				for operation := range value.ReconcileOperations {
					if !contains(declaration.Reconcile, operation) {
						check.Problems = append(check.Problems, "configured reconcile operation not declared: "+operation)
					}
				}
			}
		}
		finalizeComponent(&check)
		report.Adapters = append(report.Adapters, check)
	}
	sourceNames := make([]string, 0, len(cfg.TaskSources))
	for name := range cfg.TaskSources {
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)
	for _, name := range sourceNames {
		value := cfg.TaskSources[name]
		check := ComponentCheck{Name: name, Kind: "task_source", Type: value.Transport, Protocol: tasksource.ProtocolV1Alpha1, Support: "supported-alpha", Status: "ready"}
		if len(value.Argv) == 0 {
			check.Problems = append(check.Problems, "argv is required")
		}
		finalizeComponent(&check)
		report.TaskSources = append(report.TaskSources, check)
	}
	for _, component := range append(append(append([]ComponentCheck{}, report.Assistants...), report.Adapters...), report.TaskSources...) {
		for _, problem := range component.Problems {
			report.Problems = append(report.Problems, fmt.Sprintf("%s %s: %s", component.Kind, component.Name, problem))
		}
		for _, warning := range component.Warnings {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s %s: %s", component.Kind, component.Name, warning))
		}
	}
	if len(report.Problems) > 0 {
		report.Status = "error"
	} else if len(report.Warnings) > 0 {
		report.Status = "warning"
	}
	return report
}

func finalizeComponent(check *ComponentCheck) {
	sort.Strings(check.Capabilities)
	sort.Strings(check.Problems)
	sort.Strings(check.Warnings)
	if len(check.Problems) > 0 {
		check.Status = "error"
	} else if len(check.Warnings) > 0 {
		check.Status = "warning"
	}
}

func domainAdapterProtocol(transport string) string {
	if transport == "process" {
		return domainadapter.ProtocolV1Alpha1
	}
	if transport == "mcp" {
		return "MCP stdio"
	}
	return ""
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
