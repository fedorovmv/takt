package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"takt/internal/authoring"
	"takt/internal/command"
	cfgpkg "takt/internal/config"
	"takt/internal/control"
	"takt/internal/daemon"
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
	case "daemon":
		return daemonCmd(args[1:])
	case "events":
		return eventsCmd(args[1:])
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
	useDaemon := fs.Bool("daemon", false, "proxy MCP through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--config": true, "--daemon": false, "--socket": true})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt mcp [--daemon] [--workspace dir] [--config path]")
	}
	if *useDaemon {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		return proxyMCPThroughDaemon(context.Background(), client, os.Stdin, os.Stdout, os.Stderr)
	}
	service, err := control.New(*workspace, *configPath)
	if err != nil {
		return err
	}
	return mcp.New(service, os.Stdin, os.Stdout, os.Stderr).ServeStdio(context.Background())
}

func proxyMCPThroughDaemon(ctx context.Context, client *daemon.Client, in io.Reader, out, errOut io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var workers sync.WaitGroup
	var writeMu sync.Mutex
	sem := make(chan struct{}, 64)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		sem <- struct{}{}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-sem }()
			payload, respond, err := client.MCP(ctx, line)
			if err != nil {
				fmt.Fprintln(errOut, "daemon MCP request failed:", err)
				return
			}
			if !respond {
				return
			}
			writeMu.Lock()
			defer writeMu.Unlock()
			_, _ = out.Write(append(payload, '\n'))
		}()
	}
	workers.Wait()
	return scanner.Err()
}

func daemonCmd(args []string) error {
	subcommand := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	fs := newFlagSet("daemon " + subcommand)
	workspace := fs.String("workspace", ".", "control workspace")
	configPath := fs.String("config", ".takt/config.yaml", "default config path")
	socket := fs.String("socket", "", "daemon Unix socket path")
	jsonOut := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--config": true, "--socket": true, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: takt daemon [serve|start|status|stop] [--workspace dir]")
	}
	switch subcommand {
	case "serve":
		server, err := daemon.New(daemon.Options{Workspace: *workspace, ConfigPath: *configPath, SocketPath: *socket, ErrOut: os.Stderr})
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return server.Serve(ctx)
	case "start":
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		if metadata, healthErr := client.Health(ctx); healthErr == nil {
			cancel()
			return printResult(*jsonOut, map[string]any{"started": false, "already_running": true, "daemon": metadata})
		}
		cancel()
		paths := client.Paths()
		if err := os.MkdirAll(filepath.Dir(paths.Log), 0o700); err != nil {
			return err
		}
		logFile, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		executable, err := os.Executable()
		if err != nil {
			_ = logFile.Close()
			return err
		}
		childArgs := []string{"daemon", "serve", "--workspace", *workspace, "--config", *configPath}
		if *socket != "" {
			childArgs = append(childArgs, "--socket", *socket)
		}
		command := exec.Command(executable, childArgs...)
		command.Stdin = nil
		command.Stdout = logFile
		command.Stderr = logFile
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			_ = logFile.Close()
			return err
		}
		_ = command.Process.Release()
		_ = logFile.Close()
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer waitCancel()
		metadata, err := daemon.WaitForHealth(waitCtx, client, 50*time.Millisecond)
		if err != nil {
			return fmt.Errorf("start daemon: %w; see %s", err, paths.Log)
		}
		return printResult(*jsonOut, map[string]any{"started": true, "daemon": metadata, "log": paths.Log})
	case "status":
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		metadata, err := client.Health(ctx)
		if err != nil {
			return fmt.Errorf("daemon is not running: %w", err)
		}
		return printResult(*jsonOut, map[string]any{"running": true, "daemon": metadata})
	case "stop":
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Shutdown(ctx); err != nil {
			return err
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			probeCtx, probeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, healthErr := client.Health(probeCtx)
			probeCancel()
			if healthErr != nil {
				return printResult(*jsonOut, map[string]any{"stopped": true})
			}
			select {
			case <-deadline.C:
				return fmt.Errorf("daemon did not stop")
			case <-ticker.C:
			}
		}
	default:
		return fmt.Errorf("usage: takt daemon [serve|start|status|stop] [--workspace dir]")
	}
}

