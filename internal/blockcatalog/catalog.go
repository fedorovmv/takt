package blockcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"takt/internal/command"
	"takt/internal/definition"
	"takt/internal/spec"
	"takt/internal/workflow"
	"takt/internal/yamlmini"
)

const (
	APIVersion = "takt/v1alpha1"
	Kind       = "BlockPackage"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

type Metadata struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
}

type Limits struct {
	MaxChildRuns  int `json:"max_child_runs,omitempty"`
	MaxParallel   int `json:"max_parallel,omitempty"`
	MaxIterations int `json:"max_iterations,omitempty"`
	MaxTokens     int `json:"max_tokens,omitempty"`
}

type BranchRules struct {
	Prefix           string `json:"prefix,omitempty"`
	Pattern          string `json:"pattern,omitempty"`
	RequireCleanBase bool   `json:"require_clean_base,omitempty"`
}

type Governance struct {
	RequiredBlocks        []string        `json:"required_blocks,omitempty"`
	RequiredChecks        []string        `json:"required_checks,omitempty"`
	AllowedIntegrations   *[]string       `json:"allowed_integrations,omitempty"`
	BranchRules           BranchRules     `json:"branch_rules,omitempty"`
	ChangeRequestTemplate string          `json:"change_request_template,omitempty"`
	Policy                spec.PolicySpec `json:"policy,omitempty"`
	Limits                Limits          `json:"limits,omitempty"`
}

type Block struct {
	Workflow       string          `json:"workflow"`
	Description    string          `json:"description,omitempty"`
	Capabilities   []string        `json:"capabilities,omitempty"`
	Integrations   []string        `json:"integrations,omitempty"`
	OutputPaths    []string        `json:"output_paths,omitempty"`
	RequiredChecks []string        `json:"required_checks,omitempty"`
	Policy         spec.PolicySpec `json:"policy,omitempty"`
}

type Package struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   Metadata          `json:"metadata"`
	Blocks     map[string]Block  `json:"blocks"`
	Templates  map[string]string `json:"templates,omitempty"`
	Governance Governance        `json:"governance,omitempty"`
}

type ResolvedBlock struct {
	Name           string            `json:"name"`
	Package        string            `json:"package"`
	PackageScope   string            `json:"package_scope"`
	WorkflowPath   string            `json:"workflow_path"`
	Description    string            `json:"description,omitempty"`
	Capabilities   []string          `json:"capabilities,omitempty"`
	Integrations   []string          `json:"integrations,omitempty"`
	OutputPaths    []string          `json:"output_paths,omitempty"`
	OutputTypes    map[string]string `json:"output_types,omitempty"`
	RequiredChecks []string          `json:"required_checks,omitempty"`
	Policy         spec.PolicySpec   `json:"policy,omitempty"`
}

