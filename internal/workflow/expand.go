package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"takt/internal/command"
	"takt/internal/spec"
	"takt/internal/yamlcodec"
)

const maxExpansionDepth = 16

var inputVarRE = regexp.MustCompile(`\$INPUTS\.([A-Za-z_][A-Za-z0-9_-]*)`)

type compiledGroup struct {
	nodes     []spec.Node
	entries   []string
	terminals []string
	public    map[string]string
	outputID  string
}

type compiler struct {
	stack   []string
	rootDir string
}

// Expand compiles subworkflow and foreach containers into ordinary DAG nodes.
// The runtime therefore keeps one scheduler, one Run state and one persistence
// model for both top-level and reusable workflows.
func Expand(path string, wf *spec.Workflow) (*spec.Workflow, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}
	normalizeWorkflowAliases(wf)
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c := &compiler{stack: []string{abs}, rootDir: filepath.Dir(abs)}
	group, err := c.compileNodes(wf.Nodes, abs, "", wf.Defaults, spec.HookSet{}, nil, false)
	if err != nil {
		return nil, err
	}
	out := *wf
	out.Nodes = group.nodes
	return &out, nil
}

func (c *compiler) compileNodes(nodes []spec.Node, workflowPath, prefix string, defaults spec.Defaults, inheritedHooks spec.HookSet, vars map[string]string, inlineLocalCommands bool) (compiledGroup, error) {
	if len(nodes) == 0 {
		return compiledGroup{}, fmt.Errorf("nodes must not be empty")
	}
	byID := make(map[string]spec.Node, len(nodes))
	dependents := make(map[string]int, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			return compiledGroup{}, fmt.Errorf("workflow %s contains node without id", workflowPath)
		}
		if _, exists := byID[node.ID]; exists {
			return compiledGroup{}, fmt.Errorf("duplicate node id %q in %s", node.ID, workflowPath)
		}
		byID[node.ID] = node
	}
	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			if _, ok := byID[dep]; !ok {
				return compiledGroup{}, fmt.Errorf("node %q depends on unknown node %q in %s", node.ID, dep, workflowPath)
			}
			dependents[dep]++
		}
	}

	compiled := make([]spec.Node, 0, len(nodes))
	public := make(map[string]string, len(nodes))
	entriesBySource := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		result, err := c.compileNode(node, workflowPath, prefix, defaults, inheritedHooks, vars, inlineLocalCommands, byID)
		if err != nil {
			return compiledGroup{}, err
		}
		compiled = append(compiled, result.nodes...)
		public[node.ID] = result.public[node.ID]
		entriesBySource[node.ID] = result.entries
	}

	var entries, terminals []string
	for _, node := range nodes {
		if len(node.DependsOn) == 0 {
			entries = append(entries, entriesBySource[node.ID]...)
		}
		if dependents[node.ID] == 0 {
			terminals = append(terminals, public[node.ID])
		}
	}
	if len(terminals) == 0 {
		return compiledGroup{}, fmt.Errorf("workflow %s has no terminal node", workflowPath)
	}
	outputID := ""
	if len(terminals) == 1 {
		outputID = terminals[0]
	}
	return compiledGroup{nodes: compiled, entries: entries, terminals: terminals, public: public, outputID: outputID}, nil
}

