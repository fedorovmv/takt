package workflow

import (
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
	"takt/internal/yamlmini"
)

const maxExpansionDepth = 16

var inputVarRE = regexp.MustCompile(`\$\{inputs\.([A-Za-z_][A-Za-z0-9_-]*)\}`)

type compiledGroup struct {
	nodes     []spec.Node
	entries   []string
	terminals []string
	public    map[string]string
	outputID  string
}

type compiler struct {
	stack []string
}

// Expand compiles subworkflow and foreach containers into ordinary DAG nodes.
// The runtime therefore keeps one scheduler, one Run state and one persistence
// model for both top-level and reusable workflows.
func Expand(path string, wf *spec.Workflow) (*spec.Workflow, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c := &compiler{stack: []string{abs}}
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
		return compiledGroup{}, fmt.Errorf("node %q must define exactly one of command, prompt, bash, approval, loop_group, subworkflow, foreach", node.ID)
	}

	switch {
	case node.Subworkflow != nil:
		if err := validateContainerFields(node); err != nil {
			return compiledGroup{}, err
		}
		return c.compileSubworkflow(node, workflowPath, prefix, vars, siblings)
	case node.Foreach != nil:
		if err := validateContainerFields(node); err != nil {
			return compiledGroup{}, err
		}
		return c.compileForeach(node, workflowPath, prefix, vars, siblings)
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
		if err := rewriteNode(&clone, prefix, siblings, vars, workflowPath, inlineLocalCommands); err != nil {
			return compiledGroup{}, err
		}
		applyDefaults(&clone, defaults)
		return compiledGroup{nodes: []spec.Node{clone}, entries: []string{publicID}, terminals: []string{publicID}, public: map[string]string{node.ID: publicID}, outputID: publicID}, nil
	}
}

func (c *compiler) compileSubworkflow(node spec.Node, workflowPath, prefix string, vars map[string]string, siblings map[string]spec.Node) (compiledGroup, error) {
	publicID := qualify(prefix, node.ID)
	gateID := publicID + "__start"
	gate := spec.Node{ID: gateID, DependsOn: qualifyDependencies(prefix, node.DependsOn), When: rewriteNodeRefs(replaceVars(node.When, vars), prefix, siblings), TriggerRule: node.TriggerRule, Internal: &spec.InternalNodeSpec{Mode: "noop"}}

	childPath, child, err := c.loadChild(workflowPath, node.Subworkflow.Path)
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	childVars, err := resolveInputs(node.Subworkflow.Inputs, vars)
	for key, value := range childVars {
		childVars[key] = rewriteNodeRefs(value, prefix, siblings)
	}
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	if err := c.enter(childPath); err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	childGroup, err := c.compileNodes(child.Nodes, childPath, publicID+"__", child.Defaults, child.Hooks, childVars, true)
	c.leave()
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	addDependency(childGroup.nodes, childGroup.entries, gateID)
	outputID, err := chooseOutput(node.Subworkflow.OutputNode, childGroup, childPath)
	if err != nil {
		return compiledGroup{}, fmt.Errorf("subworkflow node %q: %w", node.ID, err)
	}
	aggregator := spec.Node{ID: publicID, DependsOn: append([]string{}, childGroup.terminals...), Internal: &spec.InternalNodeSpec{Mode: "result", ResultFrom: outputID}}
	all := make([]spec.Node, 0, len(childGroup.nodes)+2)
	all = append(all, gate)
	all = append(all, childGroup.nodes...)
	all = append(all, aggregator)
	return compiledGroup{nodes: all, entries: []string{gateID}, terminals: []string{publicID}, public: map[string]string{node.ID: publicID}, outputID: publicID}, nil
}