type PackageSummary struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Scope       string `json:"scope"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

type Catalog struct {
	Packages    []PackageSummary         `json:"packages"`
	Blocks      map[string]ResolvedBlock `json:"blocks"`
	Templates   map[string]string        `json:"templates,omitempty"`
	Governance  Governance               `json:"governance,omitempty"`
	Fingerprint string                   `json:"fingerprint"`
}

func Load(paths []string) (*Catalog, error) {
	catalog := &Catalog{Blocks: map[string]ResolvedBlock{}, Templates: map[string]string{}}
	hash := sha256.New()
	for _, path := range paths {
		resolved, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("read block package %s: %w", resolved, err)
		}
		var pkg Package
		if err := yamlmini.Unmarshal(raw, &pkg); err != nil {
			return nil, fmt.Errorf("parse block package %s: %w", resolved, err)
		}
		if err := validatePackage(pkg, resolved); err != nil {
			return nil, err
		}
		_, _ = hash.Write([]byte(resolved))
		_, _ = hash.Write(raw)
		catalog.Packages = append(catalog.Packages, PackageSummary{Name: pkg.Metadata.Name, Version: pkg.Metadata.Version, Scope: pkg.Metadata.Scope, Description: pkg.Metadata.Description, Path: resolved})
		base := filepath.Dir(resolved)
		names := make([]string, 0, len(pkg.Blocks))
		for name := range pkg.Blocks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			block := pkg.Blocks[name]
			if _, exists := catalog.Blocks[name]; exists {
				return nil, fmt.Errorf("block %q is declared by more than one trusted package", name)
			}
			workflowPath, err := securePackagePath(base, block.Workflow)
			if err != nil {
				return nil, fmt.Errorf("block package %s block %s: %w", pkg.Metadata.Name, name, err)
			}
			wf, err := workflow.Load(workflowPath)
			if err != nil {
				return nil, fmt.Errorf("validate block workflow %s: %w", workflowPath, err)
			}
			if err := validateAtomicBlockWorkflow(wf); err != nil {
				return nil, fmt.Errorf("block package %s block %s: %w", pkg.Metadata.Name, name, err)
			}
			outputTypes := map[string]string{}
			for _, outputPath := range block.OutputPaths {
				outputType, pathErr := workflowOutputType(wf, outputPath)
				if pathErr != nil {
					return nil, fmt.Errorf("block package %s block %s output_path %s: %w", pkg.Metadata.Name, name, outputPath, pathErr)
				}
				outputTypes[outputPath] = outputType
			}
			closure, err := definition.ContentClosureFingerprint(wf, workflowPath, blockCommandResolver(workflowPath))
			if err != nil {
				return nil, fmt.Errorf("fingerprint block package %s block %s: %w", pkg.Metadata.Name, name, err)
			}
			_, _ = hash.Write([]byte(name))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write([]byte(closure))
			policy := mergePolicy(pkg.Governance.Policy, block.Policy)
			catalog.Blocks[name] = ResolvedBlock{
				Name: name, Package: pkg.Metadata.Name, PackageScope: pkg.Metadata.Scope,
				WorkflowPath: workflowPath, Description: block.Description,
				Capabilities: unique(block.Capabilities), Integrations: unique(block.Integrations),
				OutputPaths: unique(block.OutputPaths), OutputTypes: outputTypes, RequiredChecks: unique(append(append([]string{}, pkg.Governance.RequiredChecks...), block.RequiredChecks...)), Policy: policy,
			}
		}
		for name, value := range pkg.Templates {
			key := pkg.Metadata.Name + ":" + name
			if _, exists := catalog.Templates[key]; exists {
				return nil, fmt.Errorf("duplicate template %q", key)
			}
			catalog.Templates[key] = value
		}
		if err := mergeGovernance(&catalog.Governance, pkg.Governance); err != nil {
			return nil, fmt.Errorf("merge governance from package %s: %w", pkg.Metadata.Name, err)
		}
	}
	if len(catalog.Blocks) == 0 {
		return nil, fmt.Errorf("trusted block catalog is empty")
	}
	for _, required := range catalog.Governance.RequiredBlocks {
		if _, ok := catalog.Blocks[required]; !ok {
			return nil, fmt.Errorf("trusted packages require unknown block %q", required)
		}
	}
	for name, block := range catalog.Blocks {
		block.Policy = mergePolicy(catalog.Governance.Policy, block.Policy)
		block.RequiredChecks = unique(append(block.RequiredChecks, catalog.Governance.RequiredChecks...))
		catalog.Blocks[name] = block
	}
	if catalog.Governance.AllowedIntegrations != nil {
		allowed := set(*catalog.Governance.AllowedIntegrations)
		for name, block := range catalog.Blocks {
			for _, integration := range block.Integrations {
				if !allowed[integration] {
					return nil, fmt.Errorf("trusted block %q requires integration %q forbidden by merged package governance", name, integration)
				}
			}
		}
	}
	catalog.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	return catalog, nil
}

func LoadOne(path string) (*Catalog, error) { return Load([]string{path}) }

func validatePackage(pkg Package, path string) error {
	if pkg.APIVersion != APIVersion || pkg.Kind != Kind {
		return fmt.Errorf("block package %s must use apiVersion %s and kind %s", path, APIVersion, Kind)
	}
	if !namePattern.MatchString(pkg.Metadata.Name) {
		return fmt.Errorf("block package %s has invalid metadata.name %q", path, pkg.Metadata.Name)
	}
	if strings.TrimSpace(pkg.Metadata.Version) == "" {
		return fmt.Errorf("block package %s metadata.version is required", path)
	}
	if pkg.Metadata.Scope != "builtin" && pkg.Metadata.Scope != "corporate" && pkg.Metadata.Scope != "project" {
		return fmt.Errorf("block package %s metadata.scope must be builtin, corporate, or project", path)
	}
	if len(pkg.Blocks) == 0 {
		return fmt.Errorf("block package %s must declare blocks", path)
	}
	var allowedIntegrations map[string]bool
	if pkg.Governance.AllowedIntegrations != nil {
		allowedIntegrations = set(*pkg.Governance.AllowedIntegrations)
	}
	for name, block := range pkg.Blocks {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("block package %s has invalid block name %q", path, name)
		}
		if strings.TrimSpace(block.Workflow) == "" {
			return fmt.Errorf("block package %s block %s requires workflow", path, name)
		}
		for _, integration := range block.Integrations {
			if allowedIntegrations != nil && !allowedIntegrations[integration] {
				return fmt.Errorf("block package %s block %s uses integration %q outside governance.allowed_integrations", path, name, integration)
			}
		}
		if len(block.OutputPaths) == 0 {
			return fmt.Errorf("block package %s block %s requires at least one output_path", path, name)
		}
		for _, outputPath := range block.OutputPaths {
			if !validOutputPath(outputPath) {
				return fmt.Errorf("block package %s block %s has invalid output_path %q", path, name, outputPath)
			}
		}
	}
	if err := validateLimits(pkg.Governance.Limits); err != nil {
		return fmt.Errorf("block package %s: %w", path, err)
	}
	return nil
}

func validateLimits(l Limits) error {
	if l.MaxChildRuns < 0 || l.MaxParallel < 0 || l.MaxIterations < 0 || l.MaxTokens < 0 {
		return fmt.Errorf("governance limits cannot be negative")
	}
	if l.MaxChildRuns > 256 || l.MaxParallel > 64 || l.MaxIterations > 16 || l.MaxTokens > 100000000 {
		return fmt.Errorf("governance limits exceed Takt hard maximums")
	}
	return nil
}

func securePackagePath(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("workflow path must be relative to its package")
	}
	joined := filepath.Clean(filepath.Join(base, rel))
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	relToBase, err := filepath.Rel(baseAbs, joinedAbs)
	if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workflow path %q escapes package directory", rel)
	}
	return joinedAbs, nil
}

func validOutputPath(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if !namePattern.MatchString(strings.ReplaceAll(part, "_", "-")) {
			return false
		}
	}
	return true
}

func (c *Catalog) Block(name string) (ResolvedBlock, bool) {
	if c == nil {
		return ResolvedBlock{}, false
	}
	value, ok := c.Blocks[name]
	return value, ok
}

func (c *Catalog) ValidateBudget(maxChildRuns, maxParallel, maxIterations, maxTokens int) error {
	if c == nil {
		return nil
	}
	limits := c.Governance.Limits
	checks := []struct {
		name string
		got  int
		max  int
	}{
		{"max_child_runs", maxChildRuns, limits.MaxChildRuns},
		{"max_parallel", maxParallel, limits.MaxParallel},
		{"max_iterations", maxIterations, limits.MaxIterations},
		{"max_tokens", maxTokens, limits.MaxTokens},
	}
	for _, check := range checks {
		if check.max > 0 && check.got > check.max {
			return fmt.Errorf("budget.%s %d exceeds trusted package limit %d", check.name, check.got, check.max)
		}
	}
	return nil
}

func (c *Catalog) ValidateRequiredBlocks(uses []string) error {
	if c == nil {
		return nil
	}
	present := set(uses)
	for _, required := range c.Governance.RequiredBlocks {
		if !present[required] {
			return fmt.Errorf("trusted packages require block %q", required)
		}
	}
	return nil
}

func (c *Catalog) PlannerView() map[string]any {
	blocks := make([]map[string]any, 0, len(c.Blocks))
	names := make([]string, 0, len(c.Blocks))
	for name := range c.Blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		block := c.Blocks[name]
		blocks = append(blocks, map[string]any{
			"name": name, "package": block.Package, "scope": block.PackageScope,
			"description": block.Description, "capabilities": block.Capabilities,
			"integrations": block.Integrations, "output_paths": block.OutputPaths, "output_types": block.OutputTypes,
			"required_checks": block.RequiredChecks,
		})
	}
	return map[string]any{"packages": c.Packages, "blocks": blocks, "templates": c.Templates, "governance": c.Governance, "fingerprint": c.Fingerprint}
}

func (c *Catalog) GovernanceJSON() string {
	if c == nil {
		return "{}"
	}
	raw, _ := json.Marshal(map[string]any{"packages": c.Packages, "templates": c.Templates, "governance": c.Governance})
	return string(raw)
}

func (c *Catalog) RequiredCapabilities(uses []string) []string {
	var values []string
	for _, use := range uses {
		if block, ok := c.Block(use); ok {
			values = append(values, block.Capabilities...)
			for _, integration := range block.Integrations {
				values = append(values, "integration:"+integration)
			}
		}
	}
	return unique(values)
}

func (c *Catalog) OutputPathType(blockName, path string) (string, bool) {
	block, ok := c.Block(blockName)
	if !ok {
		return "", false
	}
	value, ok := block.OutputTypes[path]
	return value, ok
}

func validateAtomicBlockWorkflow(wf *spec.Workflow) error {
	var visit func([]spec.Node) error
	visit = func(nodes []spec.Node) error {
		for _, node := range nodes {
			if node.WorkflowRun != nil {
				return fmt.Errorf("workflow node %q starts governed child Runs; trusted blocks must remain atomic and compose child Runs only through WorkflowPlan phases", node.ID)
			}
			if node.LoopGroup != nil {
				if err := visit(node.LoopGroup.Nodes); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(wf.Nodes)
}

func workflowOutputType(wf *spec.Workflow, path string) (string, error) {
	terminal, err := terminalOutputNode(wf)
	if err != nil {
		return "", err
	}
	if terminal.OutputFormat == nil {
		return "", fmt.Errorf("terminal node %q has no output_format", terminal.ID)
	}
	current := *terminal.OutputFormat
	for _, part := range strings.Split(path, ".") {
		if current.Type != "object" {
			return "", fmt.Errorf("path enters %q through non-object type %q", part, current.Type)
		}
		next, ok := current.Properties[part]
		if !ok {
			return "", fmt.Errorf("path component %q is not declared by terminal node %q", part, terminal.ID)
		}
		current = next
	}
	return current.Type, nil
}

func terminalOutputNode(wf *spec.Workflow) (spec.Node, error) {
	if wf == nil || len(wf.Nodes) == 0 {
		return spec.Node{}, fmt.Errorf("block workflow is empty")
	}
	depended := map[string]bool{}
	for _, node := range wf.Nodes {
		for _, dep := range node.DependsOn {
			depended[dep] = true
		}
	}
	var terminals []spec.Node
	for _, node := range wf.Nodes {
		if !node.Hidden && node.PublicParent == "" && !depended[node.ID] {
			terminals = append(terminals, node)
		}
	}
	if len(terminals) != 1 {
		return spec.Node{}, fmt.Errorf("block workflow must have exactly one terminal node, got %d", len(terminals))
	}
	return terminals[0], nil
}

func mergeGovernance(target *Governance, source Governance) error {
	target.RequiredBlocks = unique(append(target.RequiredBlocks, source.RequiredBlocks...))
	target.RequiredChecks = unique(append(target.RequiredChecks, source.RequiredChecks...))
	target.AllowedIntegrations = intersectOptionalRestrictions(target.AllowedIntegrations, source.AllowedIntegrations)
	if source.BranchRules != (BranchRules{}) {
		if target.BranchRules != (BranchRules{}) && target.BranchRules != source.BranchRules {
			return fmt.Errorf("conflicting branch_rules")
		}
		target.BranchRules = source.BranchRules
	}
	if source.ChangeRequestTemplate != "" {
		if target.ChangeRequestTemplate != "" && target.ChangeRequestTemplate != source.ChangeRequestTemplate {
			return fmt.Errorf("conflicting change_request_template values %q and %q", target.ChangeRequestTemplate, source.ChangeRequestTemplate)
		}
		target.ChangeRequestTemplate = source.ChangeRequestTemplate
	}
	target.Policy = mergePolicy(target.Policy, source.Policy)
	target.Limits = minLimits(target.Limits, source.Limits)
	return nil
}

func minLimits(a, b Limits) Limits {
	return Limits{
		MaxChildRuns: minPositive(a.MaxChildRuns, b.MaxChildRuns), MaxParallel: minPositive(a.MaxParallel, b.MaxParallel),
		MaxIterations: minPositive(a.MaxIterations, b.MaxIterations), MaxTokens: minPositive(a.MaxTokens, b.MaxTokens),
	}
}

func minPositive(a, b int) int {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func mergePolicy(a, b spec.PolicySpec) spec.PolicySpec {
	out := a
	out.AllowedTools = intersectPointerRestrictions(a.AllowedTools, b.AllowedTools)
	out.DeniedTools = unique(append(out.DeniedTools, b.DeniedTools...))
	out.Skills = intersectPointerRestrictions(a.Skills, b.Skills)
	if b.MCP != "" {
		if out.MCP == "" || out.MCP == b.MCP {
			out.MCP = b.MCP
		} else {
			// Conflicting MCP configurations cannot be combined safely. Requiring
			// both as capabilities leaves execution fail-closed during preflight.
			out.Requires = append(out.Requires, "mcp_conflict")
		}
	}
	out.Sandbox = restrictiveSandbox(out.Sandbox, b.Sandbox)
	out.Requires = unique(append(out.Requires, b.Requires...))
	return out
}

func intersectPointerRestrictions(a, b *[]string) *[]string {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		values := unique(append([]string(nil), (*b)...))
		return &values
	}
	if b == nil {
		values := unique(append([]string(nil), (*a)...))
		return &values
	}
	values := intersection(*a, *b)
	return &values
}

func restrictiveSandbox(a, b *spec.SandboxSpec) *spec.SandboxSpec {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := *a
	out.Filesystem = stricterValue(a.Filesystem, b.Filesystem, map[string]int{"": 0, "workspace": 1, "read_only": 2, "none": 3})
	out.Network = stricterValue(a.Network, b.Network, map[string]int{"": 0, "allowed": 1, "none": 2})
	return &out
}

func stricterValue(a, b string, rank map[string]int) string {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func intersectOptionalRestrictions(a, b *[]string) *[]string {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		values := unique(append([]string(nil), (*b)...))
		return &values
	}
	if b == nil {
		values := unique(append([]string(nil), (*a)...))
		return &values
	}
	values := intersection(*a, *b)
	return &values
}

func blockCommandResolver(workflowPath string) command.Resolver {
	seen := map[string]bool{}
	var dirs []string
	for dir := filepath.Dir(workflowPath); ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "commands")
		if !seen[candidate] {
			dirs = append(dirs, candidate)
			seen[candidate] = true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return command.Resolver{Dirs: dirs}
}

func intersectRestrictions(a, b []string) []string {
	if len(a) == 0 {
		return unique(b)
	}
	if len(b) == 0 {
		return unique(a)
	}
	return intersection(a, b)
}

func intersection(a, b []string) []string {
	allowed := set(b)
	var out []string
	for _, value := range a {
		if allowed[value] {
			out = append(out, value)
		}
	}
	return unique(out)
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func set(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}