func (c *compiler) compileNode(node spec.Node, workflowPath, prefix string, defaults spec.Defaults, inheritedHooks spec.HookSet, vars map[string]string, inlineLocalCommands bool, siblings map[string]spec.Node) (compiledGroup, error) {
	publicID := qualify(prefix, node.ID)
	kinds := sourceKinds(node)
	if kinds != 1 {
		return compiledGroup{}, fmt.Errorf("node %q must define exactly one action (command, prompt, bash, script, adapter, approval, loop_group, subworkflow, foreach, or workflow)", node.ID)
	}

	switch {
	case node.Subworkflow != nil:
		if err := validateContainerFields(node); err != nil {
			return compiledGroup{}, err
		}
		return c.compileSubworkflow(node, workflowPath, prefix, defaults, vars, siblings)
	case node.Foreach != nil:
		if err := validateContainerFields(node); err != nil {
			return compiledGroup{}, err
		}
		return c.compileForeach(node, workflowPath, prefix, defaults, vars, siblings)
	default:
		clone, err := cloneNode(node)
		if err != nil {
			return compiledGroup{}, err
		}
		clone.ID = publicID
		clone.DependsOn = qualifyDependencies(prefix, node.DependsOn)
		clone.Hooks = mergeHookSets(inheritedHooks, clone.Hooks)
		if clone.LoopGroup != nil {
			applyDefaults(&clone, defaults)
		}
		if err := c.rewriteNode(&clone, prefix, siblings, vars, workflowPath, inlineLocalCommands); err != nil {
			return compiledGroup{}, err
		}
		applyDefaults(&clone, defaults)
		return compiledGroup{nodes: []spec.Node{clone}, entries: []string{publicID}, terminals: []string{publicID}, public: map[string]string{node.ID: publicID}, outputID: publicID}, nil
	}
}

func (c *compiler) compileSubworkflow(node spec.Node, workflowPath, prefix string, parentDefaults spec.Defaults, vars map[string]string, siblings map[string]spec.Node) (compiledGroup, error) {
	publicID := qualify(prefix, node.ID)
	gateID := publicID + "__start"

	childPath, child, err := c.loadChild(workflowPath, node.Subworkflow.Path)
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	gateInternal := &spec.InternalNodeSpec{Mode: "noop"}
	if child.Worktree.Enabled {
		worktree := child.Worktree
		gateInternal = &spec.InternalNodeSpec{Mode: "worktree", WorkflowName: child.Metadata.Name, Worktree: &worktree}
	}
	gate := spec.Node{ID: gateID, Hidden: true, PublicParent: publicID, DependsOn: qualifyDependencies(prefix, node.DependsOn), When: rewriteWhenNodeRefs(replaceVars(node.When, vars), prefix, siblings), TriggerRule: node.TriggerRule, Internal: gateInternal}
	childVars, err := resolveInputs(node.Subworkflow.Inputs, vars)
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	for key, value := range childVars {
		childVars[key] = rewriteTemplateNodeRefs(value, prefix, siblings)
	}
	if err := c.enter(childPath); err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	childGroup, err := c.compileNodes(child.Nodes, childPath, publicID+"__", containerDefaults(parentDefaults, child.Defaults, node), child.Hooks, childVars, true)
	c.leave()
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	addDependency(childGroup.nodes, childGroup.entries, gateID)
	markNodeGuards(childGroup.nodes, gateID)
	markExpandedNodes(childGroup.nodes, publicID)
	outputID, err := chooseOutput(node.Subworkflow.OutputNode, childGroup, childPath)
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	aggregator := spec.Node{ID: publicID, Guard: gateID, DependsOn: append([]string{}, childGroup.terminals...), Internal: &spec.InternalNodeSpec{Mode: "result", ResultFrom: outputID}}
	all := make([]spec.Node, 0, len(childGroup.nodes)+2)
	all = append(all, gate)
	all = append(all, childGroup.nodes...)
	all = append(all, aggregator)
	return compiledGroup{nodes: all, entries: []string{gateID}, terminals: []string{publicID}, public: map[string]string{node.ID: publicID}, outputID: publicID}, nil
}

