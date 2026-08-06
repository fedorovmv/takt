package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/command"
	cfgpkg "takt/internal/config"
	"takt/internal/control"
	"takt/internal/definition"
	"takt/internal/evaluation"
	"takt/internal/gitworktree"
	"takt/internal/mcp"
	"takt/internal/profile"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/version"
	"takt/internal/workflow"
)

func main() {
	args := os.Args[1:]
	if err := run(args); err != nil {
		if wantsJSON(args) {
			_ = printErrorJSON(err)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "init":
		return initCmd(args[1:])
	case "validate":
		return validateCmd(args[1:])
	case "run":
		return runCmd(args[1:])
	case "workflow":
		return workflowCmd(args[1:])
	case "answer":
		return answerCmd(args[1:])
	case "resume":
		return resumeCmd(args[1:])
	case "status":
		return statusCmd(args[1:])
	case "children":
		return childrenCmd(args[1:])
	case "artifacts":
		return artifactsCmd(args[1:])
	case "cancel":
		return cancelCmd(args[1:])
	case "worktree":
		return worktreeCmd(args[1:])
	case "command":
		return commandCmd(args[1:])
	case "eval":
		return evalCmd(args[1:])
	case "mcp":
		return mcpCmd(args[1:])
	case "version":
		fmt.Println("takt v" + version.Value)
		return nil
	default:
		return usage()
	}
}

func mcpCmd(args []string) error {
	fs := newFlagSet("mcp")
	workspace := fs.String("workspace", ".", "control workspace")
	configPath := fs.String("config", ".takt/config.yaml", "default config path for direct workflow files")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--config": true})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt mcp [--workspace dir] [--config path]")
	}
	service, err := control.New(*workspace, *configPath)
	if err != nil {
		return err
	}
	return mcp.New(service, os.Stdin, os.Stdout, os.Stderr).ServeStdio(context.Background())
}

func initCmd(args []string) error {
	fs := newFlagSet("init")
	dir := fs.String("dir", ".", "destination project directory")
	force := fs.Bool("force", false, "replace an existing profile")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--dir": true, "--force": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt init <profile> [--dir project]")
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	root, err := profile.Init(fs.Arg(0), abs, *force)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"profile": fs.Arg(0), "path": root})
}

func validateCmd(args []string) error {
	fs := newFlagSet("validate")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--config": true, "--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt validate <workflow> [--config path]")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	wfPath, cfgPath, _, err := resolveWorkflowArgument(fs.Arg(0), absWorkspace, *configPath, flagPresent(args, "--config"))
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}
	wf, err := workflow.Load(wfPath)
	if err != nil {
		return err
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return err
	}
	resolver := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace).Commands
	if err := validateReferences(wf.Nodes, wf.Defaults, cfg, resolver); err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"valid": true, "workflow": wf.Metadata.Name})
}

func runCmd(args []string) error {
	fs := newFlagSet("run")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	input := fs.String("input", "", "input text or file")
	worktreeFlag := fs.Bool("worktree", false, "force Git worktree isolation")
	noWorktreeFlag := fs.Bool("no-worktree", false, "disable workflow Git worktree isolation")
	keepWorktree := fs.Bool("keep-worktree", false, "keep the worktree after a successful clean run")
	allowDirtyWorktree := fs.Bool("allow-dirty-worktree", false, "start from committed HEAD even when the control workspace is dirty")
	worktreeBase := fs.String("worktree-base", "", "Git revision used as the worktree base")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--config": true, "--workspace": true, "--input": true, "--worktree": false, "--no-worktree": false, "--keep-worktree": false, "--allow-dirty-worktree": false, "--worktree-base": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt run <workflow> [flags]")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	wfPath, cfgPath, resolvedProfile, err := resolveWorkflowArgument(fs.Arg(0), absWorkspace, *configPath, flagPresent(args, "--config"))
	if err != nil {
		return err
	}
	wf, err := workflow.Load(wfPath)
	if err != nil {
		return err
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return err
	}
	resolver := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace).Commands
	if err := validateReferences(wf.Nodes, wf.Defaults, cfg, resolver); err != nil {
		return err
	}
	var inputValue string
	if resolvedProfile != nil {
		inputValue, err = profile.PrepareInput(resolvedProfile.Manifest.Input, *input)
	} else {
		inputValue, err = readInput(*input)
	}
	if err != nil {
		return err
	}
	if *worktreeFlag && *noWorktreeFlag {
		return fmt.Errorf("--worktree and --no-worktree are mutually exclusive")
	}
	var worktreeOverride *bool
	if flagPresent(args, "--worktree") {
		value := true
		worktreeOverride = &value
	}
	if flagPresent(args, "--no-worktree") {
		value := false
		worktreeOverride = &value
	}
	runner := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace)
	state, runErr := runner.StartWithOptions(context.Background(), inputValue, runtime.StartOptions{
		Worktree: worktreeOverride, WorktreeBase: *worktreeBase, KeepWorktree: *keepWorktree, AllowDirty: *allowDirtyWorktree,
	})
	if errors.Is(runErr, runtime.ErrWaiting) {
		return printResult(*jsonOut, state)
	}
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