func (c *compiler) compileForeach(node spec.Node, workflowPath, prefix string, vars map[string]string, siblings map[string]spec.Node) (compiledGroup, error) {
	if len(node.Foreach.Items) == 0 {
		return compiledGroup{}, fmt.Errorf("foreach node %q requires at least one item", node.ID)
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
	gate := spec.Node{ID: gateID, DependsOn: qualifyDependencies(prefix, node.DependsOn), When: rewriteNodeRefs(replaceVars(node.When, vars), prefix, siblings), TriggerRule: node.TriggerRule, Internal: &spec.InternalNodeSpec{Mode: "noop"}}
	all := []spec.Node{gate}
	previous := gateID
	lastResult := ""

	for index, item := range node.Foreach.Items {
		itemVars, err := foreachVars(as, index, item)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		merged := mergeVars(vars, itemVars)
		childPath, child, err := c.loadChild(workflowPath, node.Foreach.Subworkflow.Path)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q: %w", node.ID, err)
		}
		childInputs, err := resolveInputs(node.Foreach.Subworkflow.Inputs, merged)
		for key, value := range childInputs {
			childInputs[key] = rewriteNodeRefs(value, prefix, siblings)
		}
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		iterationID := publicID + "__" + fmt.Sprintf("%03d", index+1)
		if err := c.enter(childPath); err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		childGroup, err := c.compileNodes(child.Nodes, childPath, iterationID+"__", child.Defaults, child.Hooks, childInputs, true)
		c.leave()
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		addDependency(childGroup.nodes, childGroup.entries, previous)
		outputID, err := chooseOutput(node.Foreach.Subworkflow.OutputNode, childGroup, childPath)
		if err != nil {
			return compiledGroup{}, fmt.Errorf("foreach node %q item %d: %w", node.ID, index, err)
		}
		iterationResult := spec.Node{ID: iterationID, DependsOn: append([]string{}, childGroup.terminals...), Internal: &spec.InternalNodeSpec{Mode: "result", ResultFrom: outputID}}
		all = append(all, childGroup.nodes...)
		all = append(all, iterationResult)
		previous = iterationID
		lastResult = iterationID
	}
	aggregator := spec.Node{ID: publicID, DependsOn: []string{previous}, Internal: &spec.InternalNodeSpec{Mode: "result", ResultFrom: lastResult}}
	all = append(all, aggregator)
	return compiledGroup{nodes: all, entries: []string{gateID}, terminals: []string{publicID}, public: map[string]string{node.ID: publicID}, outputID: publicID}, nil
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
	if err := yamlmini.Unmarshal(b, &wf); err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", abs, err)
	}
	if wf.APIVersion != "takt/v1alpha1" || wf.Kind != "Workflow" || strings.TrimSpace(wf.Metadata.Name) == "" {
		return "", nil, fmt.Errorf("%s is not a valid takt/v1alpha1 Workflow", abs)
	}
	return abs, &wf, nil
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

