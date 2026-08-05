package assistant

import (
	"encoding/json"
	"sort"
)

const (
	CapabilityToolPolicy        = "tool_policy"
	CapabilitySkills            = "skills"
	CapabilityMCP               = "mcp"
	CapabilitySandboxFilesystem = "sandbox_filesystem"
	CapabilitySandboxNetwork    = "sandbox_network"
)

type Policy struct {
	AllowedTools     []string        `json:"allowed_tools,omitempty"`
	DeniedTools      []string        `json:"denied_tools,omitempty"`
	ToolsRestricted  bool            `json:"tools_restricted,omitempty"`
	Skills           []string        `json:"skills,omitempty"`
	SkillsRestricted bool            `json:"skills_restricted,omitempty"`
	MCPPath          string          `json:"mcp_path,omitempty"`
	MCPConfig        json.RawMessage `json:"mcp_config,omitempty"`
	Filesystem       string          `json:"filesystem,omitempty"`
	Network          string          `json:"network,omitempty"`
	Requires         []string        `json:"requires,omitempty"`
}

func RequiredCapabilities(policy Policy) []string {
	set := map[string]bool{}
	if policy.ToolsRestricted || len(policy.AllowedTools) > 0 || len(policy.DeniedTools) > 0 {
		set[CapabilityToolPolicy] = true
	}
	if policy.SkillsRestricted || len(policy.Skills) > 0 {
		set[CapabilitySkills] = true
	}
	if len(policy.MCPConfig) > 0 || policy.MCPPath != "" {
		set[CapabilityMCP] = true
	}
	if policy.Filesystem != "" {
		set[CapabilitySandboxFilesystem] = true
	}
	if policy.Network != "" {
		set[CapabilitySandboxNetwork] = true
	}
	for _, value := range policy.Requires {
		if value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeCapabilities(base, extra []string) []string {
	set := map[string]bool{}
	for _, values := range [][]string{base, extra} {
		for _, value := range values {
			if value != "" {
				set[value] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