func (c *compiler) compileForeach(node spec.Node, workflowPath, prefix string, parentDefaults spec.Defaults, vars map[string]string, siblings map[string]spec.Node) (compiledGroup, error) {
	items, definitionHash, err := c.resolveForeachItems(node, workflowPath)
	if err != nil {
		return compiledGroup{}, err
	}
	if strings.TrimSpace(node.Foreach.Subworkflow.Path) == "" {
		return compiledGroup{}, fmt.Errorf("foreach node %q requires subworkflow.path", node.ID)
	}
	as := strings.TrimSpace(node.Foreach.As)
	if as == "" {
		as = "item"
	}
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`).MatchString(as) {
		return compiledGroup{}, fmt.Errorf("foreach node %q has invalid as value %q", node.ID, as)
	}

	publicID := qualify(prefix, node.ID)
	gateID := publicID + "__start"
	gate := spec.Node{
		ID: gateID, Hidden: true, PublicParent: publicID,
		DependsOn: qualifyDependencies(prefix, node.DependsOn),
		When:      rewriteWhenNodeRefs(replaceVars(node.When, vars), prefix, siblings), TriggerRule: node.TriggerRule,
		Internal: &spec.InternalNodeSpec{Mode: "noop", DefinitionHash: definitionHash},
	}
	all := []spec.Node{gate}
	previous := gateID
	iterationResults := make([]string, 0, len(items))

	for index, item := range items {
		itemVars, err := foreachVars(as, index, item)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		merged := mergeVars(vars, itemVars)
		childPath, child, err := c.loadChild(workflowPath, node.Foreach.Subworkflow.Path)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q: %w", node.ID, err)
		}
		if child.Worktree.Enabled {
			return compiledGroup{}, fmt.Errorf("foreach node %q child workflow %q enables worktree isolation; per-item worktrees require governed child Runs", node.ID, child.Metadata.Name)
		}
		childInputs, err := resolveInputs(node.Foreach.Subworkflow.Inputs, merged)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		for key, value := range childInputs {
			childInputs[key] = rewriteTemplateNodeRefs(value, prefix, siblings)
		}
		iterationID := publicID + "__" + fmt.Sprintf("%03d", index+1)
		if err := c.enter(childPath); err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		childGroup, err := c.compileNodes(child.Nodes, childPath, iterationID+"__", containerDefaults(parentDefaults, child.Defaults, node), child.Hooks, childInputs, true)
		c.leave()
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		dependency := previous
		if node.Foreach.Parallel {
			dependency = gateID
		}
		addDependency(childGroup.nodes, childGroup.entries, dependency)
		markNodeGuards(childGroup.nodes, gateID)
		markExpandedNodes(childGroup.nodes, publicID)
		outputID, err := chooseOutput(node.Foreach.Subworkflow.OutputNode, childGroup, childPath)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		iterationResult := spec.Node{
			ID: iterationID, Hidden: true, PublicParent: publicID, Guard: gateID,
			DependsOn: append([]string{}, childGroup.terminals...),
			Internal:  &spec.InternalNodeSpec{Mode: "result", ResultFrom: outputID},
		}
		all = append(all, childGroup.nodes...)
		all = append(all, iterationResult)
		if !node.Foreach.Parallel {
			previous = iterationID
		}
		iterationResults = append(iterationResults, iterationID)
	}
	aggregator := spec.Node{
		ID:        publicID,
		Guard:     gateID,
		DependsOn: append([]string{}, iterationResults...),
		Internal:  &spec.InternalNodeSpec{Mode: "collect", ResultsFrom: append([]string{}, iterationResults...)},
	}
	all = append(all, aggregator)
	return compiledGroup{nodes: all, entries: []string{gateID}, terminals: []string{publicID}, public: map[string]string{node.ID: publicID}, outputID: publicID}, nil
}

func (c *compiler) resolveForeachItems(node spec.Node, workflowPath string) ([]any, string, error) {
	hasInline := len(node.Foreach.Items) > 0
	hasSource := node.Foreach.ItemsFrom != nil
	if hasInline == hasSource {
		return nil, "", fmt.Errorf("foreach node %q requires exactly one of items or items_from", node.ID)
	}
	if hasInline {
		return node.Foreach.Items, "", nil
	}
	rel := strings.TrimSpace(node.Foreach.ItemsFrom.Path)
	if rel == "" {
		return nil, "", fmt.Errorf("foreach node %q items_from.path is required", node.ID)
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(workflowPath), path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", fmt.Errorf("foreach node %q read items_from %s: %w", node.ID, abs, err)
	}
	var items []any
	if err := yamlcodec.Unmarshal(b, &items); err != nil {
		return nil, "", fmt.Errorf("foreach node %q parse items_from %s: %w", node.ID, abs, err)
	}
	if len(items) == 0 {
		return nil, "", fmt.Errorf("foreach node %q items_from %s must contain at least one item", node.ID, abs)
	}
	hash := sha256.Sum256(b)
	return items, fmt.Sprintf("%x", hash[:]), nil
}

func (c *compiler) loadChild(parentPath, rel string) (string, *spec.Workflow, error) {
	if strings.TrimSpace(rel) == "" {
		return "", nil, fmt.Errorf("path is required")
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(parentPath), path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", abs, err)
	}
	var wf spec.Workflow
	if err := yamlcodec.Unmarshal(b, &wf); err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", abs, err)
	}
	normalizeWorkflowAliases(&wf)
	if strings.TrimSpace(wf.Name) == "" {
		return "", nil, fmt.Errorf("%s is not a valid target Workflow: name is required", abs)
	}
	return abs, &wf, nil
}

func normalizeWorkflowAliases(wf *spec.Workflow) {
	if wf == nil {
		return
	}
	if wf.Name == "" {
		wf.Name = wf.Metadata.Name
	}
	if wf.Description == "" {
		wf.Description = wf.Metadata.Description
	}
	if wf.Labels == nil && wf.Metadata.Labels != nil {
		wf.Labels = wf.Metadata.Labels
	}
	if wf.Provider == "" {
		wf.Provider = wf.Defaults.Assistant
	}
	if wf.Model == "" {
		wf.Model = wf.Defaults.Model
	}
	wf.Metadata = spec.Metadata{Name: wf.Name, Description: wf.Description, Labels: wf.Labels}
	wf.Defaults = spec.Defaults{Assistant: wf.Provider, Model: wf.Model, Session: "fresh"}
	for i := range wf.Nodes {
		normalizeNodeAliases(&wf.Nodes[i])
	}
}

func normalizeNodeAliases(node *spec.Node) {
	if node == nil {
		return
	}
	if node.Assistant == "" {
		node.Assistant = node.Provider
	}
	if node.Provider == "" {
		node.Provider = node.Assistant
	}
	if node.Session == "" && node.Context != "" {
		node.Session = node.Context
	}
	if node.Context == "" && node.Session != "" {
		node.Context = node.Session
	}
	if node.LoopGroup != nil {
		normalizeLoopPredicate(node.LoopGroup)
		for i := range node.LoopGroup.Nodes {
			normalizeNodeAliases(&node.LoopGroup.Nodes[i])
		}
	}
	if node.Loop != nil {
		loop := node.Loop
		body := spec.Node{ID: node.ID, Prompt: loop.Prompt, Command: loop.Command, Provider: node.Provider, Model: node.Model, Context: node.Context}
		normalizeNodeAliases(&body)
		node.LoopGroup = &spec.LoopGroupSpec{
			MaxIterations: loop.MaxIterations,
			Nodes:         []spec.Node{body},
			Until:         spec.UntilSpec{Node: body.ID, Signal: loop.Until},
			UntilBash:     loop.UntilBash,
			FreshContext:  loop.FreshContext,
		}
		node.Loop = nil
	}
}

func normalizeLoopPredicate(loop *spec.LoopGroupSpec) {
	if loop == nil || loop.Until.Node != "" || !loop.Until.Scalar {
		return
	}
	terminals := map[string]bool{}
	for _, child := range loop.Nodes {
		terminals[child.ID] = true
	}
	for _, child := range loop.Nodes {
		for _, dep := range child.DependsOn {
			delete(terminals, dep)
		}
	}
	if len(terminals) == 1 {
		for id := range terminals {
			loop.Until.Node = id
		}
	}
}

func (c *compiler) enter(path string) error {
	for _, active := range c.stack {
		if active == path {
			chain := append(append([]string{}, c.stack...), path)
			return fmt.Errorf("recursive subworkflow reference: %s", strings.Join(chain, " -> "))
		}
	}
	if len(c.stack) >= maxExpansionDepth {
		return fmt.Errorf("subworkflow expansion exceeds depth %d", maxExpansionDepth)
	}
	c.stack = append(c.stack, path)
	return nil
}

func (c *compiler) leave() {
	c.stack = c.stack[:len(c.stack)-1]
}

func (c *compiler) rewriteNode(node *spec.Node, prefix string, siblings map[string]spec.Node, vars map[string]string, workflowPath string, inlineLocalCommands bool) error {
	rewriteTemplate := func(value string) string {
		return rewriteTemplateNodeRefs(replaceVars(value, vars), prefix, siblings)
	}
	node.When = rewriteWhenNodeRefs(replaceVars(node.When, vars), prefix, siblings)
	node.Prompt = rewriteTemplate(node.Prompt)
	node.Bash = rewriteTemplate(node.Bash)
	if node.Script != nil {
		node.Script.Inline = rewriteTemplate(node.Script.Inline)
		node.Script.Path = rebaseDefinitionPath(rewriteTemplate(node.Script.Path), workflowPath)
		node.Script.WorkingDir = rewriteTemplate(node.Script.WorkingDir)
		for index := range node.Script.Dependencies {
			node.Script.Dependencies[index] = rebaseDefinitionPath(rewriteTemplate(node.Script.Dependencies[index]), workflowPath)
		}
		for index := range node.Script.Args {
			node.Script.Args[index] = rewriteTemplate(node.Script.Args[index])
		}
		for key, value := range node.Script.Env {
			node.Script.Env[key] = rewriteTemplate(value)
		}
	}
	rebaseNodePolicyPaths(node, workflowPath)
	node.OutputPath = rewriteTemplate(node.OutputPath)
	if node.Adapter != nil {
		node.Adapter.Input = rewriteTemplate(node.Adapter.Input)
	}
	if node.SideEffect != nil {
		node.SideEffect.IdempotencyKey = rewriteTemplate(node.SideEffect.IdempotencyKey)
	}
	if node.Approval != nil {
		node.Approval.Message = rewriteTemplate(node.Approval.Message)
	}
	if node.WorkflowRun != nil {
		node.WorkflowRun.Input = rewriteTemplate(node.WorkflowRun.Input)
		if !filepath.IsAbs(node.WorkflowRun.Path) {
			node.WorkflowRun.Path = filepath.Clean(filepath.Join(filepath.Dir(workflowPath), node.WorkflowRun.Path))
		}
	}
	rewriteHooks(&node.Hooks, rewriteTemplate)

	if node.LoopGroup != nil {
		if err := c.rewriteLoopGroup(node, vars, workflowPath, inlineLocalCommands); err != nil {
			return err
		}
	}
	if inlineLocalCommands && node.Command != "" {
		local, b, found, err := c.readLocalCommand(workflowPath, node.Command)
		if err != nil {
			return err
		}
		if found {
			cmd, err := command.Parse(node.Command, local, string(b))
			if err != nil {
				return err
			}
			node.Prompt = rewriteTemplate(cmd.Body)
			node.Command = ""
			if node.Assistant == "" {
				node.Assistant = cmd.Assistant
			}
			if node.Model == "" {
				node.Model = cmd.Model
			}
		}
	}
	if unresolved := unresolvedInput(node); unresolved != "" {
		return fmt.Errorf("node %q contains unresolved subworkflow input %s", node.ID, unresolved)
	}
	return nil
}

func rebaseDefinitionPath(value, workflowPath string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "${") {
		return value
	}
	return filepath.Clean(filepath.Join(filepath.Dir(workflowPath), value))
}

func rebaseNodePolicyPaths(node *spec.Node, workflowPath string) {
	if strings.TrimSpace(node.MCP) != "" {
		node.MCP = rebaseDefinitionPath(node.MCP, workflowPath)
	}
	rebaseSkillPaths(node.Skills, workflowPath)
	if node.WorkflowRun != nil && node.WorkflowRun.Policy != nil {
		if strings.TrimSpace(node.WorkflowRun.Policy.MCP) != "" {
			node.WorkflowRun.Policy.MCP = rebaseDefinitionPath(node.WorkflowRun.Policy.MCP, workflowPath)
		}
		rebaseSkillPaths(node.WorkflowRun.Policy.Skills, workflowPath)
	}
}

func rebaseSkillPaths(values *[]string, workflowPath string) {
	if values == nil {
		return
	}
	for index, value := range *values {
		if strings.TrimSpace(value) == "" || filepath.IsAbs(value) || strings.Contains(value, "${") {
			continue
		}
		candidate := filepath.Join(filepath.Dir(workflowPath), value)
		if _, err := os.Stat(candidate); err == nil {
			(*values)[index] = filepath.Clean(candidate)
		}
	}
}

func unresolvedInput(node *spec.Node) string {
	values := []string{node.Prompt, node.Bash, node.When, node.OutputPath}
	if node.Adapter != nil {
		values = append(values, node.Adapter.Input)
	}
	if node.SideEffect != nil {
		values = append(values, node.SideEffect.IdempotencyKey)
	}
	if node.Script != nil {
		values = append(values, node.Script.Path, node.Script.Inline, node.Script.WorkingDir)
		values = append(values, node.Script.Args...)
		for _, value := range node.Script.Env {
			values = append(values, value)
		}
	}
	if node.WorkflowRun != nil {
		values = append(values, node.WorkflowRun.Input)
	}
	if node.Approval != nil {
		values = append(values, node.Approval.Message)
	}
	for _, group := range [][]spec.HookSpec{node.Hooks.BeforeNode, node.Hooks.AfterNode, node.Hooks.BeforeComplete, node.Hooks.OnFailure} {
		for _, hook := range group {
			values = append(values, hook.Bash)
		}
	}
	if node.LoopGroup != nil {
		values = append(values, node.LoopGroup.Until.OutputContains)
		for i := range node.LoopGroup.Nodes {
			if value := unresolvedInput(&node.LoopGroup.Nodes[i]); value != "" {
				return value
			}
		}
	}
	for _, value := range values {
		if token := inputVarRE.FindString(value); token != "" {
			return token
		}
	}
	return ""
}

func (c *compiler) rewriteLoopGroup(node *spec.Node, vars map[string]string, workflowPath string, inlineLocalCommands bool) error {
	loop := node.LoopGroup
	originalUntil := loop.Until.Node
	childPrefix := node.ID + "__"
	defaults := spec.Defaults{Assistant: node.Assistant, Model: node.Model, Session: node.Session}
	group, err := c.compileNodes(loop.Nodes, workflowPath, childPrefix, defaults, spec.HookSet{}, vars, inlineLocalCommands)
	if err != nil {
		return fmt.Errorf("loop_group %q: %w", node.ID, err)
	}
	untilID, ok := group.public[originalUntil]
	if !ok {
		return fmt.Errorf("loop_group %q until.node %q does not exist", node.ID, originalUntil)
	}
	for i := range group.nodes {
		group.nodes[i].PublicParent = node.ID
	}
	loop.Nodes = group.nodes
	loop.Until.Node = untilID
	loop.Until.OutputContains = replaceVars(loop.Until.OutputContains, vars)
	return nil
}

func (c *compiler) readLocalCommand(workflowPath, name string) (string, []byte, bool, error) {
	dir := filepath.Dir(workflowPath)
	for {
		path := filepath.Join(dir, "commands", name+".md")
		b, err := os.ReadFile(path)
		if err == nil {
			return path, b, true, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, false, err
		}
		if dir == c.rootDir {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !isWithin(parent, c.rootDir) {
			break
		}
		dir = parent
	}
	return "", nil, false, nil
}

func isWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func markNodeGuards(nodes []spec.Node, guard string) {
	for index := range nodes {
		if nodes[index].Guard == "" {
			nodes[index].Guard = guard
		}
	}
}

func markExpandedNodes(nodes []spec.Node, publicParent string) {
	for i := range nodes {
		nodes[i].Hidden = true
		nodes[i].PublicParent = publicParent
	}
}

func sourceKinds(node spec.Node) int {
	count := 0
	if node.Command != "" {
		count++
	}
	if node.Prompt != "" {
		count++
	}
	if node.Bash != "" {
		count++
	}
	if node.Script != nil {
		count++
	}
	if node.Approval != nil {
		count++
	}
	if node.Cancel != "" {
		count++
	}
	if node.LoopGroup != nil {
		count++
	}
	if node.Subworkflow != nil {
		count++
	}
	if node.Foreach != nil {
		count++
	}
	if node.WorkflowRun != nil {
		count++
	}
	if node.Internal != nil {
		count++
	}
	if node.Adapter != nil {
		count++
	}
	return count
}

func validateContainerFields(node spec.Node) error {
	if node.Attempts.Max != 0 || len(node.Attempts.RetryOn) != 0 || node.Attempts.RetrySession != "" || node.AllowFailure || node.Timeout != "" || !hookSetEmpty(node.Hooks) || len(node.NativeHooks) != 0 || node.OutputFormat != nil || node.OutputType != "" || node.OutputMIME != "" || node.OutputPath != "" || node.AllowedTools != nil || len(node.DeniedTools) != 0 || node.Skills != nil || node.MCP != "" || node.Sandbox != nil || len(node.Requires) != 0 {
		return fmt.Errorf("container node %q supports assistant/model/session defaults, but attempts, timeout, hooks, native_hooks, policies, output contracts and allow_failure must be defined inside the child workflow", node.ID)
	}
	return nil
}

func containerDefaults(parent, child spec.Defaults, node spec.Node) spec.Defaults {
	out := child
	if out.Assistant == "" {
		out.Assistant = parent.Assistant
	}
	if out.Model == "" {
		out.Model = parent.Model
	}
	if out.Session == "" {
		out.Session = parent.Session
	}
	if node.Assistant != "" {
		out.Assistant = node.Assistant
	}
	if node.Provider != "" {
		out.Assistant = node.Provider
	}
	if node.Model != "" {
		out.Model = node.Model
	}
	if node.Context != "" {
		out.Session = node.Context
	}
	if node.Session != "" {
		out.Session = node.Session
	}
	return out
}

func chooseOutput(name string, group compiledGroup, path string) (string, error) {
	if strings.TrimSpace(name) != "" {
		id, ok := group.public[name]
		if !ok {
			return "", fmt.Errorf("output_node %q does not exist in %s", name, path)
		}
		return id, nil
	}
	if group.outputID == "" {
		var names []string
		for source, public := range group.public {
			for _, terminal := range group.terminals {
				if public == terminal {
					names = append(names, source)
				}
			}
		}
		sort.Strings(names)
		return "", fmt.Errorf("subworkflow %s has multiple terminal nodes (%s); set output_node", path, strings.Join(names, ", "))
	}
	return group.outputID, nil
}

func addDependency(nodes []spec.Node, ids []string, dependency string) {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	for i := range nodes {
		if _, ok := set[nodes[i].ID]; ok {
			nodes[i].DependsOn = appendUnique(nodes[i].DependsOn, dependency)
		}
	}
}

func applyDefaults(node *spec.Node, defaults spec.Defaults) {
	if node.Provider == "" {
		node.Provider = defaults.Assistant
	}
	if node.Assistant == "" {
		node.Assistant = defaults.Assistant
	}
	if node.Model == "" {
		node.Model = defaults.Model
	}
	if node.Session == "" {
		if node.Context != "" {
			node.Session = node.Context
		} else {
			node.Session = defaults.Session
		}
	}
	if node.Context == "" {
		node.Context = node.Session
	}
}

func rewriteTemplateNodeRefs(value, prefix string, siblings map[string]spec.Node) string {
	if value == "" || prefix == "" {
		return value
	}
	for id := range siblings {
		value = strings.ReplaceAll(value, "$"+id+".", "$"+qualify(prefix, id)+".")
		value = strings.ReplaceAll(value, "$LOOP_PREV."+id+".", "$LOOP_PREV."+qualify(prefix, id)+".")
	}
	return value
}

func rewriteWhenNodeRefs(value, prefix string, siblings map[string]spec.Node) string {
	value = rewriteTemplateNodeRefs(value, prefix, siblings)
	if value == "" || prefix == "" {
		return value
	}
	for id := range siblings {
		value = strings.ReplaceAll(value, "$"+id+".", "$"+qualify(prefix, id)+".")
	}
	return value
}

func resolveInputs(inputs map[string]string, vars map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(inputs))
	for key, value := range inputs {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("input name must not be empty")
		}
		out["INPUTS."+key] = replaceVars(value, vars)
	}
	return out, nil
}

func replaceVars(value string, vars map[string]string) string {
	if value == "" || len(vars) == 0 {
		return value
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		value = strings.ReplaceAll(value, "${"+key+"}", vars[key])
		value = strings.ReplaceAll(value, "$"+key, vars[key])
	}
	return value
}

func foreachVars(as string, index int, item any) (map[string]string, error) {
	vars := map[string]string{"INPUTS.index": strconv.Itoa(index), "INPUTS." + as + ".index": strconv.Itoa(index)}
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	switch value := item.(type) {
	case string:
		vars["INPUTS."+as] = value
	case float64:
		vars["INPUTS."+as] = strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		vars["INPUTS."+as] = strconv.FormatBool(value)
	case nil:
		vars["INPUTS."+as] = "null"
	case map[string]any:
		vars["INPUTS."+as] = string(encoded)
		for key, field := range value {
			fieldJSON, err := json.Marshal(field)
			if err != nil {
				return nil, err
			}
			if text, ok := field.(string); ok {
				vars["INPUTS."+as+"."+key] = text
			} else {
				vars["INPUTS."+as+"."+key] = string(fieldJSON)
			}
		}
	default:
		vars["INPUTS."+as] = string(encoded)
	}
	return vars, nil
}

func mergeVars(left, right map[string]string) map[string]string {
	out := make(map[string]string, len(left)+len(right))
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		out[k] = v
	}
	return out
}

func qualify(prefix, id string) string {
	if prefix == "" {
		return id
	}
	return prefix + id
}

func qualifyDependencies(prefix string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, qualify(prefix, value))
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func mergeHookSets(global, local spec.HookSet) spec.HookSet {
	return spec.HookSet{
		BeforeNode:     append(append([]spec.HookSpec{}, global.BeforeNode...), local.BeforeNode...),
		AfterNode:      append(append([]spec.HookSpec{}, global.AfterNode...), local.AfterNode...),
		BeforeComplete: append(append([]spec.HookSpec{}, global.BeforeComplete...), local.BeforeComplete...),
		OnFailure:      append(append([]spec.HookSpec{}, global.OnFailure...), local.OnFailure...),
	}
}

func rewriteHooks(hooks *spec.HookSet, rewrite func(string) string) {
	groups := [][]spec.HookSpec{hooks.BeforeNode, hooks.AfterNode, hooks.BeforeComplete, hooks.OnFailure}
	for _, group := range groups {
		for i := range group {
			group[i].Bash = rewrite(group[i].Bash)
		}
	}
}

func hookSetEmpty(h spec.HookSet) bool {
	return len(h.BeforeNode) == 0 && len(h.AfterNode) == 0 && len(h.BeforeComplete) == 0 && len(h.OnFailure) == 0
}

func cloneNode(node spec.Node) (spec.Node, error) {
	b, err := json.Marshal(node)
	if err != nil {
		return spec.Node{}, err
	}
	var out spec.Node
	if err := json.Unmarshal(b, &out); err != nil {
		return spec.Node{}, err
	}
	return out, nil
}