func rewriteNode(node *spec.Node, prefix string, siblings map[string]spec.Node, vars map[string]string, workflowPath string, inlineLocalCommands bool) error {
	rewrite := func(value string) string {
		return rewriteNodeRefs(replaceVars(value, vars), prefix, siblings)
	}
	node.When = rewrite(node.When)
	node.Prompt = rewrite(node.Prompt)
	node.Bash = rewrite(node.Bash)
	if node.Approval != nil {
		node.Approval.Message = rewrite(node.Approval.Message)
	}
	rewriteHooks(&node.Hooks, rewrite)

	if node.LoopGroup != nil {
		if err := rewriteLoopGroup(node, prefix, siblings, vars, workflowPath, inlineLocalCommands); err != nil {
			return err
		}
	}
	if inlineLocalCommands && node.Command != "" {
		local := filepath.Join(filepath.Dir(workflowPath), "commands", node.Command+".md")
		if b, err := os.ReadFile(local); err == nil {
			cmd, err := command.Parse(node.Command, local, string(b))
			if err != nil {
				return err
			}
			node.Prompt = rewrite(cmd.Body)
			node.Command = ""
			if node.Assistant == "" {
				node.Assistant = cmd.Assistant
			}
			if node.Model == "" {
				node.Model = cmd.Model
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if unresolved := unresolvedInput(node); unresolved != "" {
		return fmt.Errorf("node %q contains unresolved subworkflow input %s", node.ID, unresolved)
	}
	return nil
}

func unresolvedInput(node *spec.Node) string {
	values := []string{node.Prompt, node.Bash, node.When}
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

func rewriteLoopGroup(node *spec.Node, prefix string, siblings map[string]spec.Node, vars map[string]string, workflowPath string, inlineLocalCommands bool) error {
	loop := node.LoopGroup
	childPrefix := node.ID + "__"
	childSiblings := make(map[string]spec.Node, len(loop.Nodes))
	for _, child := range loop.Nodes {
		if child.LoopGroup != nil || child.Subworkflow != nil || child.Foreach != nil || child.Approval != nil {
			return fmt.Errorf("loop_group %q supports only command, prompt, or bash children in this release", node.ID)
		}
		childSiblings[child.ID] = child
	}
	for i := range loop.Nodes {
		child := &loop.Nodes[i]
		child.ID = qualify(childPrefix, child.ID)
		child.DependsOn = qualifyDependencies(childPrefix, child.DependsOn)
		applyDefaults(child, spec.Defaults{Assistant: node.Assistant, Model: node.Model, Session: node.Session})
		if err := rewriteNode(child, childPrefix, childSiblings, vars, workflowPath, inlineLocalCommands); err != nil {
			return err
		}
	}
	loop.Until.Node = qualify(childPrefix, loop.Until.Node)
	loop.Until.OutputContains = replaceVars(loop.Until.OutputContains, vars)
	_ = prefix
	_ = siblings
	return nil
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
	if node.Approval != nil {
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
	if node.Internal != nil {
		count++
	}
	return count
}

func validateContainerFields(node spec.Node) error {
	if node.Assistant != "" || node.Model != "" || node.Session != "" || node.Attempts.Max != 0 || node.AllowFailure || node.Timeout != "" || !hookSetEmpty(node.Hooks) || len(node.NativeHooks) != 0 {
		return fmt.Errorf("container node %q supports only id, depends_on, when, trigger_rule and subworkflow/foreach", node.ID)
	}
	return nil
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
	if node.Assistant == "" {
		node.Assistant = defaults.Assistant
	}
	if node.Model == "" {
		node.Model = defaults.Model
	}
	if node.Session == "" {
		node.Session = defaults.Session
	}
}

func rewriteNodeRefs(value, prefix string, siblings map[string]spec.Node) string {
	if value == "" || prefix == "" {
		return value
	}
	for id := range siblings {
		value = strings.ReplaceAll(value, "${nodes."+id+".", "${nodes."+qualify(prefix, id)+".")
		value = strings.ReplaceAll(value, "nodes."+id+".", "nodes."+qualify(prefix, id)+".")
		value = strings.ReplaceAll(value, "${loop.previous."+id+".", "${loop.previous."+qualify(prefix, id)+".")
	}
	return value
}

func resolveInputs(inputs map[string]string, vars map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(inputs))
	for key, value := range inputs {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("input name must not be empty")
		}
		out["inputs."+key] = replaceVars(value, vars)
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
	}
	return value
}

func foreachVars(as string, index int, item any) (map[string]string, error) {
	vars := map[string]string{"index": strconv.Itoa(index), as + ".index": strconv.Itoa(index)}
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	switch value := item.(type) {
	case string:
		vars[as] = value
	case float64:
		vars[as] = strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		vars[as] = strconv.FormatBool(value)
	case nil:
		vars[as] = "null"
	case map[string]any:
		vars[as] = string(encoded)
		for key, field := range value {
			fieldJSON, err := json.Marshal(field)
			if err != nil {
				return nil, err
			}
			if text, ok := field.(string); ok {
				vars[as+"."+key] = text
			} else {
				vars[as+"."+key] = string(fieldJSON)
			}
		}
	default:
		vars[as] = string(encoded)
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
