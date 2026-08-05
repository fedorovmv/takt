package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"takt/internal/assistant"
	"takt/internal/spec"
	"takt/internal/store"
)

func resolveNodePolicy(node spec.Node, workflowPath string, inherited assistant.Policy) (assistant.Policy, error) {
	local, err := resolvePolicyFields(spec.PolicySpec{
		AllowedTools: node.AllowedTools,
		DeniedTools:  node.DeniedTools,
		Skills:       node.Skills,
		MCP:          node.MCP,
		Sandbox:      node.Sandbox,
		Requires:     node.Requires,
	}, workflowPath)
	if err != nil {
		return assistant.Policy{}, err
	}
	merged, err := mergePolicies(inherited, local)
	if err != nil {
		return assistant.Policy{}, err
	}
	return hydratePolicyResources(merged)
}

func resolvePolicyFields(value spec.PolicySpec, workflowPath string) (assistant.Policy, error) {
	policy := assistant.Policy{
		AllowedTools:     copyStringList(value.AllowedTools),
		DeniedTools:      append([]string(nil), value.DeniedTools...),
		ToolsRestricted:  value.AllowedTools != nil,
		SkillsRestricted: value.Skills != nil,
		Requires:         append([]string(nil), value.Requires...),
	}
	base := filepath.Dir(workflowPath)
	for _, skill := range stringList(value.Skills) {
		resolved := strings.TrimSpace(skill)
		if resolved == "" {
			continue
		}
		candidate := resolved
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(base, candidate)
		}
		if _, err := os.Stat(candidate); err == nil {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return assistant.Policy{}, err
			}
			policy.Skills = append(policy.Skills, filepath.Clean(abs))
		} else {
			policy.Skills = append(policy.Skills, resolved)
		}
	}
	if strings.TrimSpace(value.MCP) != "" {
		path := value.MCP
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return assistant.Policy{}, err
		}
		policy.MCPPath = filepath.Clean(abs)
	}
	if value.Sandbox != nil {
		policy.Filesystem = value.Sandbox.Filesystem
		policy.Network = value.Sandbox.Network
	}
	return hydratePolicyResources(policy)
}

func stringList(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func copyStringList(value *[]string) []string {
	return append([]string(nil), stringList(value)...)
}

func hydratePolicyResources(policy assistant.Policy) (assistant.Policy, error) {
	if policy.MCPPath == "" || len(policy.MCPConfig) > 0 {
		return policy, nil
	}
	raw, err := os.ReadFile(policy.MCPPath)
	if err != nil {
		return assistant.Policy{}, fmt.Errorf("read MCP config %s: %w", policy.MCPPath, err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return assistant.Policy{}, fmt.Errorf("parse MCP config %s: %w", policy.MCPPath, err)
	}
	if _, ok := value.(map[string]any); !ok {
		return assistant.Policy{}, fmt.Errorf("MCP config %s must contain a JSON object", policy.MCPPath)
	}
	policy.MCPConfig = append(json.RawMessage(nil), raw...)
	return policy, nil
}

func mergePolicies(inherited, local assistant.Policy) (assistant.Policy, error) {
	out := local
	out.DeniedTools = unionStrings(inherited.DeniedTools, local.DeniedTools)
	out.Requires = unionStrings(inherited.Requires, local.Requires)
	out.AllowedTools, out.ToolsRestricted = mergeAllowlists(inherited.AllowedTools, inherited.ToolsRestricted, local.AllowedTools, local.ToolsRestricted)
	out.Skills, out.SkillsRestricted = mergeAllowlists(inherited.Skills, inherited.SkillsRestricted, local.Skills, local.SkillsRestricted)
	if inherited.MCPPath != "" {
		if local.MCPPath != "" && filepath.Clean(local.MCPPath) != filepath.Clean(inherited.MCPPath) {
			return assistant.Policy{}, fmt.Errorf("child policy cannot replace inherited MCP config %s with %s", inherited.MCPPath, local.MCPPath)
		}
		if local.MCPPath == "" {
			out.MCPPath = inherited.MCPPath
			out.MCPConfig = append(json.RawMessage(nil), inherited.MCPConfig...)
		}
	}
	if inherited.Filesystem == "read_only" {
		out.Filesystem = "read_only"
	}
	if inherited.Network == "deny" {
		out.Network = "deny"
	}
	return out, nil
}

func mergeAllowlists(parent []string, parentSet bool, child []string, childSet bool) ([]string, bool) {
	if !parentSet && !childSet {
		return nil, false
	}
	if parentSet && !childSet {
		return append([]string(nil), parent...), true
	}
	if !parentSet && childSet {
		return append([]string(nil), child...), true
	}
	allowed := map[string]bool{}
	for _, value := range parent {
		allowed[value] = true
	}
	var out []string
	for _, value := range child {
		if allowed[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, true
}

func unionStrings(groups ...[]string) []string {
	set := map[string]bool{}
	for _, group := range groups {
		for _, value := range group {
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

func validateAdapterPolicy(adapter assistant.Adapter, policy assistant.Policy) ([]string, error) {
	available := adapter.Capabilities()
	set := make(map[string]bool, len(available))
	for _, capability := range available {
		set[capability] = true
	}
	required := assistant.RequiredCapabilities(policy)
	var missing []string
	for _, capability := range required {
		if !set[capability] {
			missing = append(missing, capability)
		}
	}
	if len(missing) > 0 {
		return available, fmt.Errorf("assistant does not support required capabilities: %s", strings.Join(missing, ", "))
	}
	sort.Strings(available)
	return available, nil
}

func policyState(policy assistant.Policy, capabilities []string) *store.NodePolicyState {
	if len(assistant.RequiredCapabilities(policy)) == 0 {
		return nil
	}
	return &store.NodePolicyState{
		AllowedTools:     append([]string(nil), policy.AllowedTools...),
		DeniedTools:      append([]string(nil), policy.DeniedTools...),
		ToolsRestricted:  policy.ToolsRestricted,
		Skills:           append([]string(nil), policy.Skills...),
		SkillsRestricted: policy.SkillsRestricted,
		MCPPath:          policy.MCPPath,
		Filesystem:       policy.Filesystem,
		Network:          policy.Network,
		Requires:         append([]string(nil), policy.Requires...),
		Capabilities:     append([]string(nil), capabilities...),
	}
}

func policyFromState(value *store.NodePolicyState) assistant.Policy {
	if value == nil {
		return assistant.Policy{}
	}
	return assistant.Policy{
		AllowedTools:     append([]string(nil), value.AllowedTools...),
		DeniedTools:      append([]string(nil), value.DeniedTools...),
		ToolsRestricted:  value.ToolsRestricted,
		Skills:           append([]string(nil), value.Skills...),
		SkillsRestricted: value.SkillsRestricted,
		MCPPath:          value.MCPPath,
		Filesystem:       value.Filesystem,
		Network:          value.Network,
		Requires:         append([]string(nil), value.Requires...),
	}
}
