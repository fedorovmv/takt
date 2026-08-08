package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"takt/internal/application"
	"takt/internal/bootstrap"
	"takt/internal/daemon"
)

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
	app, err := bootstrap.New(abs, ".takt/config.yaml")
	if err != nil {
		return err
	}
	root, err := app.Services.AuthoringService.InitProfile(fs.Arg(0), *force)
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
	selector, err := absoluteIfExistingFile(fs.Arg(0))
	if err != nil {
		return err
	}
	app, err := bootstrap.New(absWorkspace, *configPath)
	if err != nil {
		return err
	}
	configOverride := ""
	if flagPresent(args, "--config") {
		configOverride, err = filepath.Abs(*configPath)
		if err != nil {
			return err
		}
	}
	result, err := app.Services.AuthoringService.ValidateWorkflow(selector, configOverride, *warningsAsErrors)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
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
	if *worktreeFlag && *noWorktreeFlag {
		return fmt.Errorf("--worktree and --no-worktree are mutually exclusive")
	}
	absWorkspace, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	selector, err := absoluteIfExistingFile(fs.Arg(0))
	if err != nil {
		return err
	}
	inputValue, err := absoluteIfExistingFile(*input)
	if err != nil {
		return err
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
	request := application.StartRequest{
		Selector: selector, Input: inputValue, Worktree: worktreeOverride,
		WorktreeBase: *worktreeBase, KeepWorktree: *keepWorktree,
		AllowDirty: *allowDirtyWorktree, Detached: *useDaemon,
	}
	if flagPresent(args, "--config") {
		request.ConfigPath, err = filepath.Abs(*configPath)
		if err != nil {
			return err
		}
	}
	if *useDaemon {
		client, err := daemon.NewClient(absWorkspace, *socket)
		if err != nil {
			return err
		}
		var result application.StartResult
		if err := client.Call(context.Background(), "run.start", request, &result); err != nil {
			return err
		}
		return printResult(*jsonOut, result)
	}
	app, err := bootstrap.New(absWorkspace, *configPath)
	if err != nil {
		return err
	}
	result, err := app.Services.RunService.Start(context.Background(), request)
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result.State)
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
	app, err := bootstrap.New(absWorkspace, "")
	if err != nil {
		return err
	}
	entries, err := app.Services.CatalogService.ListWorkflows(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, map[string]any{"profile": fs.Arg(0), "workflows": entries})
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
	app, err := bootstrap.New(absWorkspace, "")
	if err != nil {
		return err
	}
	result, err := app.Services.CatalogService.DescribeWorkflow(fs.Arg(0))
	if err != nil {
		return err
	}
	return printResult(*jsonOut, result)
}