func eventsCmd(args []string) error {
	fs := newFlagSet("events")
	workspace := fs.String("workspace", ".", "workspace")
	useDaemon := fs.Bool("daemon", false, "subscribe through the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	after := fs.Uint64("after", 0, "read events after this revision")
	limit := fs.Int("limit", 200, "maximum events per batch")
	follow := fs.Bool("follow", false, "follow until the Run becomes terminal")
	jsonOut := fs.Bool("json", true, "JSON lines output")
	if err := fs.Parse(interspersed(args, map[string]bool{"--workspace": true, "--daemon": false, "--socket": true, "--after": true, "--limit": true, "--follow": false, "--json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: takt events <run-id> [--follow] [--daemon]")
	}
	runID := fs.Arg(0)
	printEvent := func(event store.Event) error {
		if *jsonOut {
			raw, err := json.Marshal(event)
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		fmt.Printf("%d\t%s\t%s\n", event.Revision, event.Type, event.NodeID)
		return nil
	}
	if *useDaemon && *follow {
		client, err := daemon.NewClient(*workspace, *socket)
		if err != nil {
			return err
		}
		return client.Subscribe(context.Background(), runID, *after, *limit, printEvent)
	}
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	cursor := *after
	for {
		wait := time.Duration(0)
		if *follow {
			wait = 30 * time.Second
		}
		result, err := service.Events(context.Background(), runID, cursor, *limit, wait)
		if err != nil {
			return err
		}
		for _, event := range result.Events {
			if err := printEvent(event); err != nil {
				return err
			}
			cursor = event.Revision
		}
		if !*follow {
			return nil
		}
		state, err := service.GetRun(runID)
		if err != nil {
			return err
		}
		if (state.Status == store.RunCompleted || state.Status == store.RunFailed || state.Status == store.RunCancelled) && len(result.Events) == 0 {
			return nil
		}
	}
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
	warningsAsErrors := fs.Bool("warnings-as-errors", false, "treat authoring warnings as validation errors")
	if err := fs.Parse(interspersed(args, map[string]bool{"--config": true, "--workspace": true, "--json": false, "--warnings-as-errors": false})); err != nil {
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
	runner := runtime.New(wf, cfg, wfPath, cfgPath, absWorkspace)
	resolver := runner.Commands
	if err := workflow.ValidateReferences(wf, cfg, resolver); err != nil {
		return err
	}
	if err := runtime.ValidateCapabilities(wf, cfg, wfPath, resolver); err != nil {
		return fmt.Errorf("capability validation: %w", err)
	}
	diagnostics := authoring.Analyze(wf, resolver)
	if *warningsAsErrors {
		for index := range diagnostics {
			if diagnostics[index].Severity == "warning" {
				diagnostics[index].Severity = "error"
				diagnostics[index].Code = "warning_as_error." + diagnostics[index].Code
			}
		}
	}
	if authoring.HasErrors(diagnostics) {
		return &authoring.Error{Diagnostics: diagnostics}
	}
	return printResult(*jsonOut, map[string]any{"valid": true, "workflow": wf.Metadata.Name, "diagnostics": diagnostics})
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
	useDaemon := fs.Bool("daemon", false, "run in the local daemon")
	socket := fs.String("socket", "", "daemon Unix socket path")
	if err := fs.Parse(interspersed(args, map[string]bool{"--config": true, "--workspace": true, "--input": true, "--worktree": false, "--no-worktree": false, "--keep-worktree": false, "--allow-dirty-worktree": false, "--worktree-base": true, "--json": false, "--daemon": false, "--socket": true})); err != nil {
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
		inputValue, err = profile.PrepareInput(resolvedProfile.EffectiveInput(), *input)
	} else {
		inputValue, err = readInput(*input)
	}
	if err != nil {
		return err
	}
	inputValue, err = runtime.ValidateWorkflowInput(inputValue, wf.Input)
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
	if *useDaemon {
		client, err := daemon.NewClient(absWorkspace, *socket)
		if err != nil {
			return err
		}
		request := control.StartRequest{Selector: fs.Arg(0), Input: *input, Worktree: worktreeOverride, WorktreeBase: *worktreeBase, KeepWorktree: *keepWorktree, AllowDirty: *allowDirtyWorktree, Detached: true}
		if flagPresent(args, "--config") {
			request.ConfigPath = *configPath
		}
		var result control.StartResult
		if err := client.Call(context.Background(), "run.start", request, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, result)
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

func controlService(workspace string) (*control.Service, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return control.New(abs, "")
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
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	state, err := service.Answer(context.Background(), fs.Arg(0), fs.Arg(1), *value)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
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
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	state, err := service.Resume(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, state)
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
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	state, err := service.GetRun(fs.Arg(0))
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
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	children, err := service.Children(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, children)
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
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.Artifacts(fs.Arg(0), control.ArtifactQuery{NodeID: *nodeID, Type: *artifactType, Recursive: *recursive})
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
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
	service, err := controlService(*workspace)
	if err != nil {
		return err
	}
	result, err := service.Cancel(fs.Arg(0), *reason)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
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
	var authoringErr *authoring.Error
	if errors.As(err, &authoringErr) {
		code = "authoring_validation_failed"
		details["diagnostics"] = authoringErr.Diagnostics
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
	return fmt.Errorf("usage: takt <init|validate|run|workflow|answer|resume|status|children|artifacts|events|cancel|worktree|command|eval|mcp|daemon|version>")
}