type workflowListEntry struct {
	Name        string `json:"name"`
	Selector    string `json:"selector"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

func workflowCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt workflow <list|describe> ...")
	}
	switch args[0] {
	case "list":
		return workflowListCmd(args[1:])
	case "describe":
		return workflowDescribeCmd(args[1:])
	default:
		return fmt.Errorf("usage: takt workflow <list|describe> ...")
	}
}

func workflowListCmd(args []string) error {
	fs := newFlagSet("workflow list")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt workflow list <profile> [--workspace dir]")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	resolved, err := profile.Resolve(fs.Arg(0), absWorkspace)
	if err != nil {
		return err
	}
	entries := make([]workflowListEntry, 0, len(resolved.Manifest.Workflows)+1)
	defaultWorkflow, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		return err
	}
	entries = append(entries, workflowListEntry{
		Name:        defaultWorkflow.Metadata.Name,
		Selector:    resolved.Name,
		Description: defaultWorkflow.Metadata.Description,
		Default:     true,
	})
	names := make([]string, 0, len(resolved.Manifest.Workflows))
	for name := range resolved.Manifest.Workflows {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		selected, err := resolved.SelectWorkflow(name)
		if err != nil {
			return err
		}
		wf, err := workflow.Load(selected.WorkflowPath)
		if err != nil {
			return fmt.Errorf("profile workflow %q: %w", name, err)
		}
		entries = append(entries, workflowListEntry{
			Name:        name,
			Selector:    resolved.Name + ":" + name,
			Description: wf.Metadata.Description,
		})
	}
	return printResult(*jsonOut, map[string]any{"profile": resolved.Name, "workflows": entries})
}

func workflowDescribeCmd(args []string) error {
	fs := newFlagSet("workflow describe")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt workflow describe <profile[:workflow]> [--workspace dir]")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	resolved, err := profile.Resolve(fs.Arg(0), absWorkspace)
	if err != nil {
		return err
	}
	wf, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		return err
	}
	selector := resolved.Name
	if resolved.WorkflowName != "" {
		selector += ":" + resolved.WorkflowName
	}
	publicNodes := make([]map[string]any, 0)
	for _, node := range wf.Nodes {
		if node.Hidden || node.PublicParent != "" {
			continue
		}
		publicNodes = append(publicNodes, map[string]any{
			"id":           node.ID,
			"depends_on":   node.DependsOn,
			"when":         node.When,
			"trigger_rule": node.TriggerRule,
		})
	}
	return printResult(*jsonOut, map[string]any{
		"selector":    selector,
		"name":        wf.Metadata.Name,
		"description": wf.Metadata.Description,
		"nodes":       publicNodes,
	})
}

func answerCmd(args []string) error {
	fs := newFlagSet("answer")
	workspace := fs.String("workspace", ".", "workspace")
	value := fs.String("value", "", "answer value")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--value": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: takt answer <run-id> <node-id> --value text")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: absWorkspace}
	target, nodeID, err := resolveApprovalTarget(st, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return err
	}
	release, err := st.AcquireLock(target.ID)
	if err != nil {
		return err
	}
	target, err = st.Load(target.ID)
	if err != nil {
		_ = release()
		return err
	}
	runner, err := runnerForState(target)
	if err != nil {
		_ = release()
		return err
	}
	if err := runner.VerifyDefinitions(target); err != nil {
		_ = release()
		return err
	}
	if target.Waiting == nil || target.Waiting.Kind == "child_run" {
		_ = release()
		return fmt.Errorf("run %s is not waiting for an approval", target.ID)
	}
	if target.Approvals == nil {
		target.Approvals = map[string]string{}
	}
	target.Approvals[nodeID] = *value
	if ns := target.Nodes[nodeID]; ns != nil {
		ns.Status = store.NodePending
	}
	target.Status = store.RunRunning
	target.Waiting = nil
	if err := st.Commit(target, store.Event{Type: "approval.answered", NodeID: nodeID, Data: map[string]any{"value_captured": true}}); err != nil {
		_ = release()
		return err
	}
	target, runErr := runner.Resume(context.Background(), target)
	_ = release()
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) {
		return runErr
	}
	root, cascadeErr := resumeParentChain(st, target)
	if cascadeErr != nil && !errors.Is(cascadeErr, runtime.ErrWaiting) {
		return cascadeErr
	}
	return printResult(*jsonOut, root)
}

func resolveApprovalTarget(st store.FS, runID, requestedNodeID string) (*store.RunState, string, error) {
	state, err := st.Load(runID)
	if err != nil {
		return nil, "", err
	}
	allowed := map[string]bool{requestedNodeID: false}
	for state.Waiting != nil && state.Waiting.Kind == "child_run" {
		allowed[state.Waiting.NodeID] = true
		if node := state.Nodes[state.Waiting.NodeID]; node != nil && node.PublicParent != "" {
			allowed[node.PublicParent] = true
		}
		childIDs := append([]string(nil), state.Waiting.ChildRunIDs...)
		if len(childIDs) == 0 && state.Waiting.ChildRunID != "" {
			childIDs = []string{state.Waiting.ChildRunID}
		}
		if len(childIDs) != 1 {
			return nil, "", fmt.Errorf("run %s has %d child runs waiting; answer one child run directly: %s", state.ID, len(childIDs), strings.Join(childIDs, ", "))
		}
		state, err = st.Load(childIDs[0])
		if err != nil {
			return nil, "", err
		}
	}
	if state.Waiting == nil {
		return nil, "", fmt.Errorf("run %s is not waiting for approval", state.ID)
	}
	nodeID := state.Waiting.NodeID
	allowed[nodeID] = true
	if node := state.Nodes[nodeID]; node != nil && node.PublicParent != "" {
		allowed[node.PublicParent] = true
	}
	if !allowed[requestedNodeID] {
		return nil, "", fmt.Errorf("run is not waiting for approval node %q", requestedNodeID)
	}
	return state, nodeID, nil
}

func resumeParentChain(st store.FS, child *store.RunState) (*store.RunState, error) {
	current := child
	for current != nil && current.ParentRunID != "" {
		release, err := st.AcquireLock(current.ParentRunID)
		if err != nil {
			return current, err
		}
		parent, err := st.Load(current.ParentRunID)
		if err != nil {
			_ = release()
			return current, err
		}
		runner, err := runnerForState(parent)
		if err != nil {
			_ = release()
			return current, err
		}
		parent, runErr := runner.Resume(context.Background(), parent)
		_ = release()
		current = parent
		if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) {
			return current, runErr
		}
	}
	return current, nil
}

func resumeCmd(args []string) error {
	fs := newFlagSet("resume")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt resume <run-id>")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: absWorkspace}
	release, err := st.AcquireLock(fs.Arg(0))
	if err != nil {
		return err
	}
	defer release()
	state, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	runner, err := runnerForState(state)
	if err != nil {
		return err
	}
	state, runErr := runner.Resume(context.Background(), state)
	if errors.Is(runErr, runtime.ErrWaiting) {
		return printResult(*jsonOut, state)
	}
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

func runnerForState(state *store.RunState) (*runtime.Runner, error) {
	wf, err := workflow.Load(state.WorkflowPath)
	if err != nil {
		return nil, err
	}
	cfg, err := cfgpkg.Load(state.ConfigPath)
	if err != nil {
		return nil, err
	}
	runner := runtime.New(wf, cfg, state.WorkflowPath, state.ConfigPath, state.Workspace)
	runner.SetStartOptions(runtime.StartOptionsFromState(state))
	if state.ExecutionWorkspace != "" {
		if state.Worktree != nil && state.Worktree.Enabled && !state.Worktree.Removed {
			if info, statErr := os.Stat(state.ExecutionWorkspace); statErr != nil || !info.IsDir() {
				return nil, fmt.Errorf("managed worktree for run %s is unavailable at %s", state.ID, state.ExecutionWorkspace)
			}
		}
		runner.SetExecutionWorkspace(state.ExecutionWorkspace)
	}
	if err := validateReferences(wf.Nodes, wf.Defaults, cfg, runner.Commands); err != nil {
		return nil, err
	}
	return runner, nil
}

func statusCmd(args []string) error {
	fs := newFlagSet("status")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt status <run-id>")
	}
	abs, _ := filepath.Abs(*workspace)
	state, err := (store.FS{Workspace: abs}).Load(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}

func childrenCmd(args []string) error {
	fs := newFlagSet("children")
	workspace := fs.String("workspace", ".", "workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt children <run-id>")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: abs}
	parent, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	fanOutMeta := map[string]map[string]any{}
	for nodeID, node := range parent.Nodes {
		if node == nil {
			continue
		}
		for _, item := range node.ChildRuns {
			var decoded any
			if err := json.Unmarshal(item.Item, &decoded); err != nil {
				decoded = string(item.Item)
			}
			fanOutMeta[item.RunID] = map[string]any{"node_id": nodeID, "attempt": item.Attempt, "index": item.Index, "item": decoded}
		}
	}
	children := make([]map[string]any, 0, len(parent.ChildRunIDs))
	for _, id := range parent.ChildRunIDs {
		child, loadErr := st.Load(id)
		if loadErr != nil {
			children = append(children, map[string]any{"id": id, "error": loadErr.Error()})
			continue
		}
		value := map[string]any{
			"id": child.ID, "status": child.Status, "workflow_path": child.WorkflowPath,
			"parent_node_id": child.ParentNodeID, "execution_workspace": child.ExecutionWorkspace,
			"usage": child.Usage,
		}
		if meta := fanOutMeta[id]; meta != nil {
			value["fan_out"] = meta
		}
		children = append(children, value)
	}
	return printResult(*jsonOut, map[string]any{"run_id": parent.ID, "children": children})
}

func artifactsCmd(args []string) error {
	fs := newFlagSet("artifacts")
	workspace := fs.String("workspace", ".", "workspace")
	nodeID := fs.String("node", "", "filter by producer node id")
	artifactType := fs.String("type", "", "filter by semantic artifact type")
	recursive := fs.Bool("recursive", false, "include artifacts from all descendant Runs")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--node": true, "--type": true, "--recursive": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt artifacts <run-id> [--node id] [--type type] [--recursive]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: abs}
	root, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	runs := []*store.RunState{root}
	if *recursive {
		queue := append([]string(nil), root.ChildRunIDs...)
		seen := map[string]bool{root.ID: true}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			child, loadErr := st.Load(id)
			if loadErr != nil {
				return loadErr
			}
			runs = append(runs, child)
			queue = append(queue, child.ChildRunIDs...)
		}
	}
	artifacts := make([]store.ArtifactRef, 0)
	seenArtifacts := map[string]bool{}
	for _, run := range runs {
		for _, artifact := range run.Artifacts {
			if *nodeID != "" && artifact.ProducerNodeID != *nodeID {
				continue
			}
			if *artifactType != "" && artifact.Type != *artifactType {
				continue
			}
			key := artifact.ProducerRunID + "\x00" + artifact.ID
			if seenArtifacts[key] {
				continue
			}
			seenArtifacts[key] = true
			artifacts = append(artifacts, artifact)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].ProducerRunID != artifacts[j].ProducerRunID {
			return artifacts[i].ProducerRunID < artifacts[j].ProducerRunID
		}
		if artifacts[i].ProducerNodeID != artifacts[j].ProducerNodeID {
			return artifacts[i].ProducerNodeID < artifacts[j].ProducerNodeID
		}
		return artifacts[i].Type < artifacts[j].Type
	})
	return printResult(*jsonOut, map[string]any{"run_id": root.ID, "artifacts": artifacts})
}

func cancelCmd(args []string) error {
	fs := newFlagSet("cancel")
	workspace := fs.String("workspace", ".", "workspace")
	reason := fs.String("reason", "cancelled by user", "cancellation reason")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--reason": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt cancel <run-id> [--reason text]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: abs}
	state, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	if state.Status == store.RunCompleted || state.Status == store.RunFailed || state.Status == store.RunCancelled {
		return fmt.Errorf("cannot cancel terminal run %s with status %s", state.ID, state.Status)
	}
	if err := cancelRunTree(st, state, *reason, false); err != nil {
		return err
	}
	state, err = st.Load(state.ID)
	if err != nil {
		return err
	}
	if state.Status == store.RunWaiting {
		release, lockErr := st.AcquireLock(state.ID)
		if lockErr != nil {
			return lockErr
		}
		state, err = st.Load(state.ID)
		if err == nil {
			var runner *runtime.Runner
			runner, err = runnerForState(state)
			if err == nil {
				state, _ = runner.Cancel(state, *reason)
			}
		}
		_ = release()
		if err != nil {
			return err
		}
		root, cascadeErr := resumeParentChain(st, state)
		if cascadeErr == nil || errors.Is(cascadeErr, runtime.ErrWaiting) {
			return printResult(*jsonOut, root)
		}
		return cascadeErr
	}
	return printResult(*jsonOut, map[string]any{"run_id": state.ID, "status": state.Status, "cancel_requested": true, "children": state.ChildRunIDs})
}

func cancelRunTree(st store.FS, state *store.RunState, reason string, includeSelf bool) error {
	for _, childID := range state.ChildRunIDs {
		child, err := st.Load(childID)
		if err != nil {
			continue
		}
		if err := cancelRunTree(st, child, reason, true); err != nil {
			return err
		}
	}
	if !includeSelf {
		return st.RequestCancel(state.ID)
	}
	if state.Status == store.RunCompleted || state.Status == store.RunCancelled || state.Status == store.RunFailed {
		return nil
	}
	if err := st.RequestCancel(state.ID); err != nil {
		return err
	}
	if state.Status != store.RunWaiting {
		return nil
	}
	release, err := st.AcquireLock(state.ID)
	if err != nil {
		return err
	}
	defer release()
	state, err = st.Load(state.ID)
	if err != nil {
		return err
	}
	runner, err := runnerForState(state)
	if err != nil {
		return err
	}
	_, cancelErr := runner.Cancel(state, reason)
	if cancelErr != nil && !errors.Is(cancelErr, context.Canceled) {
		return cancelErr
	}
	return nil
}

type worktreeListEntry struct {
	RunID              string `json:"run_id"`
	RunStatus          string `json:"run_status"`
	Path               string `json:"path"`
	Branch             string `json:"branch"`
	BaseCommit         string `json:"base_commit,omitempty"`
	Dirty              bool   `json:"dirty,omitempty"`
	Removed            bool   `json:"removed,omitempty"`
	RetainedReason     string `json:"retained_reason,omitempty"`
	ExecutionWorkspace string `json:"execution_workspace,omitempty"`
}

func worktreeCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt worktree <list|remove|prune> ...")
	}
	switch args[0] {
	case "list":
		return worktreeListCmd(args[1:])
	case "remove":
		return worktreeRemoveCmd(args[1:])
	case "prune":
		return worktreePruneCmd(args[1:])
	default:
		return fmt.Errorf("usage: takt worktree <list|remove|prune> ...")
	}
}

func worktreeListCmd(args []string) error {
	fs := newFlagSet("worktree list")
	workspace := fs.String("workspace", ".", "control workspace")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt worktree list [--workspace dir]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	runsDir := filepath.Join(abs, ".takt", "runs")
	entries, err := os.ReadDir(runsDir)
	if errors.Is(err, os.ErrNotExist) {
		return printResult(*jsonOut, map[string]any{"worktrees": []worktreeListEntry{}})
	}
	if err != nil {
		return err
	}
	st := store.FS{Workspace: abs}
	var result []worktreeListEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, loadErr := st.Load(entry.Name())
		if loadErr != nil {
			return loadErr
		}
		if state.Worktree == nil || !state.Worktree.Enabled {
			continue
		}
		wt := state.Worktree
		if !wt.Removed {
			if status, inspectErr := gitworktree.Inspect(context.Background(), wt.Path); inspectErr == nil {
				wt.Dirty = status.Dirty
			}
		}
		result = append(result, worktreeListEntry{
			RunID: state.ID, RunStatus: state.Status, Path: wt.Path, Branch: wt.Branch,
			BaseCommit: wt.BaseCommit, Dirty: wt.Dirty, Removed: wt.Removed,
			RetainedReason: wt.RetainedReason, ExecutionWorkspace: wt.ExecutionWorkspace,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RunID < result[j].RunID })
	return printResult(*jsonOut, map[string]any{"worktrees": result})
}

func worktreeRemoveCmd(args []string) error {
	fs := newFlagSet("worktree remove")
	workspace := fs.String("workspace", ".", "control workspace")
	force := fs.Bool("force", false, "remove a dirty worktree")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--force": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt worktree remove <run-id> [--force]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	st := store.FS{Workspace: abs}
	release, err := st.AcquireLock(fs.Arg(0))
	if err != nil {
		return err
	}
	defer release()
	state, err := st.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	if state.Status == store.RunRunning || state.Status == store.RunWaiting {
		return fmt.Errorf("cannot remove worktree for active run %s with status %s", state.ID, state.Status)
	}
	wt := state.Worktree
	if wt == nil || !wt.Enabled {
		return fmt.Errorf("run %s has no managed worktree", state.ID)
	}
	if wt.Removed {
		return printResult(*jsonOut, state)
	}
	status, inspectErr := gitworktree.Inspect(context.Background(), wt.Path)
	if inspectErr == nil {
		wt.Dirty = status.Dirty
	}
	if wt.Dirty && !*force {
		return fmt.Errorf("worktree %s has uncommitted changes; inspect it or pass --force", wt.Path)
	}
	if err := gitworktree.Remove(context.Background(), wt.RepositoryRoot, wt.Path, *force); err != nil {
		return err
	}
	wt.Removed = true
	wt.RemovedAt = time.Now().UTC()
	wt.RetainedReason = ""
	wt.CleanupError = ""
	branchRemoved, branchErr := gitworktree.DeleteBranchIfUnchanged(context.Background(), wt.RepositoryRoot, wt.Branch, wt.BaseCommit)
	wt.BranchRemoved = branchRemoved
	if branchErr != nil {
		wt.BranchCleanupError = branchErr.Error()
	}
	if err := st.Commit(state, store.Event{Type: "worktree.removed", Data: map[string]any{"path": wt.Path, "branch": wt.Branch, "manual": true, "force": *force, "branch_removed": branchRemoved, "branch_cleanup_error": wt.BranchCleanupError}}); err != nil {
		return err
	}
	return printResult(*jsonOut, state)
}

func worktreePruneCmd(args []string) error {
	fs := newFlagSet("worktree prune")
	workspace := fs.String("workspace", ".", "workspace inside the Git repository")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt worktree prune [--workspace dir]")
	}
	abs, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	if err := gitworktree.Prune(context.Background(), abs); err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"pruned": true})
}

func commandCmd(args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return fmt.Errorf("usage: takt command run <name> [flags]")
	}
	fs := newFlagSet("command run")
	configPath := fs.String("config", ".takt/config.yaml", "config path")
	workspace := fs.String("workspace", ".", "workspace")
	input := fs.String("input", "", "input text or file")
	assistantName := fs.String("assistant", "", "override assistant")
	modelName := fs.String("model", "", "override model")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args[1:], map[string]bool{"--config": true, "--workspace": true, "--input": true, "--assistant": true, "--model": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt command run <name>")
	}
	abs, _ := filepath.Abs(*workspace)
	cfgPath, _ := filepath.Abs(*configPath)
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return err
	}
	dirs := []string{filepath.Join(abs, ".takt", "commands"), filepath.Join(abs, "commands")}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".takt", "commands"))
	}
	resolver := command.Resolver{Dirs: dirs}
	cmd, err := resolver.Resolve(fs.Arg(0))
	if err != nil {
		return err
	}
	a := *assistantName
	if a == "" {
		a = cmd.Assistant
	}
	m := *modelName
	if m == "" {
		m = cmd.Model
	}
	if a == "" || m == "" {
		return fmt.Errorf("command must resolve assistant and model")
	}
	inputValue, err := readInput(*input)
	if err != nil {
		return err
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "command-" + fs.Arg(0)}, Defaults: spec.Defaults{Assistant: a, Model: m}, Nodes: []spec.Node{{ID: "command", Command: fs.Arg(0)}}}
	runner := runtime.New(wf, cfg, "<command>", cfgPath, abs)
	runner.Commands = resolver
	state, runErr := runner.Start(context.Background(), inputValue)
	if runErr != nil {
		return runErr
	}
	return printResult(*jsonOut, state)
}

func evalCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: takt eval <run|report> [flags]")
	}
	switch args[0] {
	case "run":
		fs := newFlagSet("eval run")
		configPath := fs.String("config", ".takt/config.yaml", "config path")
		casesDir := fs.String("cases", "", "directory containing Markdown cases")
		templateDir := fs.String("workspace-template", "", "workspace template directory")
		outputDir := fs.String("output", ".takt/evals/latest", "evaluation output directory")
		repeat := fs.Int("repeat", 1, "number of repetitions per case")
		answer := fs.String("answer", "", "automatic approval answer")
		replace := fs.Bool("replace", false, "replace existing case workspaces")
		strategyID := fs.String("strategy-id", "", "stable strategy identifier")
		benchmarkID := fs.String("benchmark-id", "", "stable benchmark identifier")
		qualityNode := fs.String("quality-node", "", "node that emits takt-validation/v1alpha1")
		generationNode := fs.String("generation-node", "", "generation node used for success@1")
		validatorID := fs.String("validator-id", "", "validator identifier")
		validatorVersion := fs.String("validator-version", "", "validator version")
		validatorPath := fs.String("validator-path", "", "validator file or directory to fingerprint")
		jsonOut := fs.Bool("json", true, "JSON output")
		values := map[string]bool{
			"--config": true, "--cases": true, "--workspace-template": true, "--output": true,
			"--repeat": true, "--answer": true, "--replace": false, "--json": false,
			"--strategy-id": true, "--benchmark-id": true, "--quality-node": true,
			"--generation-node": true, "--validator-id": true, "--validator-version": true,
			"--validator-path": true,
		}
		if err := fs.Parse(interspersed(args[1:], values)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval run <workflow> --config path --cases dir --workspace-template dir [flags]")
		}
		report, err := evaluation.Run(context.Background(), evaluation.RunOptions{
			WorkflowPath: fs.Arg(0), ConfigPath: *configPath, CasesDir: *casesDir,
			WorkspaceTemplate: *templateDir, OutputDir: *outputDir, Repeat: *repeat,
			ApprovalAnswer: *answer, Replace: *replace,
			StrategyID: *strategyID, BenchmarkID: *benchmarkID,
			QualityNode: *qualityNode, GenerationNode: *generationNode,
			ValidatorID: *validatorID, ValidatorVersion: *validatorVersion, ValidatorPath: *validatorPath,
		})
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	case "report":
		fs := newFlagSet("eval report")
		jsonOut := fs.Bool("json", true, "JSON output")
		if err := fs.Parse(interspersed(args[1:], map[string]bool{"--json": false})); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: takt eval report <evaluation-output-dir>")
		}
		report, err := evaluation.LoadReport(fs.Arg(0))
		if err != nil {
			return err
		}
		return printResult(*jsonOut, report)
	default:
		return fmt.Errorf("usage: takt eval <run|report> [flags]")
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func interspersed(args []string, takesValue map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := arg
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			name = name[:idx]
			continue
		}
		if takesValue[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func validateReferences(nodes []spec.Node, defaults spec.Defaults, cfg *spec.Config, resolver command.Resolver) error {
	return validateReferencesRecursive(nodes, defaults, cfg, resolver, map[string]bool{}, 0)
}

func validateReferencesRecursive(nodes []spec.Node, defaults spec.Defaults, cfg *spec.Config, resolver command.Resolver, stack map[string]bool, depth int) error {
	if depth > 16 {
		return fmt.Errorf("governed child workflow validation exceeds depth 16")
	}
	for _, n := range nodes {
		assistantName, modelName := n.Assistant, n.Model
		if n.Command != "" {
			cmd, err := resolver.Resolve(n.Command)
			if err != nil {
				return fmt.Errorf("node %q: %w", n.ID, err)
			}
			if assistantName == "" {
				assistantName = cmd.Assistant
			}
			if modelName == "" {
				modelName = cmd.Model
			}
		}
		if n.Command != "" || n.Prompt != "" {
			if assistantName == "" {
				assistantName = defaults.Assistant
			}
			if modelName == "" {
				modelName = defaults.Model
			}
			if _, ok := cfg.Assistants[assistantName]; !ok {
				return fmt.Errorf("node %q references unknown assistant %q", n.ID, assistantName)
			}
			if _, ok := cfg.Models[modelName]; !ok {
				return fmt.Errorf("node %q references unknown model %q", n.ID, modelName)
			}
		}
		if n.LoopGroup != nil {
			if err := validateReferencesRecursive(n.LoopGroup.Nodes, defaults, cfg, resolver, stack, depth); err != nil {
				return fmt.Errorf("loop_group %q: %w", n.ID, err)
			}
		}
		if n.WorkflowRun != nil {
			path := n.WorkflowRun.Path
			if !filepath.IsAbs(path) {
				return fmt.Errorf("node %q child workflow path was not resolved: %s", n.ID, path)
			}
			path = filepath.Clean(path)
			if stack[path] {
				return fmt.Errorf("recursive governed child workflow reference at %s", path)
			}
			child, err := workflow.Load(path)
			if err != nil {
				return fmt.Errorf("node %q child workflow: %w", n.ID, err)
			}
			if n.WorkflowRun.OutputNode != "" {
				found := false
				for _, childNode := range child.Nodes {
					if childNode.ID == n.WorkflowRun.OutputNode && !childNode.Hidden && childNode.PublicParent == "" {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("node %q child output_node %q does not exist", n.ID, n.WorkflowRun.OutputNode)
				}
			} else if terminals := publicTerminalIDs(child.Nodes); len(terminals) != 1 {
				return fmt.Errorf("node %q child workflow %q has %d terminal nodes; set output_node", n.ID, child.Metadata.Name, len(terminals))
			}
			childResolver := resolver
			childResolver.Dirs = append([]string{filepath.Join(filepath.Dir(path), "commands")}, childResolver.Dirs...)
			stack[path] = true
			err = validateReferencesRecursive(child.Nodes, child.Defaults, cfg, childResolver, stack, depth+1)
			delete(stack, path)
			if err != nil {
				return fmt.Errorf("node %q child workflow %q: %w", n.ID, child.Metadata.Name, err)
			}
		}
	}
	return nil
}

func publicTerminalIDs(nodes []spec.Node) []string {
	public := map[string]bool{}
	depended := map[string]bool{}
	for _, node := range nodes {
		if !node.Hidden && node.PublicParent == "" {
			public[node.ID] = true
		}
	}
	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			if public[dep] {
				depended[dep] = true
			}
		}
	}
	var out []string
	for id := range public {
		if !depended[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func resolveWorkflowArgument(value, workspace, configValue string, configExplicit bool) (string, string, *profile.Resolved, error) {
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		wfPath, err := filepath.Abs(value)
		if err != nil {
			return "", "", nil, err
		}
		cfgPath, err := filepath.Abs(configValue)
		return wfPath, cfgPath, nil, err
	}
	resolved, err := profile.Resolve(value, workspace)
	if err != nil {
		return "", "", nil, err
	}
	cfgPath := resolved.ConfigPath
	if configExplicit {
		cfgPath, err = filepath.Abs(configValue)
		if err != nil {
			return "", "", nil, err
		}
	}
	return resolved.WorkflowPath, cfgPath, resolved, nil
}

func flagPresent(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func readInput(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if b, err := os.ReadFile(v); err == nil {
		return string(b), nil
	}
	return v, nil
}
func printResult(jsonOut bool, value any) error {
	if state, ok := value.(*store.RunState); ok {
		value = state.PublicView()
	}
	if jsonOut {
		b, err := json.MarshalIndent(map[string]any{"ok": true, "result": value}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("%+v\n", value)
	return nil
}

func wantsJSON(args []string) bool {
	value := false
	if len(args) > 0 {
		switch args[0] {
		case "run", "answer", "resume", "status", "children", "artifacts", "worktree", "eval":
			value = true
		case "command":
			value = len(args) > 1 && args[1] == "run"
		}
	}
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			value = true
		}
		if arg == "--json=false" {
			value = false
		}
	}
	return value
}

func printErrorJSON(err error) error {
	code := "internal_error"
	details := map[string]any{}
	retryable := false
	var runErr *runtime.RunFailedError
	if errors.As(err, &runErr) {
		code = runErr.Code
		if code == "" {
			code = "run_failed"
		}
		details["run_id"] = runErr.RunID
		details["node_id"] = runErr.NodeID
	}
	var changed *definition.ChangedError
	if errors.As(err, &changed) {
		code = "definition_changed"
		details["definition"] = changed.Kind
	}
	var inconsistent *store.InconsistentError
	if errors.As(err, &inconsistent) {
		code = "store_inconsistent"
		details["run_id"] = inconsistent.RunID
	}
	payload := map[string]any{"ok": false, "error": map[string]any{
		"code": code, "message": err.Error(), "retryable": retryable, "details": details,
	}}
	b, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Fprintln(os.Stderr, string(b))
	return nil
}

func usage() error {
	return fmt.Errorf("usage: takt <init|validate|run|workflow|answer|resume|status|children|artifacts|cancel|worktree|command|eval|mcp|version>")
}
